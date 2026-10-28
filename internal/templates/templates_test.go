package templates

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// ---------------------------------------------------------------------------
// fakes

type fakeTemplates struct {
	vertracloud.TemplatesService
	detail    vertracloud.TemplateDetail
	validated any
	validate  vertracloud.TemplateValidation
	created   vertracloud.TemplateCreateParams
	version   vertracloud.TemplateVersionCreateParams
	versionID string
	uploaded  map[string][]byte
	deployed  vertracloud.TemplateDeployBody
	deploySlg string
	listed    *vertracloud.TemplateListParams
	err       error
	jobs      []vertracloud.TemplateDeploy
	links     *fakeLinks
	share     *fakeShare
	gets      int
}

func (f *fakeTemplates) List(_ context.Context, p *vertracloud.TemplateListParams, _ ...rest.RequestOpt) (vertracloud.TemplatePage, error) {
	f.listed = p
	return vertracloud.TemplatePage{Data: []vertracloud.Template{f.detail.Template}, Page: 1, TotalPages: 2, HasNextPage: true}, nil
}
func (f *fakeTemplates) Get(context.Context, string, ...rest.RequestOpt) (vertracloud.TemplateDetail, error) {
	return f.detail, f.err
}
func (f *fakeTemplates) Validate(_ context.Context, manifest any, _ ...rest.RequestOpt) (vertracloud.TemplateValidation, error) {
	f.validated = manifest
	return f.validate, f.err
}
func (f *fakeTemplates) Deploy(_ context.Context, slug string, b vertracloud.TemplateDeployBody, _ ...rest.RequestOpt) (vertracloud.TemplateDeploy, error) {
	f.deploySlg, f.deployed = slug, b
	return vertracloud.TemplateDeploy{ID: "job-1", Status: vertracloud.TemplateDeployStatusQueued}, f.err
}
func readSources(p map[string]io.Reader) map[string][]byte {
	out := map[string][]byte{}
	for id, r := range p {
		out[id], _ = io.ReadAll(r)
	}
	return out
}
func (f *fakeTemplates) Create(_ context.Context, p vertracloud.TemplateCreateParams, _ ...rest.RequestOpt) (vertracloud.TemplateDetail, error) {
	f.created = p
	f.uploaded = readSources(p.Sources)
	return f.detail, f.err
}
func (f *fakeTemplates) NewVersion(_ context.Context, id string, p vertracloud.TemplateVersionCreateParams, _ ...rest.RequestOpt) (vertracloud.TemplateVersion, error) {
	f.versionID, f.version = id, p
	f.uploaded = readSources(p.Sources)
	return vertracloud.TemplateVersion{TemplateVersionSummary: vertracloud.TemplateVersionSummary{Version: "1.1.0"}}, f.err
}
func (f *fakeTemplates) Deploys() vertracloud.TemplateDeploysService { return &fakeDeploys{f} }
func (f *fakeTemplates) ShareLinks() vertracloud.TemplateShareLinksService {
	if f.links == nil {
		f.links = &fakeLinks{}
	}
	return f.links
}
func (f *fakeTemplates) Share() vertracloud.TemplateShareService { return f.share }

type fakeDeploys struct{ f *fakeTemplates }

func (d *fakeDeploys) Events(context.Context, string, ...rest.RequestOpt) (*vertracloud.TemplateDeployEventStream, error) {
	return nil, errors.New("no stream in tests")
}
func (d *fakeDeploys) Get(context.Context, string, ...rest.RequestOpt) (vertracloud.TemplateDeploy, error) {
	i := d.f.gets
	d.f.gets++
	if i >= len(d.f.jobs) {
		i = len(d.f.jobs) - 1
	}
	return d.f.jobs[i], nil
}

type fakeLinks struct {
	vertracloud.TemplateShareLinksService
	body    vertracloud.TemplateShareLinkCreateBody
	revoked [2]string
}

func (l *fakeLinks) Create(_ context.Context, _ string, b vertracloud.TemplateShareLinkCreateBody, _ ...rest.RequestOpt) (vertracloud.TemplateShareLinkCreated, error) {
	l.body = b
	return vertracloud.TemplateShareLinkCreated{TemplateShareLink: vertracloud.TemplateShareLink{ID: "lnk-1", Mode: vertracloud.TemplateShareLinkModeUse, Last4: "wxyz"}, Token: "vtpl_secrettoken"}, nil
}
func (l *fakeLinks) Revoke(_ context.Context, tid, lid string, _ ...rest.RequestOpt) (vertracloud.TemplateShareLink, error) {
	l.revoked = [2]string{tid, lid}
	return vertracloud.TemplateShareLink{ID: lid}, nil
}

type fakeShare struct {
	vertracloud.TemplateShareService
	preview  vertracloud.TemplateSharePreview
	token    string
	deployed vertracloud.TemplateShareDeployBody
}

func (s *fakeShare) Preview(_ context.Context, token string, _ ...rest.RequestOpt) (vertracloud.TemplateSharePreview, error) {
	s.token = token
	return s.preview, nil
}
func (s *fakeShare) Deploy(_ context.Context, token string, b vertracloud.TemplateShareDeployBody, _ ...rest.RequestOpt) (vertracloud.TemplateDeploy, error) {
	s.token, s.deployed = token, b
	return vertracloud.TemplateDeploy{ID: "job-2", Status: vertracloud.TemplateDeployStatusQueued}, nil
}

func newClient(f *fakeTemplates) *vertracloud.Client { return &vertracloud.Client{Templates: f} }

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func zipNames(t *testing.T, data []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names
}

func withInput(t *testing.T, input string, tty bool) {
	t.Helper()
	oldIn, oldTTY, oldReader := stdin, interactive, reader
	stdin, interactive, reader = strings.NewReader(input), func() bool { return tty }, nil
	t.Cleanup(func() { stdin, interactive, reader = oldIn, oldTTY, oldReader })
}

func fast(t *testing.T) {
	t.Helper()
	old := pollEvery
	pollEvery = time.Millisecond
	t.Cleanup(func() { pollEvery = old })
}

func render(v any) string {
	var b bytes.Buffer
	ui.Render(&b, v, "template", "en")
	return b.String()
}

const manifestYAML = `id: demo
version: 1.0.0
name: { pt-br: Demo }
description: { pt-br: Teste }
category: starters
inputs:
  - id: token
    type: secret
    label: { pt-br: Token }
    required: true
services:
  app:
    source: { type: code, path: src }
    language: javascript
`

// ---------------------------------------------------------------------------
// manifest

func TestLineFindsTheKeyAndFallsBackToTheDeepestParent(t *testing.T) {
	m, err := ParseManifest("t.yaml", []byte(manifestYAML))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]int{
		"id":                  1,
		"inputs[0].id":        7,
		"inputs.0.type":       8,
		"services.app.source": 13,
		"services.app.ram":    12, // missing: the service that should carry it
		"nada.mais":           0,
	}
	for path, want := range cases {
		got := m.Line(path)
		if path == "nada.mais" {
			if got == 0 {
				t.Fatalf("%s: want the root line, got 0", path)
			}
			continue
		}
		if got != want {
			t.Errorf("Line(%q) = %d, want %d", path, got, want)
		}
	}
}

func TestParseManifestSyntaxErrorHasALine(t *testing.T) {
	_, err := ParseManifest("t.yaml", []byte("id: a\nservices: [\n"))
	var syn *SyntaxError
	if !errors.As(err, &syn) || syn.Line == 0 {
		t.Fatalf("err = %v", err)
	}
	if _, err := ParseManifest("t.yaml", []byte("- a\n- b\n")); err == nil {
		t.Fatal("a list is not a manifest")
	}
}

func TestParseManifestKeepsValuesAsJSON(t *testing.T) {
	m, err := ParseManifest("t.yaml", []byte("id: a\nversion: 1.0.0\nflag: on\nn: 3\nwhen: 2026-01-02\nmap:\n  1: x\n"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(m.Doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"version":"1.0.0"`) || !strings.Contains(string(raw), `"n":3`) || !strings.Contains(string(raw), `"1":"x"`) {
		t.Fatalf("json = %s", raw)
	}
}

func TestCodeServicesStayInsideTheFolder(t *testing.T) {
	dir := tree(t, map[string]string{"vertracloud.template.yaml": manifestYAML, "src/index.js": "x"})
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.CodeServices()
	if err != nil || len(got) != 1 || !strings.HasSuffix(got["app"], "src") {
		t.Fatalf("got=%v err=%v", got, err)
	}
	for _, path := range []string{"../out", "/etc", "missing"} {
		m.Doc["services"] = map[string]any{"app": map[string]any{"source": map[string]any{"type": "code", "path": path}}}
		if _, err := m.CodeServices(); err == nil {
			t.Errorf("path %q: expected an error", path)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err == nil {
		m.Doc["services"] = map[string]any{"app": map[string]any{"source": map[string]any{"type": "code", "path": "link"}}}
		if _, err := m.CodeServices(); err == nil {
			t.Error("a symlink out of the folder must be refused")
		}
	}
}

// ---------------------------------------------------------------------------
// validate

func TestValidateInvalidReturnsIssuesWithLines(t *testing.T) {
	dir := tree(t, map[string]string{"vertracloud.template.yaml": manifestYAML})
	f := &fakeTemplates{validate: vertracloud.TemplateValidation{Valid: false, Issues: []vertracloud.TemplateValidationIssue{
		{Path: "inputs[0].type", Code: vertracloud.TemplateValidationCodeInvalidType, Message: "bad type"},
		{Path: "services.app.ram", Code: vertracloud.TemplateValidationCodeRequired, Message: "ram is required"},
	}}}
	_, err := Run(context.Background(), newClient(f), "validate", []string{dir}, false, io.Discard)
	var invalid *ValidationError
	if !errors.As(err, &invalid) || invalid.Count != 2 {
		t.Fatalf("err = %v", err)
	}
	human := render(invalid.Result)
	if !strings.Contains(human, "vertracloud.template.yaml:8") || !strings.Contains(human, "inputs[0].type") || !strings.Contains(human, "invalid_type") {
		t.Fatalf("human output:\n%s", human)
	}
	raw, _ := json.Marshal(invalid.Result.JSON)
	if !strings.Contains(string(raw), `"line":8`) || !strings.Contains(string(raw), `"valid":false`) {
		t.Fatalf("json = %s", raw)
	}
	if f.validated == nil {
		t.Fatal("the manifest was not sent")
	}
}

func TestValidateValidPrintsSummary(t *testing.T) {
	dir := tree(t, map[string]string{"vertracloud.template.yaml": manifestYAML})
	plan, ram := vertracloud.UserPlanName("starter"), 512
	f := &fakeTemplates{validate: vertracloud.TemplateValidation{Valid: true, RequiredPlan: &plan, RAMMinTotal: &ram}}
	got, err := Run(context.Background(), newClient(f), "validate", []string{dir}, false, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if out := render(got); !strings.Contains(out, "valid") || !strings.Contains(out, "512 MB") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestValidateYAMLSyntaxIsAFindingNotACrash(t *testing.T) {
	dir := tree(t, map[string]string{"vertracloud.template.yaml": "id: a\nservices: [\n"})
	f := &fakeTemplates{}
	_, err := Run(context.Background(), newClient(f), "validate", []string{dir}, true, io.Discard)
	var invalid *ValidationError
	if !errors.As(err, &invalid) || f.validated != nil {
		t.Fatalf("err=%v validated=%v", err, f.validated)
	}
}

func TestValidateWithoutManifestFails(t *testing.T) {
	if _, err := Run(context.Background(), newClient(&fakeTemplates{}), "validate", []string{t.TempDir()}, true, io.Discard); err == nil {
		t.Fatal("expected an error")
	}
}

// ---------------------------------------------------------------------------
// create

func TestCreatePacksEachCodeServiceWithoutSecrets(t *testing.T) {
	dir := tree(t, map[string]string{
		"vertracloud.template.yaml": manifestYAML,
		"src/index.js":              "ok",
		"src/.env":                  "S=1",
		"src/.env.local":            "S=2",
		"src/.git/config":           "x",
		"src/skip.txt":              "x",
		"src/.vertraignore":         "skip.txt\n",
		"src/.dockerignore":         "index.js\n", // .vertraignore wins
	})
	f := &fakeTemplates{}
	if _, err := Run(context.Background(), newClient(f), "create", []string{dir, "--workspace", "ws-1", "--changelog", "primeira"}, true, io.Discard); err != nil {
		t.Fatal(err)
	}
	names := zipNames(t, f.uploaded["app"])
	want := []string{".dockerignore", ".vertraignore", "index.js"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("zip = %v, want %v", names, want)
	}
	if f.created.WorkspaceID != "ws-1" || f.created.Changelog == nil || f.created.Changelog.PtBR != "primeira" || f.created.Manifest == nil {
		t.Fatalf("params = %+v", f.created)
	}
}

func TestCreateFallsBackToDockerignore(t *testing.T) {
	dir := tree(t, map[string]string{"vertracloud.template.yaml": manifestYAML, "src/a.js": "x", "src/b.js": "x", "src/.dockerignore": "b.js\n"})
	f := &fakeTemplates{}
	if _, err := Run(context.Background(), newClient(f), "create", []string{dir}, true, io.Discard); err != nil {
		t.Fatal(err)
	}
	if names := zipNames(t, f.uploaded["app"]); strings.Join(names, ",") != ".dockerignore,a.js" {
		t.Fatalf("zip = %v", names)
	}
}

func TestCreateWithTemplateFlagPublishesANewVersion(t *testing.T) {
	dir := tree(t, map[string]string{"vertracloud.template.yaml": manifestYAML, "src/a.js": "x"})
	f := &fakeTemplates{}
	got, err := Run(context.Background(), newClient(f), "create", []string{dir, "--template", "tpl-1"}, true, io.Discard)
	if err != nil || f.versionID != "tpl-1" || f.created.Manifest != nil {
		t.Fatalf("err=%v id=%q", err, f.versionID)
	}
	if out := render(got); !strings.Contains(out, "1.1.0") {
		t.Fatalf("output:\n%s", out)
	}
	if _, err := Run(context.Background(), newClient(f), "create", []string{dir, "--template", "tpl-1", "--workspace", "w"}, true, io.Discard); err == nil {
		t.Fatal("--workspace with --template must fail")
	}
}

func TestCreateSizeCapStopsBeforeUpload(t *testing.T) {
	old := MaxSourceBytes
	MaxSourceBytes = 1 << 10
	defer func() { MaxSourceBytes = old }()
	big := make([]byte, 64<<10)
	for i := range big {
		big[i] = byte(i*7 + i>>3) // hard to compress
	}
	dir := tree(t, map[string]string{"vertracloud.template.yaml": manifestYAML, "src/big.bin": string(big)})
	f := &fakeTemplates{}
	_, err := Run(context.Background(), newClient(f), "create", []string{dir}, true, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "100 MB") || f.created.Manifest != nil {
		t.Fatalf("err=%v", err)
	}
}

func TestCreateManifestRejectedBecomesAListingWithLines(t *testing.T) {
	dir := tree(t, map[string]string{"vertracloud.template.yaml": manifestYAML, "src/a.js": "x"})
	f := &fakeTemplates{err: &rest.APIError{Status: 400, Code: "TEMPLATE_MANIFEST_INVALID", Details: map[string]any{
		"issues": []any{map[string]any{"path": "category", "code": "invalid_category", "message": "unknown category"}},
	}}}
	_, err := Run(context.Background(), newClient(f), "create", []string{dir}, true, io.Discard)
	var invalid *ValidationError
	if !errors.As(err, &invalid) || !strings.Contains(render(invalid.Result), "vertracloud.template.yaml:5") {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateSecretInSourceListsTheFiles(t *testing.T) {
	dir := tree(t, map[string]string{"vertracloud.template.yaml": manifestYAML, "src/a.js": "x"})
	f := &fakeTemplates{err: &rest.APIError{Status: 422, Code: "TEMPLATE_SECRET_IN_SOURCE", Details: map[string]any{"files": []any{"keys/a.pem"}}}}
	_, err := Run(context.Background(), newClient(f), "create", []string{dir}, true, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "keys/a.pem") {
		t.Fatalf("err = %v", err)
	}
}

// ---------------------------------------------------------------------------
// share links

func TestShareShowsTheTokenOnceAndPassesTheOptions(t *testing.T) {
	f := &fakeTemplates{}
	got, err := Run(context.Background(), newClient(f), "share", []string{"tpl-1", "--mode", "full_copy", "--max-uses", "3", "--expires-days", "7"}, false, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	b := f.links.body
	if b.Mode != vertracloud.TemplateShareLinkModeFullCopy || b.MaxUses != 3 || b.ExpiresInDays != 7 {
		t.Fatalf("body = %+v", b)
	}
	if out := render(got); !strings.Contains(out, "vtpl_secrettoken") {
		t.Fatalf("the token must be printed (not redacted):\n%s", out)
	}
	if _, err := Run(context.Background(), newClient(f), "share", []string{"tpl-1", "--mode", "x"}, true, io.Discard); err == nil {
		t.Fatal("bad mode must fail")
	}
}

func TestShareRevoke(t *testing.T) {
	f := &fakeTemplates{}
	if _, err := Run(context.Background(), newClient(f), "share", []string{"revoke", "tpl-1", "lnk-9"}, true, io.Discard); err != nil || f.links.revoked != [2]string{"tpl-1", "lnk-9"} {
		t.Fatalf("err=%v revoked=%v", err, f.links.revoked)
	}
}

// ---------------------------------------------------------------------------
// answers

func inputs() []vertracloud.TemplateInput {
	return []vertracloud.TemplateInput{
		{ID: "storage", Type: vertracloud.TemplateInputTypeSelect, Label: vertracloud.TemplateI18nText{PtBR: "Armazenamento"}, Default: "postgres",
			Options: []vertracloud.TemplateInputOption{{Value: "postgres"}, {Value: "sqlite"}}},
		{ID: "pg_password", Type: vertracloud.TemplateInputTypeText, Label: vertracloud.TemplateI18nText{PtBR: "Senha"}, Required: true,
			When: &vertracloud.TemplateWhen{Input: "storage", Equals: "postgres"}},
		{ID: "workers", Type: vertracloud.TemplateInputTypeNumber, Label: vertracloud.TemplateI18nText{PtBR: "Workers"}, Validation: &vertracloud.TemplateInputValidation{Min: ptr(1.0), Max: ptr(8.0)}},
		{ID: "debug", Type: vertracloud.TemplateInputTypeBoolean, Label: vertracloud.TemplateI18nText{PtBR: "Debug"}},
		{ID: "key", Type: vertracloud.TemplateInputTypeSecret, Label: vertracloud.TemplateI18nText{PtBR: "Chave"}, Generate: "random:32"},
	}
}

func ptr[T any](v T) *T { return &v }

func TestCollectAnswersNonInteractiveCoercesAndSkipsWhat(t *testing.T) {
	got, err := CollectAnswers(inputs(), map[string]string{"pg_password": "x", "workers": "4", "debug": "sim"}, false, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got["pg_password"] != "x" || got["workers"] != int64(4) || got["debug"] != true {
		t.Fatalf("got = %#v", got)
	}
	if _, ok := got["storage"]; ok {
		t.Fatal("an unanswered question with a default is left to the server")
	}
	if _, ok := got["key"]; ok {
		t.Fatal("a generated secret is not asked")
	}
}

func TestCollectAnswersMissingRequiredListsThem(t *testing.T) {
	_, err := CollectAnswers(inputs(), nil, false, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "pg_password") {
		t.Fatalf("err = %v", err)
	}
}

func TestCollectAnswersWhenFalseDropsTheQuestion(t *testing.T) {
	got, err := CollectAnswers(inputs(), map[string]string{"storage": "sqlite"}, false, io.Discard)
	if err != nil {
		t.Fatalf("pg_password only applies to postgres: %v", err)
	}
	if got["storage"] != "sqlite" {
		t.Fatalf("got = %#v", got)
	}
}

func TestCollectAnswersRejectsBadValuesAndUnknownKeys(t *testing.T) {
	for _, set := range []map[string]string{
		{"workers": "99", "pg_password": "x"},
		{"workers": "abc", "pg_password": "x"},
		{"storage": "mongo"},
		{"debug": "talvez", "pg_password": "x"},
		{"nope": "1", "pg_password": "x"},
	} {
		if _, err := CollectAnswers(inputs(), set, false, io.Discard); err == nil {
			t.Errorf("set %v: expected an error", set)
		}
	}
}

func TestCollectAnswersPromptsAndRetries(t *testing.T) {
	// storage: "2" (sqlite by number); workers: "zero" (bad) then "3"; debug: empty (default); pg_password skipped by when.
	withInput(t, "2\nzero\n3\n\n", true)
	var errOut bytes.Buffer
	got, err := CollectAnswers(inputs(), nil, true, &errOut)
	if err != nil {
		t.Fatal(err)
	}
	if got["storage"] != "sqlite" || got["workers"] != int64(3) {
		t.Fatalf("got = %#v", got)
	}
	if _, ok := got["debug"]; ok {
		t.Fatal("an empty answer keeps the default")
	}
	if !strings.Contains(errOut.String(), "Armazenamento") || !strings.Contains(errOut.String(), "1) postgres") {
		t.Fatalf("prompt output:\n%s", errOut.String())
	}
}

func TestCollectAnswersPromptReadsSecretsWithoutEcho(t *testing.T) {
	old := readSecret
	readSecret = func(string) (string, error) { return "s3cret", nil }
	defer func() { readSecret = old }()
	in := []vertracloud.TemplateInput{{ID: "token", Type: vertracloud.TemplateInputTypeSecret, Label: vertracloud.TemplateI18nText{PtBR: "Token"}, Required: true}}
	withInput(t, "", true)
	got, err := CollectAnswers(in, nil, true, io.Discard)
	if err != nil || got["token"] != "s3cret" {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestCollectAnswersPromptAtEOFFailsForRequired(t *testing.T) {
	in := []vertracloud.TemplateInput{{ID: "name", Type: vertracloud.TemplateInputTypeText, Label: vertracloud.TemplateI18nText{PtBR: "Nome"}, Required: true}}
	withInput(t, "", true)
	if _, err := CollectAnswers(in, nil, true, io.Discard); err == nil {
		t.Fatal("expected an error")
	}
}

// ---------------------------------------------------------------------------
// deploy

func ready() *fakeTemplates {
	return &fakeTemplates{
		detail: vertracloud.TemplateDetail{Template: vertracloud.Template{ID: "t1", Slug: "n8n"}, Inputs: inputs()},
		jobs: []vertracloud.TemplateDeploy{
			{ID: "job-1", Status: vertracloud.TemplateDeployStatusRunning, Steps: []vertracloud.TemplateDeployStepState{{Step: vertracloud.TemplateDeployStepApps, Status: vertracloud.TemplateDeployStepStatusRunning}}},
			{
				ID: "job-1", Status: vertracloud.TemplateDeployStatusSucceeded, Version: "1.0.0", ApplicationID: ptr("app-1"),
				Steps:     []vertracloud.TemplateDeployStepState{{Step: vertracloud.TemplateDeployStepApps, Status: vertracloud.TemplateDeployStepStatusDone}},
				Resources: []vertracloud.TemplateDeployResource{{ResourceType: "application", ResourceID: "app-1", Name: "meu-n8n"}},
				Outputs: []vertracloud.TemplateDeployOutput{
					{Label: vertracloud.TemplateI18nText{PtBR: "URL", EN: "URL"}, Value: json.RawMessage(`"https://x.example"`)},
					{Label: vertracloud.TemplateI18nText{PtBR: "Chave", EN: "Key"}, Value: json.RawMessage(`"k-123"`), Secret: true, Once: true},
				},
			},
		},
	}
}

func TestDeployYesSendsAnswersAndShowsOutputsOnce(t *testing.T) {
	fast(t)
	f := ready()
	f.detail.Services = []vertracloud.TemplateServiceSummary{{ID: "app"}}
	var errOut bytes.Buffer
	got, err := Run(context.Background(), newClient(f), "deploy", []string{"n8n", "--name", "meu-n8n", "--set", "pg_password=abc", "--set", "workers=2", "--yes", "--service", "app.ram=2048", "--version", "1.0.0"}, false, &errOut)
	if err != nil {
		t.Fatal(err)
	}
	b := f.deployed
	if f.deploySlg != "n8n" || b.Name != "meu-n8n" || b.Version != "1.0.0" || b.Inputs["pg_password"] != "abc" || b.Services["app"].RAM != 2048 {
		t.Fatalf("slug=%q body=%+v", f.deploySlg, b)
	}
	if len(b.IdempotencyKey) != 32 {
		t.Fatalf("idempotency key = %q", b.IdempotencyKey)
	}
	out := render(got)
	for _, want := range []string{"https://x.example", "k-123", "meu-n8n", "only now"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(errOut.String(), "Apps") {
		t.Fatalf("progress:\n%s", errOut.String())
	}
}

func TestDeployJSONReturnsTheJobAndStaysQuiet(t *testing.T) {
	fast(t)
	f := ready()
	var errOut bytes.Buffer
	got, err := Run(context.Background(), newClient(f), "deploy", []string{"n8n", "--name", "x", "--set", "pg_password=a"}, true, &errOut)
	if err != nil {
		t.Fatal(err)
	}
	p := got.(ui.Presentation)
	job := p.JSON.(vertracloud.TemplateDeploy)
	if job.Status != vertracloud.TemplateDeployStatusSucceeded || len(job.Outputs) != 2 {
		t.Fatalf("job = %+v", job)
	}
	if strings.Contains(errOut.String(), "Apps") {
		t.Fatalf("JSON mode must not print progress:\n%s", errOut.String())
	}
}

func TestDeployNonInteractiveNeedsNameAndAnswers(t *testing.T) {
	f := ready()
	withInput(t, "", false)
	if _, err := Run(context.Background(), newClient(f), "deploy", []string{"n8n", "--set", "pg_password=a"}, false, io.Discard); err == nil {
		t.Fatal("--name is required without a terminal")
	}
	if _, err := Run(context.Background(), newClient(f), "deploy", []string{"n8n", "--name", "x"}, false, io.Discard); err == nil || !strings.Contains(err.Error(), "pg_password") {
		t.Fatalf("missing answers: %v", err)
	}
	if f.deploySlg != "" {
		t.Fatal("nothing may be deployed")
	}
}

func TestDeployInteractiveAsksTheQuestions(t *testing.T) {
	fast(t)
	f := ready()
	withInput(t, "\n\nsenha\n3\n\n", true) // name (default slug), storage (default), pg_password, workers, debug
	var errOut bytes.Buffer
	if _, err := Run(context.Background(), newClient(f), "deploy", []string{"n8n"}, false, &errOut); err != nil {
		t.Fatal(err)
	}
	b := f.deployed
	if b.Name != "n8n" || b.Inputs["pg_password"] != "senha" || b.Inputs["workers"] != int64(3) {
		t.Fatalf("body = %+v", b)
	}
}

func TestDeployFailedJobBecomesAnError(t *testing.T) {
	fast(t)
	f := ready()
	step := vertracloud.TemplateDeployStepApps
	f.jobs = []vertracloud.TemplateDeploy{{ID: "job-1", Status: vertracloud.TemplateDeployStatusFailed, Error: &vertracloud.TemplateDeployError{Code: "TEMPLATE_RAM_INSUFFICIENT", Message: "no ram", Step: &step}}}
	_, err := Run(context.Background(), newClient(f), "deploy", []string{"n8n", "--name", "x", "--yes", "--set", "pg_password=a"}, true, io.Discard)
	apiErr, ok := rest.AsAPIError(err)
	if !ok || apiErr.Code != "TEMPLATE_RAM_INSUFFICIENT" || !strings.Contains(apiErr.Message, "memory") {
		t.Fatalf("err = %v", err)
	}
}

func TestDeployNoWaitReturnsTheJobRightAway(t *testing.T) {
	f := ready()
	if _, err := Run(context.Background(), newClient(f), "deploy", []string{"n8n", "--name", "x", "--yes", "--set", "pg_password=a", "--no-wait"}, true, io.Discard); err != nil || f.gets != 0 {
		t.Fatalf("err=%v gets=%d", err, f.gets)
	}
}

func TestDeployRejectsBadServiceFlag(t *testing.T) {
	f := ready()
	f.detail.Services = []vertracloud.TemplateServiceSummary{{ID: "app"}}
	for _, svc := range []string{"app", "app.ram=abc", "nope.ram=1", "app.color=red"} {
		if _, err := Run(context.Background(), newClient(f), "deploy", []string{"n8n", "--name", "x", "--yes", "--set", "pg_password=a", "--service", svc}, true, io.Discard); err == nil {
			t.Errorf("--service %s: expected an error", svc)
		}
	}
}

func TestDeployThroughAShareLink(t *testing.T) {
	fast(t)
	f := ready()
	f.share = &fakeShare{preview: vertracloud.TemplateSharePreview{Mode: vertracloud.TemplateShareLinkModeUse, Inputs: inputs()}}
	_, err := Run(context.Background(), newClient(f), "deploy", []string{"--share", "https://vertracloud.app/t/vtpl_abc?x=1", "--name", "n", "--yes", "--set", "pg_password=a"}, true, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if f.share.token != "vtpl_abc" || f.share.deployed.Name != "n" || f.deploySlg != "" {
		t.Fatalf("token=%q deployed=%+v", f.share.token, f.share.deployed)
	}
	f.share.preview.Mode = vertracloud.TemplateShareLinkModeFullCopy
	if _, err := Run(context.Background(), newClient(f), "deploy", []string{"--share", "vtpl_abc", "--name", "n", "--yes"}, true, io.Discard); err == nil {
		t.Fatal("a copy link is not a deploy")
	}
}

// ---------------------------------------------------------------------------
// catalog

func TestListPassesFiltersAndPrintsRows(t *testing.T) {
	f := ready()
	f.detail.Template = vertracloud.Template{Slug: "n8n", Name: vertracloud.TemplateI18nText{PtBR: "n8n"}, Category: "automation", Origin: "vertra",
		CurrentVersion: vertracloud.TemplateVersionSummary{Version: "1.0.0"}, RequiredPlan: "starter", RAMMinTotal: 1024}
	var errOut bytes.Buffer
	got, err := Run(context.Background(), newClient(f), "list", []string{"--search", "auto", "--category", "automation", "--fits", "--limit", "5", "--page", "1"}, false, &errOut)
	if err != nil {
		t.Fatal(err)
	}
	p := f.listed
	if p.Search != "auto" || p.Category != "automation" || p.Fits == nil || !*p.Fits || p.Limit != 5 {
		t.Fatalf("params = %+v", p)
	}
	if out := render(got); !strings.Contains(out, "n8n") || !strings.Contains(out, "1024 MB") {
		t.Fatalf("output:\n%s", out)
	}
	if !strings.Contains(errOut.String(), "--page 2") {
		t.Fatalf("page hint:\n%s", errOut.String())
	}
	f2 := ready()
	if _, err := Run(context.Background(), newClient(f2), "list", nil, true, io.Discard); err != nil || f2.listed.Fits != nil {
		t.Fatalf("--fits must stay unset when not given: %v", err)
	}
}

func TestInfoPrintsServicesAndQuestions(t *testing.T) {
	f := ready()
	f.detail.Name = vertracloud.TemplateI18nText{PtBR: "n8n"}
	f.detail.Services = []vertracloud.TemplateServiceSummary{{ID: "app", Kind: "app", RAM: vertracloud.TemplateRAM{Min: 1024}}}
	f.detail.Account = &vertracloud.TemplateAccountContext{Fits: false, Reasons: []vertracloud.TemplateFitIssue{{Reason: vertracloud.TemplateFitReasonRAM}}}
	got, err := Run(context.Background(), newClient(f), "info", []string{"n8n"}, false, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	out := render(got)
	for _, want := range []string{"app (app, 1024 MB)", "pg_password", "not enough memory"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestUnknownCommandAndFriendlyErrors(t *testing.T) {
	if _, err := Run(context.Background(), newClient(&fakeTemplates{}), "bogus", nil, true, io.Discard); err == nil {
		t.Fatal("expected an error")
	}
	f := ready()
	f.err = &rest.APIError{Status: 409, Code: "SHARE_LINK_EXPIRED", Message: "raw"}
	_, err := Run(context.Background(), newClient(f), "info", []string{"x"}, true, io.Discard)
	apiErr, _ := rest.AsAPIError(err)
	if apiErr == nil || apiErr.Message == "raw" || apiErr.Code != "SHARE_LINK_EXPIRED" {
		t.Fatalf("err = %v", err)
	}
}
