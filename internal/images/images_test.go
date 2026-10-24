package images

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
