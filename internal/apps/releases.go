package apps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// ErrCancelled is returned when the person declines a confirmation prompt.
var ErrCancelled = errors.New("operation cancelled")

// healthPathRe is the API rule for the health check path: starts with "/", visible ASCII, no spaces.
var healthPathRe = regexp.MustCompile(`^/[\x21-\x7e]*$`)

// deployUpdate builds the Deploy block of `app config`; nil when no deploy flag was given.
func deployUpdate(set map[string]bool, zeroDowntime, autoRollback, healthPath string, clearHealth bool) (*vertracloud.ApplicationDeploySettingsUpdate, error) {
	if set["health-path"] && clearHealth {
		return nil, errors.New("--health-path and --clear-health-path cannot be used together")
	}
	if !set["zero-downtime"] && !set["auto-rollback"] && !set["health-path"] && !clearHealth {
		return nil, nil
	}
	d := &vertracloud.ApplicationDeploySettingsUpdate{}
	if set["zero-downtime"] {
		switch zd := vertracloud.ApplicationZeroDowntime(zeroDowntime); zd {
		case vertracloud.ApplicationZeroDowntimeAuto, vertracloud.ApplicationZeroDowntimeOn, vertracloud.ApplicationZeroDowntimeOff:
			d.ZeroDowntime = &zd
		default:
			return nil, errors.New("--zero-downtime must be auto, on or off")
		}
	}
	if set["auto-rollback"] {
		if autoRollback != "on" && autoRollback != "off" {
			return nil, errors.New("--auto-rollback must be on or off")
		}
		on := autoRollback == "on"
		d.AutoRollback = &on
	}
	if set["health-path"] {
		if len(healthPath) > 200 || !healthPathRe.MatchString(healthPath) {
			return nil, errors.New("--health-path must start with / and have up to 200 visible characters without spaces")
		}
		d.HealthCheckPath = vertracloud.NullableValue(healthPath)
	}
	if clearHealth {
		d.HealthCheckPath = vertracloud.NullableNull[string]()
	}
	return d, nil
}

// friendly swaps the API message of known plan/state errors for a localized one,
// keeping status, code and details. needsEnvScope says the call used apps:envs.
func friendly(err error, needsEnvScope bool) error {
	var apiErr *rest.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	key := map[string]string{
		"PLAN_DOES_NOT_SUPPORT_ROLLBACK":      "err_rollback_plan",
		"RELEASE_EXPIRED":                     "err_release_expired",
		"RELEASE_ALREADY_ACTIVE":              "err_release_active",
		"RESTORE_COOLDOWN":                    "err_restore_cooldown",
		"OPERATION_IN_PROGRESS":               "err_operation_in_progress",
		"PLAN_DOES_NOT_SUPPORT_ZERO_DOWNTIME": "err_zero_downtime_plan",
		"PLAN_DOES_NOT_SUPPORT_AUTO_ROLLBACK": "err_auto_rollback_plan",
	}[apiErr.Code]
	if apiErr.Code == "API_KEY_SCOPE_DENIED" && needsEnvScope {
		key = "err_scope_envs"
	}
	if key == "" {
		return err
	}
	c := *apiErr
	c.Message = ui.Text(key)
	return &c
}

// idAndRelease reads "<id> <release-id>"; with a single argument the app comes from the linked folder.
func idAndRelease(ctx context.Context, client *vertracloud.Client, pos []string, command string) (string, string, error) {
	switch len(pos) {
	case 1:
		id, err := resolveAppID(ctx, client, "", command)
		return id, pos[0], err
	case 2:
		return pos[0], pos[1], nil
	}
	return "", "", fmt.Errorf("%s needs [<app-id>] <release-id>", command)
}

func releases(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	if len(args) > 0 && args[0] == "log" {
		return releaseLog(ctx, client, args[1:])
	}
	f := fs("app releases")
	page := f.Int("page", 0, "page number")
	limit := f.Int("limit", 0, "rows per page")
	pos, err := parse(f, args, map[string]bool{"--page": true, "--limit": true}, nil)
	if err != nil {
		return nil, err
	}
	if *page < 0 || *limit < 0 || *limit > 50 {
		return nil, errors.New("--page must be positive and --limit between 1 and 50")
	}
	id, rest, err := requireID(ctx, client, pos, "app releases")
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, errors.New("app releases accepts one app id")
	}
	result, err := client.Apps.Releases().List(ctx, id, vertracloud.ApplicationReleasesListParams{Page: *page, Limit: *limit})
	if err != nil {
		return nil, err
	}
	if result.HasNextPage {
		fmt.Fprintln(os.Stderr, ui.Dim(fmt.Sprintf(ui.Text("releases_more"), result.Page, result.TotalPages, result.Page+1)))
	}
	rows := make([]map[string]any, 0, len(result.Data))
	for _, r := range result.Data {
		message := ""
		if r.Message != nil {
			message = strings.TrimSpace(strings.SplitN(*r.Message, "\n", 2)[0])
			if runes := []rune(message); len(runes) > 48 {
				message = string(runes[:47]) + "…"
			}
		}
		rows = append(rows, map[string]any{
			"id": r.ID, "source": r.Source, "release_status": r.Status, "author": r.Author, "message": message,
			"created_at": r.CreatedAt, "rollback": r.Rollback.Available, "expires_at": r.Rollback.ExpiresAt,
		})
	}
	return ui.Presentation{JSON: result, Human: rows}, nil
}

func releaseLog(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	f := fs("app releases log")
	pos, err := parse(f, args, nil, nil)
	if err != nil {
		return nil, err
	}
	id, releaseID, err := idAndRelease(ctx, client, pos, "app releases log")
	if err != nil {
		return nil, err
	}
	result, err := client.Apps.Releases().Log(ctx, id, releaseID)
	if err != nil {
		return nil, err
	}
	human := ui.Text("release_no_log")
	if result.Content != nil && strings.TrimSpace(*result.Content) != "" {
		human = *result.Content
	}
	return ui.Presentation{JSON: result, Human: human}, nil
}

func rollback(ctx context.Context, client *vertracloud.Client, args []string, jsonMode bool) (any, error) {
	f := fs("app rollback")
	restoreEnv := f.Bool("env", false, "also restore the release's environment variables")
	yes := f.Bool("yes", false, "skip confirmation")
	pos, err := parse(f, args, nil, map[string]bool{"--env": true, "--yes": true, "-y": true})
	if err != nil {
		return nil, err
	}
	id, releaseID, err := idAndRelease(ctx, client, pos, "app rollback")
	if err != nil {
		return nil, err
	}
	// Fail closed before any request: without --yes a person has to answer.
	if !*yes && (jsonMode || !ui.IsInteractive()) {
		return nil, errors.New("app rollback requires --yes when there is no terminal to confirm")
	}
	svc := client.Apps.Releases()
	var preview vertracloud.ApplicationRollbackPreview
	if *restoreEnv {
		if preview, err = svc.Preview(ctx, id, releaseID); err != nil {
			return nil, friendly(err, true)
		}
	}
	if !*yes {
		fmt.Fprintln(os.Stderr, fmt.Sprintf(ui.Text("rollback_summary"), releaseID))
		switch {
		case !*restoreEnv:
			fmt.Fprintln(os.Stderr, ui.Dim(ui.Text("rollback_env_kept")))
		case len(preview.EnvironmentChanges) == 0:
			fmt.Fprintln(os.Stderr, ui.Text("rollback_env_none"))
		default:
			fmt.Fprintln(os.Stderr, ui.Text("rollback_env_changes"))
			for _, c := range preview.EnvironmentChanges {
				fmt.Fprintf(os.Stderr, "  %s  %s\n", c.Key, ui.Dim(ui.Text("env_"+string(c.Change))))
			}
		}
		ok, err := confirm(os.Stdin, ui.Text("rollback_confirm")+" [y/N] ")
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrCancelled
		}
	}
	body := vertracloud.ApplicationRollbackBody{}
	if *restoreEnv {
		body.RestoreEnvironment = restoreEnv
	}
	release, err := svc.Rollback(ctx, id, releaseID, body)
	if err != nil {
		return nil, friendly(err, *restoreEnv)
	}
	return ui.Presentation{JSON: release, Human: map[string]any{"id": release.ID, "status": release.Status, "source": release.Source}}, nil
}
