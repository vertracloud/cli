package apps

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// fakeReleases records the calls that reach the SDK.
type fakeReleases struct {
	calls        []string
	listParams   vertracloud.ApplicationReleasesListParams
	rollbackBody *vertracloud.ApplicationRollbackBody
	rollbackErr  error
	log          vertracloud.ApplicationReleaseLog
}

func (f *fakeReleases) List(_ context.Context, _ string, p vertracloud.ApplicationReleasesListParams, _ ...rest.RequestOpt) (vertracloud.ApplicationReleasePage, error) {
	f.calls = append(f.calls, "list")
	f.listParams = p
	return vertracloud.ApplicationReleasePage{}, nil
}
func (f *fakeReleases) Log(context.Context, string, string, ...rest.RequestOpt) (vertracloud.ApplicationReleaseLog, error) {
	f.calls = append(f.calls, "log")
	return f.log, nil
}
func (f *fakeReleases) Preview(context.Context, string, string, ...rest.RequestOpt) (vertracloud.ApplicationRollbackPreview, error) {
	f.calls = append(f.calls, "preview")
	return vertracloud.ApplicationRollbackPreview{}, nil
}
func (f *fakeReleases) Rollback(_ context.Context, _, _ string, body vertracloud.ApplicationRollbackBody, _ ...rest.RequestOpt) (vertracloud.ApplicationRelease, error) {
	f.calls = append(f.calls, "rollback")
	f.rollbackBody = &body
	return vertracloud.ApplicationRelease{ID: "new"}, f.rollbackErr
}

func run(t *testing.T, apps *fakeApps, args ...string) (any, error) {
	t.Helper()
	t.Setenv("CI", "1") // no terminal
	return Run(context.Background(), &vertracloud.Client{Apps: apps}, args[0], args[1:])
}

func TestRollbackWithoutYesFailsClosedBeforeAnyCall(t *testing.T) {
	apps := &fakeApps{}
	if _, err := run(t, apps, "rollback", "app-1", "rel-1", "--env"); err == nil {
		t.Fatal("expected error without --yes and without a terminal")
	}
	if len(apps.releases.calls) != 0 {
		t.Fatalf("SDK called: %v", apps.releases.calls)
	}
}

func TestRollbackYesSendsRestoreEnvironment(t *testing.T) {
	apps := &fakeApps{}
	if _, err := run(t, apps, "rollback", "app-1", "rel-1", "--yes"); err != nil {
		t.Fatal(err)
	}
	if got := apps.releases.calls; len(got) != 1 || got[0] != "rollback" || apps.releases.rollbackBody.RestoreEnvironment != nil {
		t.Fatalf("calls=%v body=%+v", got, apps.releases.rollbackBody)
	}
	apps = &fakeApps{}
	if _, err := run(t, apps, "rollback", "app-1", "rel-1", "--env", "-y"); err != nil {
		t.Fatal(err)
	}
	b := apps.releases.rollbackBody
	if got := apps.releases.calls; len(got) != 2 || got[0] != "preview" || got[1] != "rollback" || b.RestoreEnvironment == nil || !*b.RestoreEnvironment {
		t.Fatalf("calls=%v body=%+v", got, b)
	}
}

func TestRollbackKnownErrorsAreLocalized(t *testing.T) {
	ui.Locale = "pt"
	defer func() { ui.Locale = "en" }()
	apps := &fakeApps{}
	apps.releases.rollbackErr = &rest.APIError{Status: 403, Code: "PLAN_DOES_NOT_SUPPORT_ROLLBACK", Message: "x"}
	_, err := run(t, apps, "rollback", "app-1", "rel-1", "--yes")
	var apiErr *rest.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "PLAN_DOES_NOT_SUPPORT_ROLLBACK" || !strings.Contains(apiErr.Message, "Pro") || apiErr.Message == "x" {
		t.Fatalf("err = %v", err)
	}
	apps.releases.rollbackErr = &rest.APIError{Status: 403, Code: "API_KEY_SCOPE_DENIED", Message: "x"}
	if _, err = run(t, apps, "rollback", "app-1", "rel-1", "--yes"); err == nil || err.Error() != "API_KEY_SCOPE_DENIED (403): x" {
		t.Fatalf("scope error without --env must stay untouched, got %v", err)
	}
	if _, err = run(t, apps, "rollback", "app-1", "rel-1", "--yes", "--env"); err == nil || !strings.Contains(err.Error(), "apps:envs") {
		t.Fatalf("scope error with --env = %v", err)
	}
}

func TestReleasesPassesPageAndLimit(t *testing.T) {
	apps := &fakeApps{}
	if _, err := run(t, apps, "releases", "app-1", "--page", "2", "--limit=10"); err != nil {
		t.Fatal(err)
	}
	if p := apps.releases.listParams; p.Page != 2 || p.Limit != 10 {
		t.Fatalf("params = %+v", p)
	}
	if _, err := run(t, apps, "releases", "app-1", "--limit", "51"); err == nil {
		t.Fatal("limit above 50 must fail locally")
	}
}

func TestReleasesLogWithoutContentSaysSo(t *testing.T) {
	apps := &fakeApps{}
	got, err := run(t, apps, "releases", "log", "app-1", "rel-1")
	p, ok := got.(ui.Presentation)
	if err != nil || !ok || p.Human != ui.Text("release_no_log") {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestConfigBuildsDeploySettings(t *testing.T) {
	apps := &fakeApps{}
	if _, err := run(t, apps, "config", "app-1", "--ram", "512"); err != nil || apps.configBody.Deploy != nil {
		t.Fatalf("deploy must be absent: %+v err=%v", apps.configBody, err)
	}
	if _, err := run(t, apps, "config", "app-1", "--zero-downtime", "on", "--auto-rollback", "off", "--health-path", "/healthz"); err != nil {
		t.Fatal(err)
	}
	d := apps.configBody.Deploy
	if *d.ZeroDowntime != vertracloud.ApplicationZeroDowntimeOn || *d.AutoRollback || d.HealthCheckPath == nil || d.HealthCheckPath.Value == nil {
		t.Fatalf("deploy = %+v", d)
	}
	if _, err := run(t, apps, "config", "app-1", "--clear-health-path"); err != nil {
		t.Fatal(err)
	}
	if h := apps.configBody.Deploy.HealthCheckPath; h == nil || h.Value != nil {
		t.Fatalf("clear must send null, got %+v", h)
	}
}

func TestConfigRejectsInvalidDeployFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--zero-downtime", "maybe"},
		{"--auto-rollback", "true"},
		{"--health-path", "health"},
		{"--health-path", "/a b"},
		{"--health-path", "/ação"},
		{"--health-path", "/" + strings.Repeat("a", 200)},
		{"--health-path", "/ok", "--clear-health-path"},
	} {
		apps := &fakeApps{}
		if _, err := run(t, apps, append([]string{"config", "app-1"}, args...)...); err == nil || apps.configBody != nil {
			t.Fatalf("%v must fail before the SDK, err=%v", args, err)
		}
	}
	apps := &fakeApps{}
	if _, err := run(t, apps, "config", "app-1", "--health-path", "/"+strings.Repeat("a", 199)); err != nil {
		t.Fatalf("200 chars is the limit: %v", err)
	}
}
