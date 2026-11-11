package images

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

type fakeImages struct {
	vertracloud.ImagesService
	image     vertracloud.CustomImage
	deleted   string
	confirm   string
	created   vertracloud.ImageCreateParams
	rebuilt   vertracloud.ImageRebuildParams
	rebuiltID string
	updatedID string
	updated   vertracloud.ImageUpdateBody
	updates   int
	uploaded  []byte
	err       error
}

func (f *fakeImages) Get(context.Context, string, ...rest.RequestOpt) (vertracloud.CustomImage, error) {
	return f.image, f.err
}
func (f *fakeImages) List(context.Context, ...rest.RequestOpt) ([]vertracloud.CustomImage, error) {
	return []vertracloud.CustomImage{f.image}, nil
}
func (f *fakeImages) Delete(_ context.Context, id string, b vertracloud.ImageDeleteBody, _ ...rest.RequestOpt) (vertracloud.ImageDeleteResult, error) {
	f.deleted, f.confirm = id, b.ConfirmName
	return vertracloud.ImageDeleteResult{DeletedApps: []string{"app-1"}}, nil
}
func (f *fakeImages) Create(_ context.Context, p vertracloud.ImageCreateParams, _ ...rest.RequestOpt) (vertracloud.CustomImage, error) {
	f.created = p
	f.uploaded, _ = io.ReadAll(p.File)
	return vertracloud.CustomImage{ID: "img-1", Name: p.Name, Status: vertracloud.CustomImageStatusBuilding}, f.err
}
func (f *fakeImages) Update(_ context.Context, id string, b vertracloud.ImageUpdateBody, _ ...rest.RequestOpt) (vertracloud.CustomImage, error) {
	f.updatedID, f.updated = id, b
	f.updates++
	return vertracloud.CustomImage{ID: id, Name: "api-base"}, f.err
}
func (f *fakeImages) Rebuild(_ context.Context, id string, p vertracloud.ImageRebuildParams, _ ...rest.RequestOpt) (vertracloud.CustomImage, error) {
	f.rebuiltID, f.rebuilt = id, p
	f.uploaded, _ = io.ReadAll(p.File)
	return vertracloud.CustomImage{ID: id, Status: vertracloud.CustomImageStatusBuilding}, f.err
}

type fakeApps struct {
	vertracloud.AppsService
	got vertracloud.ApplicationCreateFromImageParams
}

func (f *fakeApps) CreateFromImage(_ context.Context, p vertracloud.ApplicationCreateFromImageParams, _ ...rest.RequestOpt) (vertracloud.Application, error) {
	f.got = p
	return vertracloud.Application{ID: "app-9", Name: p.Name}, nil
}

func newClient(im *fakeImages) *vertracloud.Client {
	return &vertracloud.Client{Images: im, Apps: &fakeApps{}}
}

func withStdin(t *testing.T, input string, tty bool) {
	t.Helper()
	oldIn, oldTTY := stdin, interactive
	stdin, interactive = strings.NewReader(input), func() bool { return tty }
	t.Cleanup(func() { stdin, interactive = oldIn, oldTTY })
}

func TestDeleteYesSendsTheImageName(t *testing.T) {
	im := &fakeImages{image: vertracloud.CustomImage{ID: "img-1", Name: "api-base"}}
	got, err := Run(context.Background(), newClient(im), "delete", []string{"img-1", "-y"}, true)
	if err != nil || im.deleted != "img-1" || im.confirm != "api-base" {
		t.Fatalf("got=%v err=%v deleted=%q confirm=%q", got, err, im.deleted, im.confirm)
	}
}

func TestDeleteWithoutYesAsksForTheName(t *testing.T) {
	im := &fakeImages{image: vertracloud.CustomImage{ID: "img-1", Name: "api-base"}}
	withStdin(t, "api-base\n", true)
	if _, err := Run(context.Background(), newClient(im), "delete", []string{"img-1"}, false); err != nil || im.deleted != "img-1" {
		t.Fatalf("err=%v deleted=%q", err, im.deleted)
	}
}

func TestDeleteWrongNameCancels(t *testing.T) {
	im := &fakeImages{image: vertracloud.CustomImage{ID: "img-1", Name: "api-base"}}
	withStdin(t, "yes\n", true)
	_, err := Run(context.Background(), newClient(im), "delete", []string{"img-1"}, false)
	if !errors.Is(err, ErrCancelled) || im.deleted != "" {
		t.Fatalf("err=%v deleted=%q", err, im.deleted)
	}
}

func TestDeleteWithoutYesInJSONOrNonInteractiveFails(t *testing.T) {
	im := &fakeImages{image: vertracloud.CustomImage{ID: "img-1", Name: "api-base"}}
	withStdin(t, "api-base\n", true)
	if _, err := Run(context.Background(), newClient(im), "delete", []string{"img-1"}, true); err == nil || im.deleted != "" {
		t.Fatalf("json without --yes must fail: %v", err)
	}
	withStdin(t, "api-base\n", false)
	if _, err := Run(context.Background(), newClient(im), "delete", []string{"img-1"}, false); err == nil || im.deleted != "" {
		t.Fatalf("non-interactive without --yes must fail: %v", err)
	}
}

func TestBuildPacksFolderAndSendsName(t *testing.T) {
	dir := tree(t, map[string]string{"Dockerfile": "FROM scratch\n", ".env": "S=1"})
	im := &fakeImages{}
	if _, err := Run(context.Background(), newClient(im), "build", []string{"--name", "web", "--context", dir, "--workspace", "ws-1"}, true); err != nil {
		t.Fatal(err)
	}
	if im.created.Name != "web" || im.created.WorkspaceID != "ws-1" || !strings.HasSuffix(im.created.FileName, ".zip") || len(im.uploaded) == 0 {
		t.Fatalf("created=%+v", im.created)
	}
	if strings.Contains(string(im.uploaded), ".env") {
		t.Fatal(".env was uploaded")
	}
}

func TestBuildSendsDescriptionOnlyWhenGiven(t *testing.T) {
	dir := tree(t, map[string]string{"Dockerfile": "FROM scratch\n"})
	im := &fakeImages{}
	if _, err := Run(context.Background(), newClient(im), "build", []string{"--name", "web", "--context", dir, "--description", "Node 22 base"}, true); err != nil {
		t.Fatal(err)
	}
	if im.created.Description != "Node 22 base" {
		t.Fatalf("description = %q", im.created.Description)
	}
	if _, err := Run(context.Background(), newClient(im), "build", []string{"--name", "web", "--context", dir}, true); err != nil {
		t.Fatal(err)
	}
	if im.created.Description != "" {
		t.Fatalf("description = %q, want none", im.created.Description)
	}
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUpdateSendsOnlyTheGivenFields(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"name", []string{"img-1", "--name", "api-v2"}, `{"name":"api-v2"}`},
		{"description", []string{"img-1", "--description", "Base Node 22"}, `{"description":"Base Node 22"}`},
		{"both", []string{"img-1", "--name", "api-v2", "--description=Base"}, `{"name":"api-v2","description":"Base"}`},
		{"empty description clears", []string{"img-1", "--description", ""}, `{"description":null}`},
		{"flags before the id", []string{"--name", "api-v2", "img-1"}, `{"name":"api-v2"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			im := &fakeImages{}
			if _, err := Run(context.Background(), newClient(im), "update", tc.args, true); err != nil {
				t.Fatal(err)
			}
			if im.updatedID != "img-1" || jsonOf(t, im.updated) != tc.want {
				t.Fatalf("id=%q body=%s, want %s", im.updatedID, jsonOf(t, im.updated), tc.want)
			}
		})
	}
}

func TestUpdateNeedsAFieldAndOneID(t *testing.T) {
	im := &fakeImages{}
	for _, args := range [][]string{
		{"img-1"},
		{"--name", "x"},
		{"img-1", "img-2", "--name", "x"},
		{"img-1", "--name", "  "},
	} {
		if _, err := Run(context.Background(), newClient(im), "update", args, true); err == nil {
			t.Fatalf("args %v: expected error", args)
		}
	}
	if im.updates != 0 {
		t.Fatalf("API was called %d times", im.updates)
	}
}

func TestRenameIsUpdateOfTheName(t *testing.T) {
	im := &fakeImages{}
	if _, err := Run(context.Background(), newClient(im), "rename", []string{"img-1", "api-v2"}, true); err != nil {
		t.Fatal(err)
	}
	if im.updatedID != "img-1" || jsonOf(t, im.updated) != `{"name":"api-v2"}` {
		t.Fatalf("id=%q body=%s", im.updatedID, jsonOf(t, im.updated))
	}
	for _, args := range [][]string{{"img-1"}, {"img-1", "a", "b"}} {
		if _, err := Run(context.Background(), newClient(im), "rename", args, true); err == nil {
			t.Fatalf("args %v: expected error", args)
		}
	}
}

func TestListShowsDescription(t *testing.T) {
	desc := "Base Node 22"
	im := &fakeImages{image: vertracloud.CustomImage{ID: "img-1", Name: "api", Description: &desc}}
	out, err := Run(context.Background(), newClient(im), "list", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	rows := out.(ui.Presentation).Human.([]map[string]any)
	if rows[0]["description"] != desc {
		t.Fatalf("description = %v", rows[0]["description"])
	}
}

func TestBuildNeedsName(t *testing.T) {
	if _, err := Run(context.Background(), newClient(&fakeImages{}), "build", []string{"--context", "."}, true); err == nil {
		t.Fatal("expected error")
	}
}

func TestRebuildUploadsGivenZipAsIs(t *testing.T) {
	zp := t.TempDir() + "/ctx.zip"
	if err := os.WriteFile(zp, []byte("PK-not-really"), 0o644); err != nil {
		t.Fatal(err)
	}
	im := &fakeImages{}
	if _, err := Run(context.Background(), newClient(im), "rebuild", []string{"img-1", "--context", zp}, true); err != nil {
		t.Fatal(err)
	}
	if im.rebuiltID != "img-1" || im.rebuilt.FileName != "ctx.zip" || string(im.uploaded) != "PK-not-really" {
		t.Fatalf("id=%q params=%+v", im.rebuiltID, im.rebuilt)
	}
}

// fakeREST is a real REST client pointed at a local test server that records
// the request `rebuild` without a context sends.
type fakeREST struct {
	rest.Client
	method, path, rawQuery, contentType string
	body                                []byte
	calls                               int
	status                              int
	reply                               string
}

func newFakeREST(t *testing.T, status int, reply string) *fakeREST {
	t.Helper()
	f := &fakeREST{status: status, reply: reply}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls++
		f.method, f.path, f.rawQuery, f.contentType = r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Content-Type")
		f.body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.status)
		_, _ = io.WriteString(w, f.reply)
	}))
	t.Cleanup(srv.Close)
	c, err := rest.NewClient("test-key", rest.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	f.Client = c
	return f
}

func TestRebuildWithoutContextCallsTheSourceRoute(t *testing.T) {
	im := &fakeImages{}
	rc := newFakeREST(t, 202, `{"response":{"id":"img-1","status":"building"}}`)
	data, err := RunWithREST(context.Background(), newClient(im), rc, "rebuild", []string{"img-1", "--from-source", "--workspace", "ws-7"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if rc.calls != 1 || rc.method != http.MethodPost || rc.path != "/v1/images/img-1/rebuild" {
		t.Fatalf("call: %d %s %s", rc.calls, rc.method, rc.path)
	}
	if len(rc.body) != 0 || rc.contentType != "" {
		t.Fatalf("expected no body: %q %q", rc.body, rc.contentType)
	}
	if rc.rawQuery != "workspace_id=ws-7" {
		t.Fatalf("query = %q", rc.rawQuery)
	}
	if im.rebuiltID != "" {
		t.Fatal("must not use the upload route")
	}
	if image, ok := data.(vertracloud.CustomImage); !ok || image.ID != "img-1" || image.Status != vertracloud.CustomImageStatusBuilding {
		t.Fatalf("data = %#v", data)
	}
}

func TestRebuildWithoutContextOmitsWorkspaceWhenNotGiven(t *testing.T) {
	rc := newFakeREST(t, 202, `{"response":{"id":"img-1","status":"building"}}`)
	if _, err := RunWithREST(context.Background(), newClient(&fakeImages{}), rc, "rebuild", []string{"img-1", "--from-source"}, true); err != nil {
		t.Fatal(err)
	}
	if rc.rawQuery != "" {
		t.Fatalf("query = %q", rc.rawQuery)
	}
}

func TestRebuildWithoutContextFriendlyError(t *testing.T) {
	rc := newFakeREST(t, 409, `{"code":"IMAGE_TEMPLATE_ONLY","message":"raw"}`)
	_, err := RunWithREST(context.Background(), newClient(&fakeImages{}), rc, "rebuild", []string{"img-1", "--from-source"}, true)
	var apiErr *rest.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "IMAGE_TEMPLATE_ONLY" || apiErr.Message == "raw" {
		t.Fatalf("err = %v", err)
	}
}

func TestRebuildWithoutContextNeedsTheRESTClient(t *testing.T) {
	if _, err := Run(context.Background(), newClient(&fakeImages{}), "rebuild", []string{"img-1", "--from-source"}, true); err == nil {
		t.Fatal("expected error")
	}
}

func TestRebuildWithContextDoesNotCallTheSourceRoute(t *testing.T) {
	zp := t.TempDir() + "/ctx.zip"
	if err := os.WriteFile(zp, []byte("PK"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc := newFakeREST(t, 202, `{}`)
	if _, err := RunWithREST(context.Background(), newClient(&fakeImages{}), rc, "rebuild", []string{"img-1", "--context", zp}, true); err != nil {
		t.Fatal(err)
	}
	if rc.calls != 0 {
		t.Fatal("source route called")
	}
}

func TestRebuildWithoutFlagsUploadsTheCurrentFolder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/Dockerfile", []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(prev) }()
	im := &fakeImages{}
	rc := newFakeREST(t, 202, `{}`)
	if _, err := RunWithREST(context.Background(), newClient(im), rc, "rebuild", []string{"img-1"}, true); err != nil {
		t.Fatal(err)
	}
	if im.rebuiltID != "img-1" || len(im.uploaded) == 0 {
		t.Fatalf("id=%q uploaded=%d bytes", im.rebuiltID, len(im.uploaded))
	}
	if rc.calls != 0 {
		t.Fatal("source route called")
	}
}

func TestRebuildFromSourceWithContextIsAUsageError(t *testing.T) {
	for _, loc := range []string{"pt", "en", "es"} {
		ui.Locale = loc
		for _, args := range [][]string{
			{"img-1", "--from-source", "--context", "."},
			{"img-1", "--context=.", "--from-source"},
		} {
			im := &fakeImages{}
			rc := newFakeREST(t, 202, `{}`)
			_, err := RunWithREST(context.Background(), newClient(im), rc, "rebuild", args, true)
			if err == nil || err.Error() != text("from_source_with_context") || !strings.Contains(err.Error(), "--from-source") {
				t.Fatalf("%s %v: err = %v", loc, args, err)
			}
			if rc.calls != 0 || im.rebuiltID != "" {
				t.Fatalf("%s: API was called", loc)
			}
		}
	}
	ui.Locale = "en"
}

func TestFriendlySourceAndGitHubErrors(t *testing.T) {
	defer func() { ui.Locale = "en" }()
	msgOf := func(err error) string {
		var apiErr *rest.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("not an APIError: %v", err)
		}
		return apiErr.Message
	}
	for _, loc := range []string{"pt", "en", "es"} {
		ui.Locale = loc
		generic := msgOf(friendly(&rest.APIError{Status: 409, Code: "IMAGE_NOT_READY", Message: "raw"}))
		src := msgOf(friendlyFor(&rest.APIError{Status: 409, Code: "IMAGE_NOT_READY", Message: "raw"}, true))
		if generic != text("IMAGE_NOT_READY") || src != text("IMAGE_NOT_READY_SOURCE") || src == generic || !strings.Contains(src, "--from-source") {
			t.Fatalf("%s: generic=%q src=%q", loc, generic, src)
		}
		// already localized for the command: friendly must keep it
		if got := msgOf(friendly(friendlyFor(&rest.APIError{Status: 409, Code: "IMAGE_NOT_READY"}, true))); got != src {
			t.Fatalf("%s: overwritten: %q", loc, got)
		}
		m404 := msgOf(friendly(&rest.APIError{Status: 404, Code: "FETCH_ZIP_ERROR", Message: "raw"}))
		m502 := msgOf(friendly(&rest.APIError{Status: 502, Code: "FETCH_ZIP_ERROR", Message: "raw"}))
		inst := msgOf(friendly(&rest.APIError{Status: 404, Code: "INSTALLATION_NOT_FOUND", Message: "raw"}))
		if m404 != text("FETCH_ZIP_ERROR_404") || m502 != text("FETCH_ZIP_ERROR_502") || inst != text("INSTALLATION_NOT_FOUND") || m404 == m502 {
			t.Fatalf("%s: %q %q %q", loc, m404, m502, inst)
		}
	}
	ui.Locale = "en"
	other := &rest.APIError{Status: 500, Code: "FETCH_ZIP_ERROR", Message: "raw"}
	if friendly(other) != error(other) {
		t.Fatal("unmapped status must pass through")
	}
}

func TestDeployMapsFlags(t *testing.T) {
	apps := &fakeApps{}
	client := &vertracloud.Client{Images: &fakeImages{}, Apps: apps}
	_, err := Run(context.Background(), client, "deploy", []string{"img-1", "--name", "site", "--ram", "512", "--web", "--port", "3000", "--env", "A=1", "--env", "B=x=y", "--subdomain", "site"}, true)
	if err != nil {
		t.Fatal(err)
	}
	g := apps.got
	if g.ImageID != "img-1" || g.Name != "site" || g.Memory != 512 || !g.Web || g.Port == nil || *g.Port != 3000 || g.Subdomain != "site" || len(g.Envs) != 2 || g.Envs[1].Value != "x=y" {
		t.Fatalf("params=%+v", g)
	}
}

func TestDeployUsesSuggestedPortWhenPortIsOmitted(t *testing.T) {
	port := 4000
	apps := &fakeApps{}
	client := &vertracloud.Client{Images: &fakeImages{image: vertracloud.CustomImage{ID: "img-1", SuggestedPort: &port}}, Apps: apps}
	if _, err := Run(context.Background(), client, "deploy", []string{"img-1", "--name", "site", "--ram", "512", "--web"}, true); err != nil {
		t.Fatal(err)
	}
	if apps.got.Port == nil || *apps.got.Port != 4000 {
		t.Fatalf("port = %v, want the suggested 4000", apps.got.Port)
	}
}

func TestDeployWithoutWebDoesNotSendPort(t *testing.T) {
	port := 4000
	apps := &fakeApps{}
	client := &vertracloud.Client{Images: &fakeImages{image: vertracloud.CustomImage{ID: "img-1", SuggestedPort: &port}}, Apps: apps}
	if _, err := Run(context.Background(), client, "deploy", []string{"img-1", "--name", "bot", "--ram", "512"}, true); err != nil {
		t.Fatal(err)
	}
	if apps.got.Port != nil {
		t.Fatalf("port = %v, want none without --web", *apps.got.Port)
	}
}

func TestListShowsSuggestedPort(t *testing.T) {
	port := 3000
	im := &fakeImages{image: vertracloud.CustomImage{ID: "img-1", Name: "api", Status: vertracloud.CustomImageStatusSuccess, SuggestedPort: &port}}
	out, err := Run(context.Background(), newClient(im), "list", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	rows, ok := out.(ui.Presentation).Human.([]map[string]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("human rows = %#v", out)
	}
	if rows[0]["suggested_port"] != 3000 {
		t.Fatalf("suggested_port = %v, want 3000", rows[0]["suggested_port"])
	}
}

func TestDeployRejectsPortWithoutWebAndMissingFlags(t *testing.T) {
	client := newClient(&fakeImages{})
	for _, args := range [][]string{
		{"img-1", "--name", "x", "--ram", "256", "--port", "80"},
		{"img-1", "--name", "x"},
		{"img-1", "--ram", "256"},
		{"img-1", "--name", "x", "--ram", "256", "--env", "BAD"},
		{"--name", "x", "--ram", "256"},
	} {
		if _, err := Run(context.Background(), client, "deploy", args, true); err == nil {
			t.Errorf("expected error for %v", args)
		}
	}
}

func TestFriendlyImageAndDockerfileErrors(t *testing.T) {
	ui.Locale = "en"
	err := friendly(&rest.APIError{Status: 409, Code: "IMAGE_BUILD_IN_PROGRESS", Message: "raw"})
	var apiErr *rest.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "IMAGE_BUILD_IN_PROGRESS" || !strings.Contains(apiErr.Message, "already running") {
		t.Fatalf("got %v", err)
	}
	refused := friendly(&rest.APIError{Status: 400, Code: "DOCKERFILE_REFUSED", Message: "raw", Details: map[string]any{
		"issues": []any{map[string]any{"code": "run_network_host", "line": float64(4)}},
	}})
	if !errors.As(refused, &apiErr) || !strings.Contains(apiErr.Message, "line 4") || !strings.Contains(apiErr.Message, "--network=host") {
		t.Fatalf("got %v", refused)
	}
	other := &rest.APIError{Status: 500, Code: "SOMETHING", Message: "raw"}
	if friendly(other) != error(other) {
		t.Fatal("unrelated errors must pass through")
	}
	if got := friendly(ErrContextTooLarge); !strings.Contains(got.Error(), "100 MB") {
		t.Fatalf("got %v", got)
	}
}

func TestIssueLabelsCoverEveryCode(t *testing.T) {
	codes := []vertracloud.DockerfileIssueCode{
		vertracloud.DockerfileIssueCodeMissingDockerfile, vertracloud.DockerfileIssueCodeSyntaxFrontend,
		vertracloud.DockerfileIssueCodeRunSecurityInsecure, vertracloud.DockerfileIssueCodeRunNetworkHost,
		vertracloud.DockerfileIssueCodeMountSSH, vertracloud.DockerfileIssueCodeMountSecret,
		vertracloud.DockerfileIssueCodeWorkdirApp, vertracloud.DockerfileIssueCodeEnvFileInContext,
		vertracloud.DockerfileIssueCodeCopyEnvFile, vertracloud.DockerfileIssueCodeVolumeOutsideApp,
		vertracloud.DockerfileIssueCodeVolumeRefused,
	}
	for locale, m := range messages {
		for _, c := range codes {
			if m[string(c)] == "" {
				t.Errorf("locale %s: no label for %s", locale, c)
			}
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	if _, err := Run(context.Background(), newClient(&fakeImages{}), "nope", nil, true); err == nil {
		t.Fatal("expected error")
	}
}
