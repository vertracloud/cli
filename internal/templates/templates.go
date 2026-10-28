// Package templates implements `vertra template`: the catalog, validating a
// manifest folder, using a template (deploy), creating private templates and
// share links. Every API call goes through the Go SDK (client.Templates).
package templates

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/vertracloud/cli/internal/apps"
	"github.com/vertracloud/cli/internal/flagset"
	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// ErrCancelled is returned when the person does not confirm; the dispatcher
// reports it as a cancelled operation, not as a failure.
var ErrCancelled = apps.ErrCancelled

// Overridable by tests.
var (
	stdin       io.Reader = os.Stdin
	interactive           = ui.IsInteractive
	readSecret            = ui.ReadMasked
	reader      *bufio.Reader
)

// Run executes one `template` command and returns the data to render. errOut
// receives progress and prompts (never data); JSON mode keeps it quiet.
func Run(ctx context.Context, client *vertracloud.Client, command string, args []string, jsonMode bool, errOut io.Writer) (any, error) {
	if client == nil || client.Templates == nil {
		return nil, errors.New("templates: SDK client is unavailable")
	}
	var (
		data any
		err  error
	)
	switch strings.ToLower(command) {
	case "list", "ls":
		data, err = list(ctx, client, args, errOut)
	case "info", "get":
		data, err = info(ctx, client, args)
	case "versions":
		data, err = versions(ctx, client, args)
	case "categories":
		data, err = categories(ctx, client)
	case "validate":
		data, err = validate(ctx, client, args)
	case "deploy":
		data, err = deploy(ctx, client, args, jsonMode, errOut)
	case "create":
		data, err = create(ctx, client, args, jsonMode)
	case "share":
		data, err = share(ctx, client, args)
	default:
		return nil, fmt.Errorf("unknown template command %q", command)
	}
	return data, Friendly(err)
}

// ---------------------------------------------------------------------------
// helpers

func oneArg(pos []string, command, what string) (string, error) {
	if len(pos) != 1 || strings.TrimSpace(pos[0]) == "" {
		return "", fmt.Errorf("%s needs exactly one %s", command, what)
	}
	return pos[0], nil
}

func workspaceOpts(workspace string) []rest.RequestOpt {
	if workspace == "" {
		return nil
	}
	return []rest.RequestOpt{rest.WithWorkspaceID(workspace)}
}

func spin(jsonMode bool, action string) func() {
	if jsonMode {
		return func() {}
	}
	return ui.Spin(text("spin_" + action))
}

func yesNo(b bool) string {
	if b {
		return text("label_yes")
	}
	return text("label_no")
}

// ---------------------------------------------------------------------------
// catalog

func list(ctx context.Context, client *vertracloud.Client, args []string, errOut io.Writer) (any, error) {
	f := flagset.New("template list")
	search := f.String("search", "", "text to look for")
	category := f.String("category", "", "category")
	origin := f.String("origin", "", "vertra, community or private")
	tag := f.String("tag", "", "tag")
	fits := f.Bool("fits", false, "only templates that fit your account")
	sort := f.String("sort", "", "recommended, popular, newest or name")
	page := f.Int("page", 0, "page (1-based)")
	limit := f.Int("limit", 0, "items per page")
	workspace := f.String("workspace", "", "workspace id")
	pos, err := flagset.Parse(f, args)
	if err != nil {
		return nil, err
	}
	if len(pos) != 0 {
		return nil, errors.New("template list takes no arguments")
	}
	params := &vertracloud.TemplateListParams{
		Search: *search, Category: vertracloud.TemplateCategory(*category), Origin: vertracloud.TemplateOrigin(*origin),
		Tag: *tag, Sort: vertracloud.TemplateSort(*sort), Page: *page, Limit: *limit, WorkspaceID: *workspace,
	}
	if *fits {
		params.Fits = fits
	}
	result, err := client.Templates.List(ctx, params)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(result.Data))
	for _, t := range result.Data {
		row := map[string]any{
			"name": I18n(t.Name), "slug": t.Slug, "category": string(t.Category), "origin": string(t.Origin),
			"version": t.CurrentVersion.Version, "plan": string(t.RequiredPlan), "ram": float64(t.RAMMinTotal),
		}
		if t.Account != nil {
			row["fits"] = t.Account.Fits
		}
		rows = append(rows, row)
	}
	if result.HasNextPage && errOut != nil {
		fmt.Fprintln(errOut, ui.Dim(fmt.Sprintf(text("page_hint"), result.Page, result.TotalPages, result.Page+1)))
	}
	return ui.Presentation{JSON: result, Human: rows}, nil
}

func info(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	f := flagset.New("template info")
	workspace := f.String("workspace", "", "workspace id")
	pos, err := flagset.Parse(f, args)
	if err != nil {
		return nil, err
	}
	slug, err := oneArg(pos, "template info", "template id or slug")
	if err != nil {
		return nil, err
	}
	detail, err := client.Templates.Get(ctx, slug, workspaceOpts(*workspace)...)
	if err != nil {
		return nil, err
	}
	return ui.Presentation{JSON: detail, Human: infoView{detail}}, nil
}

func versions(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	f := flagset.New("template versions")
	workspace := f.String("workspace", "", "workspace id")
	pos, err := flagset.Parse(f, args)
	if err != nil {
		return nil, err
	}
	slug, err := oneArg(pos, "template versions", "template id or slug")
	if err != nil {
		return nil, err
	}
	list, err := client.Templates.Versions(ctx, slug, workspaceOpts(*workspace)...)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(list))
	for _, v := range list {
		rows = append(rows, map[string]any{"version": v.Version, "security": v.Security, "created_at": v.CreatedAt.Format("2006-01-02T15:04:05Z07:00")})
	}
	return ui.Presentation{JSON: list, Human: rows}, nil
}

func categories(ctx context.Context, client *vertracloud.Client) (any, error) {
	list, err := client.Templates.Categories(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(list))
	for _, c := range list {
		rows = append(rows, map[string]any{"category": string(c.Category), "count": float64(c.Count)})
	}
	return ui.Presentation{JSON: list, Human: rows}, nil
}

// ---------------------------------------------------------------------------
// validate

// ValidationError is returned when a manifest is not valid. Result carries
// what to print (the dispatcher prints it and exits with a failure status).
type ValidationError struct {
	Result ui.Presentation
	Count  int
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("manifest is invalid (%d issues)", e.Count)
}

func validate(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	f := flagset.New("template validate")
	pos, err := flagset.Parse(f, args)
	if err != nil {
		return nil, err
	}
	target := "."
	if len(pos) > 1 {
		return nil, errors.New("template validate takes one folder")
	} else if len(pos) == 1 {
		target = pos[0]
	}
	m, err := LoadManifest(target)
	if err != nil {
		var syn *SyntaxError
		if errors.As(err, &syn) {
			return nil, syntaxResult(syn)
		}
		return nil, err
	}
	result, err := client.Templates.Validate(ctx, m.Doc)
	if err != nil {
		return nil, err
	}
	view := newValidationView(m, result)
	if !result.Valid || len(result.Issues) > 0 {
		return nil, &ValidationError{Result: ui.Presentation{JSON: view.JSON(), Human: view}, Count: len(view.Issues)}
	}
	return ui.Presentation{JSON: view.JSON(), Human: view}, nil
}

func syntaxResult(syn *SyntaxError) *ValidationError {
	issue := issueView{Code: "yaml_syntax", Message: syn.Message, Line: syn.Line, File: syn.File}
	view := validationView{File: syn.File, Valid: false, Issues: []issueView{issue}}
	return &ValidationError{Result: ui.Presentation{JSON: view.JSON(), Human: view}, Count: 1}
}

// ---------------------------------------------------------------------------
// create

func create(ctx context.Context, client *vertracloud.Client, args []string, jsonMode bool) (any, error) {
	f := flagset.New("template create")
	workspace := f.String("workspace", "", "workspace id that will own the template")
	templateID := f.String("template", "", "publish a new version of this template instead of creating one")
	changelog := f.String("changelog", "", "what changed in this version (pt-br)")
	changelogEN := f.String("changelog-en", "", "what changed in this version (English)")
	changelogES := f.String("changelog-es", "", "what changed in this version (Spanish)")
	pos, err := flagset.Parse(f, args)
	if err != nil {
		return nil, err
	}
	target := "."
	if len(pos) > 1 {
		return nil, errors.New("template create takes one folder")
	} else if len(pos) == 1 {
		target = pos[0]
	}
	if *templateID != "" && *workspace != "" {
		return nil, errors.New("--workspace is for a new template; a new version stays with the template's owner")
	}
	m, err := LoadManifest(target)
	if err != nil {
		var syn *SyntaxError
		if errors.As(err, &syn) {
			return nil, syntaxResult(syn)
		}
		return nil, err
	}
	var log *vertracloud.TemplateI18nText
	if *changelog != "" || *changelogEN != "" || *changelogES != "" {
		pt := *changelog
		if pt == "" {
			pt = *changelogEN
		}
		if pt == "" {
			pt = *changelogES
		}
		log = &vertracloud.TemplateI18nText{PtBR: pt, EN: *changelogEN, ES: *changelogES}
	}
	stop := spin(jsonMode, "pack")
	pkg, err := PackSources(m)
	stop()
	if err != nil {
		return nil, err
	}
	defer pkg.Close()

	stop = spin(jsonMode, "upload")
	defer stop()
	if *templateID != "" {
		version, err := client.Templates.NewVersion(ctx, *templateID, vertracloud.TemplateVersionCreateParams{Manifest: m.Doc, Changelog: log, Sources: pkg.Readers})
		if err != nil {
			return nil, createError(m, err)
		}
		return ui.Presentation{JSON: version, Human: createdView{Version: &version, ID: *templateID}}, nil
	}
	detail, err := client.Templates.Create(ctx, vertracloud.TemplateCreateParams{Manifest: m.Doc, Changelog: log, WorkspaceID: *workspace, Sources: pkg.Readers})
	if err != nil {
		return nil, createError(m, err)
	}
	return ui.Presentation{JSON: detail, Human: createdView{Detail: &detail}}, nil
}

// createError turns the manifest findings of a rejected create into the same
// listing `validate` prints, with the lines of the file, and the files that
// hold secrets into a readable message.
func createError(m *Manifest, err error) error {
	if issues, ok := vertracloud.TemplateManifestIssues(err); ok {
		view := newValidationView(m, vertracloud.TemplateValidation{Valid: false, Issues: issues})
		return &ValidationError{Result: ui.Presentation{JSON: view.JSON(), Human: view}, Count: len(view.Issues)}
	}
	if files, ok := vertracloud.TemplateSecretFiles(err); ok {
		list := make([]string, 0, len(files))
		for _, name := range files {
			list = append(list, ui.Plain(name))
		}
		c := &rest.APIError{Code: vertracloud.ErrCodeTemplateSecretInSource, Message: fmt.Sprintf(text("secret_in_source"), strings.Join(list, ", "))}
		if apiErr, ok := rest.AsAPIError(err); ok {
			c.Status, c.Details = apiErr.Status, apiErr.Details
		}
		return c
	}
	return err
}

// ---------------------------------------------------------------------------
// share links

func share(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	verb := "create"
	if len(args) > 0 {
		switch args[0] {
		case "create", "list", "ls", "revoke":
			verb, args = args[0], args[1:]
		}
	}
	links := client.Templates.ShareLinks()
	switch verb {
	case "list", "ls":
		f := flagset.New("template share list")
		pos, err := flagset.Parse(f, args)
		if err != nil {
			return nil, err
		}
		id, err := oneArg(pos, "template share list", "template id")
		if err != nil {
			return nil, err
		}
		list, err := links.List(ctx, id)
		if err != nil {
			return nil, err
		}
		rows := make([]map[string]any, 0, len(list))
		for _, l := range list {
			row := map[string]any{"id": l.ID, "mode": string(l.Mode), "last4": l.Last4, "uses": float64(l.Uses), "created_at": l.CreatedAt.Format("2006-01-02T15:04:05Z07:00")}
			if l.MaxUses != nil {
				row["max_uses"] = float64(*l.MaxUses)
			}
			if l.ExpiresAt != nil {
				row["expires_at"] = l.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")
			}
			if l.RevokedAt != nil {
				row["revoked_at"] = l.RevokedAt.Format("2006-01-02T15:04:05Z07:00")
			}
			rows = append(rows, row)
		}
		return ui.Presentation{JSON: list, Human: rows}, nil
	case "revoke":
		f := flagset.New("template share revoke")
		pos, err := flagset.Parse(f, args)
		if err != nil {
			return nil, err
		}
		if len(pos) != 2 {
			return nil, errors.New("template share revoke needs a template id and a link id")
		}
		link, err := links.Revoke(ctx, pos[0], pos[1])
		if err != nil {
			return nil, err
		}
		return ui.Presentation{JSON: link, Human: messageView{Success: text("share_revoked")}}, nil
	}
	f := flagset.New("template share")
	mode := f.String("mode", "use", "use or full_copy")
	maxUses := f.Int("max-uses", 0, "maximum number of uses (0: no limit)")
	expires := f.Int("expires-days", 0, "days until the link expires (0: never)")
	pos, err := flagset.Parse(f, args)
	if err != nil {
		return nil, err
	}
	id, err := oneArg(pos, "template share", "template id")
	if err != nil {
		return nil, err
	}
	if *mode != string(vertracloud.TemplateShareLinkModeUse) && *mode != string(vertracloud.TemplateShareLinkModeFullCopy) {
		return nil, errors.New(text("share_mode_bad"))
	}
	if *maxUses < 0 || *expires < 0 {
		return nil, errors.New("--max-uses and --expires-days cannot be negative")
	}
	created, err := links.Create(ctx, id, vertracloud.TemplateShareLinkCreateBody{
		Mode: vertracloud.TemplateShareLinkMode(*mode), MaxUses: *maxUses, ExpiresInDays: *expires,
	})
	if err != nil {
		return nil, err
	}
	return ui.Presentation{JSON: created, Human: shareView{created}}, nil
}
