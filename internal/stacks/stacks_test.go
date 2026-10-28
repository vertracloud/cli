package stacks

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/vertracloud/cli/internal/templates"
	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

type fakeStacks struct {
	vertracloud.StacksService
	stack     vertracloud.Stack
	deleted   vertracloud.StackDeleteBody
	deletedID string
	updated   vertracloud.StackUpdateBody
	err       error
}

func (f *fakeStacks) List(context.Context, ...rest.RequestOpt) ([]vertracloud.Stack, error) {
	return []vertracloud.Stack{f.stack}, f.err
}
func (f *fakeStacks) Get(context.Context, string, ...rest.RequestOpt) (vertracloud.Stack, error) {
	return f.stack, f.err
}
func (f *fakeStacks) Update(_ context.Context, _ string, b vertracloud.StackUpdateBody, _ ...rest.RequestOpt) (vertracloud.Stack, error) {
	f.updated = b
	return f.stack, f.err
}
func (f *fakeStacks) Delete(_ context.Context, id string, b vertracloud.StackDeleteBody, _ ...rest.RequestOpt) (vertracloud.StackDeleteResult, error) {
	f.deletedID, f.deleted = id, b
	return vertracloud.StackDeleteResult{DeletedApps: []string{"app-1"}}, f.err
}

type fakeUpdates struct {
	vertracloud.TemplateUpdatesService
	info vertracloud.TemplateUpdateInfo
	body vertracloud.TemplateUpdateBody
	ran  bool
}

func (u *fakeUpdates) StackInfo(context.Context, string, ...rest.RequestOpt) (vertracloud.TemplateUpdateInfo, error) {
	return u.info, nil
}
func (u *fakeUpdates) StackUpdate(_ context.Context, _ string, b vertracloud.TemplateUpdateBody, _ ...rest.RequestOpt) (vertracloud.TemplateDeploy, error) {
	u.ran, u.body = true, b
	return vertracloud.TemplateDeploy{ID: "job-1", Status: vertracloud.TemplateDeployStatusQueued}, nil
}

type fakeTemplates struct {
	vertracloud.TemplatesService
	updates *fakeUpdates
}

func (f *fakeTemplates) Updates() vertracloud.TemplateUpdatesService { return f.updates }
func (f *fakeTemplates) Deploys() vertracloud.TemplateDeploysService { return &fakeDeploys{} }

type fakeDeploys struct {
	vertracloud.TemplateDeploysService
}

func (d *fakeDeploys) Events(context.Context, string, ...rest.RequestOpt) (*vertracloud.TemplateDeployEventStream, error) {
	return nil, errors.New("no stream")
}
func (d *fakeDeploys) Get(context.Context, string, ...rest.RequestOpt) (vertracloud.TemplateDeploy, error) {
	return vertracloud.TemplateDeploy{ID: "job-1", Status: vertracloud.TemplateDeployStatusSucceeded, Version: "1.1.0"}, nil
}

func sample() vertracloud.Stack {
	status := "up"
	db := vertracloud.DatabaseTypePostgreSQL
	key := "app"
	to := "db-1"
	return vertracloud.Stack{
		ID: "stk-1", Name: "meu-n8n", NetworkMode: vertracloud.StackNetworkModePublic, RAMTotal: 1536,
		Status: vertracloud.StackStatus{Up: 2, Total: 2},
		Members: []vertracloud.StackMember{
			{ResourceType: vertracloud.StackMemberTypeApplication, ResourceID: "app-1", Name: "n8n-app", Status: &status, RAM: 1024, ServiceKey: &key},
			{ResourceType: vertracloud.StackMemberTypeDatabase, ResourceID: "db-1", Name: "n8n-db", Status: &status, RAM: 512, DatabaseType: &db},
		},
		Bindings:  []vertracloud.StackBinding{{ID: "b1", AppID: "app-1", DatabaseID: &to, EnvMap: map[string]vertracloud.StackBindingValue{"DATABASE_URL": "url"}}},
		Template:  &vertracloud.TemplateOriginInfo{Name: vertracloud.TemplateI18nText{PtBR: "n8n"}, Version: "1.0.0", UpdateAvailable: true},
		UpdatedAt: time.Now(), UpdateAvailable: true,
	}
}

func client(s *fakeStacks, u *fakeUpdates) *vertracloud.Client {
	return &vertracloud.Client{Stacks: s, Templates: &fakeTemplates{updates: u}}
}

func withStdin(t *testing.T, input string, tty bool) {
	t.Helper()
	t.Cleanup(templates.UseInput(strings.NewReader(input), tty))
}

func render(v any) string {
	var b bytes.Buffer
	ui.Render(&b, v, "stack", "en")
	return b.String()
}

func TestListAndGetPrintMembersAndConnections(t *testing.T) {
	f := &fakeStacks{stack: sample()}
	got, err := Run(context.Background(), client(f, nil), "list", nil, false, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if out := render(got); !strings.Contains(out, "meu-n8n") || !strings.Contains(out, "n8n@1.0.0") {
		t.Fatalf("list:\n%s", out)
	}
	got, err = Run(context.Background(), client(f, nil), "get", []string{"stk-1"}, false, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	out := render(got)
	for _, want := range []string{"n8n-app", "n8n-db", "n8n-app → n8n-db", "DATABASE_URL", "vertra stack update stk-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("get lacks %q:\n%s", want, out)
		}
	}
}

func TestDeleteDissolveByDefaultAndSendsTheName(t *testing.T) {
	f := &fakeStacks{stack: sample()}
	if _, err := Run(context.Background(), client(f, nil), "delete", []string{"stk-1", "--yes"}, true, io.Discard); err != nil {
		t.Fatal(err)
	}
	if f.deleted.Mode != vertracloud.StackDeleteModeDissolve || f.deleted.ConfirmName != "meu-n8n" || f.deleted.Blob != "" {
		t.Fatalf("body = %+v", f.deleted)
	}
}

func TestDeleteAllWithBlobChoice(t *testing.T) {
	f := &fakeStacks{stack: sample()}
	if _, err := Run(context.Background(), client(f, nil), "delete", []string{"stk-1", "--delete-all", "--blob", "delete", "-y"}, true, io.Discard); err != nil {
		t.Fatal(err)
	}
	if f.deleted.Mode != vertracloud.StackDeleteModeDeleteAll || f.deleted.Blob != vertracloud.StackBlobDispositionDelete {
		t.Fatalf("body = %+v", f.deleted)
	}
	for _, args := range [][]string{{"stk-1", "--blob", "delete", "--yes"}, {"stk-1", "--delete-all", "--blob", "maybe", "--yes"}} {
		f2 := &fakeStacks{stack: sample()}
		if _, err := Run(context.Background(), client(f2, nil), "delete", args, true, io.Discard); err == nil || f2.deletedID != "" {
			t.Errorf("args %v: expected an error before any call", args)
		}
	}
}

func TestDeleteAsksForTheNameAndCancelsOnAMismatch(t *testing.T) {
	f := &fakeStacks{stack: sample()}
	withStdin(t, "meu-n8n\n", true)
	if _, err := Run(context.Background(), client(f, nil), "delete", []string{"stk-1"}, false, io.Discard); err != nil || f.deletedID != "stk-1" {
		t.Fatalf("err=%v deleted=%q", err, f.deletedID)
	}
	f = &fakeStacks{stack: sample()}
	withStdin(t, "yes\n", true)
	if _, err := Run(context.Background(), client(f, nil), "delete", []string{"stk-1"}, false, io.Discard); !errors.Is(err, ErrCancelled) || f.deletedID != "" {
		t.Fatalf("err=%v deleted=%q", err, f.deletedID)
	}
}

func TestDeleteWithoutYesFailsInJSONOrWithoutATerminal(t *testing.T) {
	f := &fakeStacks{stack: sample()}
	withStdin(t, "meu-n8n\n", true)
	if _, err := Run(context.Background(), client(f, nil), "delete", []string{"stk-1"}, true, io.Discard); err == nil || f.deletedID != "" {
		t.Fatalf("json: %v", err)
	}
	withStdin(t, "meu-n8n\n", false)
	if _, err := Run(context.Background(), client(f, nil), "delete", []string{"stk-1"}, false, io.Discard); err == nil || f.deletedID != "" {
		t.Fatalf("no tty: %v", err)
	}
}

func TestRenameSendsOnlyTheGivenFields(t *testing.T) {
	f := &fakeStacks{stack: sample()}
	if _, err := Run(context.Background(), client(f, nil), "rename", []string{"stk-1", "novo"}, true, io.Discard); err != nil || f.updated.Name == nil || *f.updated.Name != "novo" || f.updated.Description != nil {
		t.Fatalf("err=%v body=%+v", err, f.updated)
	}
	if _, err := Run(context.Background(), client(f, nil), "rename", []string{"stk-1", "--description", ""}, true, io.Discard); err != nil || f.updated.Description == nil {
		t.Fatalf("an empty description clears it: %v", err)
	}
	if _, err := Run(context.Background(), client(f, nil), "rename", []string{"stk-1"}, true, io.Discard); err == nil {
		t.Fatal("nothing to change must fail")
	}
}

func available() *fakeUpdates {
	latest := "1.1.0"
	return &fakeUpdates{info: vertracloud.TemplateUpdateInfo{
		UpdateAvailable: true, CurrentVersion: "1.0.0", LatestVersion: &latest, Security: true,
		RAMChanges: []vertracloud.TemplateRAMChange{{ServiceID: "app", Current: 512, RequiredMin: 1024}},
		NewInputs:  []vertracloud.TemplateInput{{ID: "smtp", Type: vertracloud.TemplateInputTypeText, Label: vertracloud.TemplateI18nText{PtBR: "SMTP"}, Required: true}},
	}}
}

func TestUpdateCheckOnlyShowsWhatWouldChange(t *testing.T) {
	u := available()
	got, err := Run(context.Background(), client(&fakeStacks{}, u), "update", []string{"stk-1", "--check"}, false, io.Discard)
	if err != nil || u.ran {
		t.Fatalf("err=%v ran=%v", err, u.ran)
	}
	out := render(got)
	for _, want := range []string{"1.0.0 → 1.1.0", "security", "512 to 1024 MB", "smtp"} {
		if !strings.Contains(out, want) {
			t.Errorf("check lacks %q:\n%s", want, out)
		}
	}
}

func TestUpdateYesSendsAnswersAndMinimumRAM(t *testing.T) {
	u := available()
	withStdin(t, "", false)
	if _, err := Run(context.Background(), client(&fakeStacks{}, u), "update", []string{"stk-1", "--yes", "--set", "smtp=mail.example"}, true, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !u.ran || u.body.Inputs["smtp"] != "mail.example" || u.body.RAM["app"] != 1024 {
		t.Fatalf("body = %+v ran=%v", u.body, u.ran)
	}
	u = available()
	if _, err := Run(context.Background(), client(&fakeStacks{}, u), "update", []string{"stk-1", "--yes", "--set", "smtp=x", "--ram", "app=2048"}, true, io.Discard); err != nil || u.body.RAM["app"] != 2048 {
		t.Fatalf("err=%v body=%+v", err, u.body)
	}
}

func TestUpdateNeedsConfirmationAndRequiredAnswers(t *testing.T) {
	u := available()
	withStdin(t, "", false)
	if _, err := Run(context.Background(), client(&fakeStacks{}, u), "update", []string{"stk-1", "--set", "smtp=x"}, false, io.Discard); err == nil || u.ran {
		t.Fatalf("without --yes and without a terminal it must not run: %v", err)
	}
	if _, err := Run(context.Background(), client(&fakeStacks{}, u), "update", []string{"stk-1", "--yes"}, true, io.Discard); err == nil || u.ran {
		t.Fatalf("a required new question must be answered: %v", err)
	}
	withStdin(t, "mail.example\nn\n", true)
	if _, err := Run(context.Background(), client(&fakeStacks{}, u), "update", []string{"stk-1"}, false, io.Discard); !errors.Is(err, ErrCancelled) || u.ran {
		t.Fatalf("declining must cancel: %v", err)
	}
	withStdin(t, "mail.example\ny\n", true)
	if _, err := Run(context.Background(), client(&fakeStacks{}, u), "update", []string{"stk-1"}, false, io.Discard); err != nil || !u.ran {
		t.Fatalf("err=%v ran=%v", err, u.ran)
	}
}

func TestUpdateAlreadyLatestAndBlocked(t *testing.T) {
	u := &fakeUpdates{info: vertracloud.TemplateUpdateInfo{CurrentVersion: "1.1.0"}}
	got, err := Run(context.Background(), client(&fakeStacks{}, u), "update", []string{"stk-1", "--yes"}, false, io.Discard)
	if err != nil || u.ran || !strings.Contains(render(got), "latest") {
		t.Fatalf("err=%v out=%s", err, render(got))
	}
	u = available()
	u.info.Blocked = &vertracloud.TemplateUpdateBlocked{Reason: vertracloud.TemplateUpdateBlockReasonAppBuilding}
	if _, err := Run(context.Background(), client(&fakeStacks{}, u), "update", []string{"stk-1", "--yes", "--set", "smtp=x"}, true, io.Discard); err == nil || u.ran {
		t.Fatalf("blocked update must fail: %v", err)
	}
}

func TestStackErrorsAreFriendly(t *testing.T) {
	f := &fakeStacks{err: &rest.APIError{Status: 404, Code: "STACK_NOT_FOUND", Message: "raw"}}
	_, err := Run(context.Background(), client(f, nil), "get", []string{"x"}, true, io.Discard)
	apiErr, _ := rest.AsAPIError(err)
	if apiErr == nil || apiErr.Message == "raw" {
		t.Fatalf("err = %v", err)
	}
	if _, err := Run(context.Background(), client(f, nil), "get", nil, true, io.Discard); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("err = %v", err)
	}
	_ = templates.Text("stack_none")
}
