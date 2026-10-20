package apps

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"github.com/vertracloud/cli/internal/ui"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

func TestFlagsAfterIDAndShortAlias(t *testing.T) {
	f := fs("test")
	yes := f.Bool("yes", false, "")
	pos, err := parse(f, []string{"app-1", "-y"}, nil, map[string]bool{"--yes": true, "-y": true})
	if err != nil || len(pos) != 1 || pos[0] != "app-1" || !*yes {
		t.Fatalf("parse returned pos=%v yes=%v err=%v", pos, *yes, err)
	}
}

func TestPresentAppsIncludesSavedFoldersAndFavorites(t *testing.T) {
	apps := []vertracloud.Application{{ID: "app-1", Name: "demo"}}
	organization := vertracloud.WorkspaceResourceOrganization{
		Folders:   []vertracloud.WorkspaceResourceFolder{{Name: "Production", Resources: []vertracloud.WorkspaceFolderItem{{WorkspaceResourceRef: vertracloud.WorkspaceResourceRef{ResourceType: vertracloud.WorkspaceResourceTypeApplication, ResourceID: "app-1"}}}}},
		Favorites: []vertracloud.WorkspaceFavorite{{WorkspaceResourceRef: vertracloud.WorkspaceResourceRef{ResourceType: vertracloud.WorkspaceResourceTypeApplication, ResourceID: "app-1"}}},
	}
	rows := presentApps(apps, organization)
	if len(rows) != 1 || rows[0]["folder"] != "Production" || rows[0]["favorite"] != true {
		t.Fatalf("human rows = %#v", rows)
	}
}

func TestEnvironmentParserPreservesEqualsInValue(t *testing.T) {
	got, err := envPairs([]string{"TOKEN=a=b=c"})
	if err != nil || len(got) != 1 || got[0].Key != "TOKEN" || got[0].Value != "a=b=c" {
		t.Fatalf("got %#v err=%v", got, err)
	}
	if _, err := envPairs([]string{"missing-equals"}); err == nil {
		t.Fatal("expected malformed pair error")
	}
}

func TestConfirmDefaultsToNo(t *testing.T) {
	ok, err := confirm(strings.NewReader("no\n"), "")
	if err != nil || ok {
		t.Fatalf("got ok=%v err=%v", ok, err)
	}
	ok, err = confirm(strings.NewReader("yes\n"), "")
	if err != nil || !ok {
		t.Fatalf("got ok=%v err=%v", ok, err)
	}
}

func TestDeleteYesUsesSDKAndReturnsID(t *testing.T) {
	apps := &fakeApps{}
	client := &vertracloud.Client{Apps: apps}
	got, err := Run(context.Background(), client, "delete", []string{"app-1", "--yes"})
	if err != nil || !reflect.DeepEqual(got, map[string]string{"id": "app-1"}) || apps.deleted != "app-1" {
		t.Fatalf("got=%#v err=%v deleted=%q", got, err, apps.deleted)
	}
}

func TestUploadPromptsFailClosedWhenNonInteractive(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(original)
	t.Setenv("CI", "1")
	apps := &fakeApps{}
	if _, err := Run(context.Background(), &vertracloud.Client{Apps: apps}, "upload", nil); err == nil {
		t.Fatal("expected missing interactive upload input error")
	}
	if apps.createCalls != 0 {
		t.Fatalf("create calls = %d, want 0", apps.createCalls)
	}
	if _, err := RunWithOptions(context.Background(), &vertracloud.Client{Apps: apps}, "upload", nil, true); err == nil || err.Error() != "upload requires --name, --memory, and --main" {
		t.Fatalf("JSON upload error = %v, want missing options without prompting", err)
	}
}

// sseBody is a rest.Client whose streamed responses are a fixed SSE body.
type sseBody string

func (b sseBody) Do(context.Context, string, string, url.Values, io.Reader, string, ...rest.RequestOpt) ([]byte, error) {
	return nil, errors.New("unused")
}
func (b sseBody) DoStream(context.Context, string, string, url.Values, ...rest.RequestOpt) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(b))), nil
}

func TestFollowWritesSDKEventsAsTheyArrive(t *testing.T) {
	stream, err := rest.OpenEventStream(context.Background(), sseBody("data: first\n\n"), "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	apps := &fakeApps{stream: &vertracloud.ApplicationRealtimeEventStream{EventStream: stream}}
	client := &vertracloud.Client{Apps: apps}
	var out bytes.Buffer
	if err := Follow(context.Background(), client, []string{"app-1", "--follow"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "first\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestFollowStripsEscapesButKeepsColor(t *testing.T) {
	stream, err := rest.OpenEventStream(context.Background(), sseBody("data: \x1b]52;c;QQ==\x07\x1b[2J\x1b[31mred\x1b[0m\n\n"), "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &vertracloud.Client{Apps: &fakeApps{stream: &vertracloud.ApplicationRealtimeEventStream{EventStream: stream}}}
	var out bytes.Buffer
	if err := Follow(context.Background(), client, []string{"app-1", "--follow"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "\x1b[31mred\x1b[0m\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestSourceReaderUsesLocalIgnoreRules(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"main.go":           "package main",
		".env":              "TOKEN=secret",
		".env.local":        "TOKEN=secret",
		".vertraignore":     "*.secret\n",
		"credential.secret": "secret",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(original)
	r, _, err := sourceReader("")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, entry := range z.File {
		names[entry.Name] = true
	}
	if !names["main.go"] || names[".env"] || names[".env.local"] || names["credential.secret"] || names[".vertraignore"] {
		t.Fatalf("unexpected zip contents: %#v", names)
	}
}

func TestRequireIDFallsBackToProjectConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vertracloud.config"), []byte("ID=app-from-config\n"), 0600); err != nil {
		t.Fatal(err)
	}
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(original)
	id, rest, err := requireID(context.Background(), &vertracloud.Client{}, nil, "app info")
	if err != nil || id != "app-from-config" || len(rest) != 0 {
		t.Fatalf("id=%q rest=%v err=%v", id, rest, err)
	}
}

func TestMissingAppIDFailsWithoutAccountRequestWhenNonInteractive(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(original)
	t.Setenv("CI", "1")
	account := &fakeAccount{}
	client := &vertracloud.Client{Apps: &fakeApps{}, Account: account}
	if _, err := Run(context.Background(), client, "info", nil); err == nil {
		t.Fatal("expected missing app id error")
	}
	if account.gets != 0 {
		t.Fatalf("account GET calls = %d, want 0", account.gets)
	}
}

func TestFileReadResponseMatchesNodeJSONShape(t *testing.T) {
	files := fakeFiles{readContent: vertracloud.ApplicationFileContent{
		Type: vertracloud.ApplicationFileContentTypeBase64,
		Data: "aGk=",
	}}
	client := &vertracloud.Client{Apps: &fakeApps{files: files}}
	outputPath := filepath.Join(t.TempDir(), "saved.txt")
	got, err := RunWithOptions(context.Background(), client, "file", []string{"read", "main.txt", "--app", "app-1", "--output", outputPath}, true)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"path": "main.txt", "size": 2, "encoding": "base64", "data": "aGk="}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("file read response = %#v, want %#v", got, want)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("JSON file read unexpectedly wrote --output file: err=%v", err)
	}
}

func TestFileWriteAndMoveReturnNodeSuccessShapes(t *testing.T) {
	client := &vertracloud.Client{Apps: &fakeApps{}}
	contentPath := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(contentPath, []byte("hi"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := RunWithOptions(context.Background(), client, "file", []string{"write", "main.txt", "--app", "app-1", "--from", contentPath}, true)
	if err != nil || got != "success" {
		t.Fatalf("file write response = %#v, err=%v", got, err)
	}
	got, err = RunWithOptions(context.Background(), client, "file", []string{"move", "main.txt", "src/main.txt", "--app", "app-1"}, true)
	want := map[string]any{"status": "success", "moved": map[string]string{"from": "main.txt", "to": "src/main.txt"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("file move response = %#v, want %#v, err=%v", got, want, err)
	}
}

type fakeApps struct {
	deleted     string
	getID       string
	createCalls int
	createdApp  vertracloud.Application
	waitApp     vertracloud.Application
	waitErr     error
	waitCalls   int
	stream      *vertracloud.ApplicationRealtimeEventStream
	files       fakeFiles
	releases    fakeReleases
	domains     fakeDomains
	configBody  *vertracloud.ApplicationUpdateConfigBody
	configErr   error
	live        []vertracloud.ApplicationStatusShort
}

type fakeAccount struct {
	vertracloud.AccountService
	gets int
	apps []vertracloud.Application
}

func (f *fakeAccount) Get(context.Context, ...rest.RequestOpt) (vertracloud.AccountInfo, error) {
	f.gets++
	return vertracloud.AccountInfo{Applications: f.apps}, nil
}

func (f *fakeApps) Runtimes(context.Context, ...rest.RequestOpt) (vertracloud.ApplicationRuntimes, error) {
	return nil, nil
}
func (f *fakeApps) StatusAll(context.Context, ...rest.RequestOpt) ([]vertracloud.ApplicationStatusShort, error) {
	return f.live, nil
}
func (f *fakeApps) Get(_ context.Context, id string, _ ...rest.RequestOpt) (vertracloud.Application, error) {
	f.getID = id
	return vertracloud.Application{}, nil
}
func (f *fakeApps) Status(context.Context, string, ...rest.RequestOpt) (vertracloud.ApplicationStatusInfo, error) {
	return vertracloud.ApplicationStatusInfo{}, nil
}
func (f *fakeApps) Realtime(context.Context, string, *vertracloud.ApplicationRealtimeParams, ...rest.RequestOpt) (*vertracloud.ApplicationRealtimeEventStream, error) {
	if f.stream == nil {
		return nil, errors.New("unused")
	}
	return f.stream, nil
}
func (f *fakeApps) Metrics(context.Context, string, *vertracloud.ApplicationMetricsParams, ...rest.RequestOpt) ([]vertracloud.ApplicationMetric, error) {
	return nil, nil
}
func (f *fakeApps) Logs(context.Context, string, ...rest.RequestOpt) (string, error) { return "", nil }
func (f *fakeApps) Download(context.Context, string, ...rest.RequestOpt) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (f *fakeApps) Create(context.Context, vertracloud.ApplicationCreateParams, ...rest.RequestOpt) (vertracloud.Application, error) {
	f.createCalls++
	return f.createdApp, nil
}
func (f *fakeApps) WaitUntilReady(context.Context, string, *vertracloud.WaitOptions, ...rest.RequestOpt) (vertracloud.Application, error) {
	f.waitCalls++
	return f.waitApp, f.waitErr
}
func (f *fakeApps) Start(context.Context, string, ...rest.RequestOpt) (vertracloud.ApplicationOperationResponse, error) {
	return vertracloud.ApplicationOperationResponse{}, nil
}
func (f *fakeApps) Stop(context.Context, string, ...rest.RequestOpt) (vertracloud.ApplicationOperationResponse, error) {
	return vertracloud.ApplicationOperationResponse{}, nil
}
func (f *fakeApps) Restart(context.Context, string, *vertracloud.ApplicationRestartBody, ...rest.RequestOpt) (vertracloud.ApplicationOperationResponse, error) {
	return vertracloud.ApplicationOperationResponse{}, nil
}
func (f *fakeApps) UpdateConfig(_ context.Context, _ string, body vertracloud.ApplicationUpdateConfigBody, _ ...rest.RequestOpt) (string, error) {
	f.configBody = &body
	return "", f.configErr
}
func (f *fakeApps) Delete(_ context.Context, id string, _ ...rest.RequestOpt) error {
	f.deleted = id
	return nil
}
func (f *fakeApps) Deploys() vertracloud.AppsDeploysService   { return fakeDeploys{} }
func (f *fakeApps) Network() vertracloud.AppsNetworkService   { return fakeNetwork{} }
func (f *fakeApps) Domains() vertracloud.AppsDomainsService   { return &f.domains }
func (f *fakeApps) Envs() vertracloud.AppsEnvsService         { return fakeEnvs{} }
func (f *fakeApps) Releases() vertracloud.AppsReleasesService { return &f.releases }
func (f *fakeApps) Files() vertracloud.AppsFilesService       { return &f.files }

type fakeDeploys struct{}

func (fakeDeploys) List(context.Context, string, ...rest.RequestOpt) ([]vertracloud.ApplicationDeployment, error) {
	return nil, nil
}
func (fakeDeploys) Webhook() vertracloud.AppsDeploysWebhookService { return fakeWebhook{} }

type fakeWebhook struct{}

func (fakeWebhook) Get(context.Context, string, ...rest.RequestOpt) (vertracloud.ApplicationWebhook, error) {
	return vertracloud.ApplicationWebhook{}, nil
}
func (fakeWebhook) Create(context.Context, string, vertracloud.ApplicationWebhookCreateBody, ...rest.RequestOpt) (vertracloud.ApplicationWebhook, error) {
	return vertracloud.ApplicationWebhook{}, nil
}
func (fakeWebhook) Delete(context.Context, string, ...rest.RequestOpt) error { return nil }

type fakeNetwork struct{}

func (fakeNetwork) PurgeCache(context.Context, string, *vertracloud.ApplicationPurgeCacheBody, ...rest.RequestOpt) error {
	return nil
}
func (fakeNetwork) SetSubdomain(context.Context, string, string, ...rest.RequestOpt) (vertracloud.ApplicationSubdomainResponse, error) {
	return vertracloud.ApplicationSubdomainResponse{}, nil
}
func (fakeNetwork) Publish(context.Context, string, string, ...rest.RequestOpt) (vertracloud.ApplicationWebPublish, error) {
	return vertracloud.ApplicationWebPublish{}, nil
}
func (fakeNetwork) Unpublish(context.Context, string, ...rest.RequestOpt) (vertracloud.ApplicationWebPublish, error) {
	return vertracloud.ApplicationWebPublish{}, nil
}

type fakeDomains struct {
	existing []vertracloud.ApplicationDomain
	added    *vertracloud.ApplicationDomainAddBody
	updated  *vertracloud.ApplicationDomainUpdateBody
	updateID string
	removed  string
	dnsID    string
	appID    string
}

func (f *fakeDomains) List(_ context.Context, id string, _ ...rest.RequestOpt) ([]vertracloud.ApplicationDomain, error) {
	f.appID = id
	return f.existing, nil
}
func (f *fakeDomains) Add(_ context.Context, id string, body vertracloud.ApplicationDomainAddBody, _ ...rest.RequestOpt) ([]vertracloud.ApplicationDomain, error) {
	f.appID, f.added = id, &body
	return f.existing, nil
}
func (f *fakeDomains) Update(_ context.Context, id, domainID string, body vertracloud.ApplicationDomainUpdateBody, _ ...rest.RequestOpt) ([]vertracloud.ApplicationDomain, error) {
	f.appID, f.updateID, f.updated = id, domainID, &body
	return f.existing, nil
}
func (f *fakeDomains) Remove(_ context.Context, id, domainID string, _ ...rest.RequestOpt) error {
	f.appID, f.removed = id, domainID
	return nil
}
func (f *fakeDomains) DNS(_ context.Context, id, domainID string, _ ...rest.RequestOpt) ([]vertracloud.ApplicationDomainDNSRecord, error) {
	f.appID, f.dnsID = id, domainID
	return []vertracloud.ApplicationDomainDNSRecord{{Type: vertracloud.ApplicationDomainDNSRecordTypeCNAME, Name: "x.com"}}, nil
}
func (f *fakeDomains) Connect(context.Context, string, string, ...rest.RequestOpt) (vertracloud.ApplicationDomainConnect, error) {
	return vertracloud.ApplicationDomainConnect{}, nil
}

func domainsClient(existing ...vertracloud.ApplicationDomain) (*vertracloud.Client, *fakeDomains) {
	apps := &fakeApps{}
	apps.domains.existing = existing
	return &vertracloud.Client{Apps: apps}, &apps.domains
}

func TestDomainsAddSendsFlags(t *testing.T) {
	client, domains := domainsClient()
	_, err := Run(context.Background(), client, "domains", []string{"add", "example.com", "--app", "app-1", "--port", "3000", "--path-prefix", "/api", "--redirect-status", "302", "--no-www"})
	if err != nil {
		t.Fatal(err)
	}
	no := false
	want := vertracloud.ApplicationDomainAddBody{Hostname: "example.com", WWW: &no, Port: 3000, PathPrefix: "/api", RedirectStatus: vertracloud.ApplicationDomainRedirectStatusFound}
	if domains.appID != "app-1" || !reflect.DeepEqual(*domains.added, want) {
		t.Fatalf("app=%q body=%#v", domains.appID, domains.added)
	}
}

func TestDomainsAddDefaultsKeepWWWUnset(t *testing.T) {
	client, domains := domainsClient()
	if _, err := Run(context.Background(), client, "domains", []string{"add", "example.com", "--app", "app-1"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*domains.added, vertracloud.ApplicationDomainAddBody{Hostname: "example.com"}) {
		t.Fatalf("body = %#v", domains.added)
	}
}

func TestDomainsAddResolvesRedirectTargetByHostname(t *testing.T) {
	client, domains := domainsClient(vertracloud.ApplicationDomain{ID: "d1", Hostname: "main.com"})
	if _, err := Run(context.Background(), client, "domains", []string{"add", "old.com", "--app", "app-1", "--redirect-to", "MAIN.com"}); err != nil {
		t.Fatal(err)
	}
	if domains.added.RedirectTo != "d1" {
		t.Fatalf("redirect_to = %q, want d1", domains.added.RedirectTo)
	}
}

func TestDomainsAddRequiresHostnameAndValidRanges(t *testing.T) {
	client, _ := domainsClient()
	for _, args := range [][]string{
		{"add", "--app", "app-1"},
		{"add", "x.com", "--app", "app-1", "--port", "0"},
		{"add", "x.com", "--app", "app-1", "--port", "70000"},
		{"add", "x.com", "--app", "app-1", "--redirect-status", "300"},
	} {
		if _, err := Run(context.Background(), client, "domains", args); err == nil {
			t.Errorf("%v: expected error", args)
		}
	}
}

func TestDomainsUpdateSendsOnlySetFlags(t *testing.T) {
	client, domains := domainsClient(vertracloud.ApplicationDomain{ID: "d1", Hostname: "x.com"}, vertracloud.ApplicationDomain{ID: "d2", Hostname: "www.x.com"})
	if _, err := Run(context.Background(), client, "domains", []string{"update", "www.x.com", "--app", "app-1", "--primary"}); err != nil {
		t.Fatal(err)
	}
	if domains.updateID != "d2" || !reflect.DeepEqual(*domains.updated, vertracloud.ApplicationDomainUpdateBody{Primary: true}) {
		t.Fatalf("id=%q body=%#v", domains.updateID, domains.updated)
	}
	if _, err := Run(context.Background(), client, "domains", []string{"update", "d1", "--app", "app-1", "--port", "8080", "--redirect-to", "d2"}); err != nil {
		t.Fatal(err)
	}
	port := 8080
	want := vertracloud.ApplicationDomainUpdateBody{Port: &port, RedirectTo: vertracloud.NullableValue("d2")}
	if domains.updateID != "d1" || !reflect.DeepEqual(*domains.updated, want) {
		t.Fatalf("id=%q body=%#v", domains.updateID, domains.updated)
	}
}

func TestDomainsUpdateEmptyValueClears(t *testing.T) {
	client, domains := domainsClient(vertracloud.ApplicationDomain{ID: "d1", Hostname: "x.com"})
	if _, err := Run(context.Background(), client, "domains", []string{"update", "d1", "--app", "app-1", "--path-prefix", "", "--redirect-to", ""}); err != nil {
		t.Fatal(err)
	}
	want := vertracloud.ApplicationDomainUpdateBody{PathPrefix: vertracloud.NullableNull[string](), RedirectTo: vertracloud.NullableNull[string]()}
	if !reflect.DeepEqual(*domains.updated, want) {
		t.Fatalf("body = %#v", domains.updated)
	}
}

func TestDomainsUpdateWithoutChangesFails(t *testing.T) {
	client, domains := domainsClient(vertracloud.ApplicationDomain{ID: "d1", Hostname: "x.com"})
	if _, err := Run(context.Background(), client, "domains", []string{"update", "d1", "--app", "app-1"}); err == nil || domains.updated != nil {
		t.Fatalf("err=%v updated=%v", err, domains.updated)
	}
}

func TestDomainsRemoveYesAndDNSResolveByHostname(t *testing.T) {
	client, domains := domainsClient(vertracloud.ApplicationDomain{ID: "d1", Hostname: "x.com"})
	got, err := Run(context.Background(), client, "domains", []string{"remove", "x.com", "--app", "app-1", "--yes"})
	if err != nil || domains.removed != "d1" || !reflect.DeepEqual(got, map[string]string{"id": "d1"}) {
		t.Fatalf("got=%#v err=%v removed=%q", got, err, domains.removed)
	}
	if _, err := Run(context.Background(), client, "domains", []string{"dns", "d1", "--app", "app-1"}); err != nil || domains.dnsID != "d1" {
		t.Fatalf("err=%v dns=%q", err, domains.dnsID)
	}
}

func TestDomainsUnknownReferenceFails(t *testing.T) {
	client, domains := domainsClient(vertracloud.ApplicationDomain{ID: "d1", Hostname: "x.com"})
	if _, err := Run(context.Background(), client, "domains", []string{"remove", "nope.com", "--app", "app-1", "--yes"}); err == nil || domains.removed != "" {
		t.Fatalf("err=%v removed=%q", err, domains.removed)
	}
}

func TestDomainsListAcceptsPositionalAppID(t *testing.T) {
	client, domains := domainsClient()
	if _, err := Run(context.Background(), client, "domains", []string{"list", "app-9"}); err != nil || domains.appID != "app-9" {
		t.Fatalf("err=%v app=%q", err, domains.appID)
	}
}

func TestListFilterDomainMatchesAnyDomainOrSubdomain(t *testing.T) {
	sub := "demo.vertraweb.app"
	app := vertracloud.Application{Subdomain: &sub, Domains: []vertracloud.ApplicationDomain{{Hostname: "shop.example.com"}, {Hostname: "www.shop.example.com"}}}
	for _, value := range []string{"www.shop", "shop.example", "demo.vertraweb"} {
		if !appMatches(app, "domain", value) {
			t.Errorf("domain=%s should match", value)
		}
	}
	if appMatches(app, "domain", "other.com") {
		t.Error("domain=other.com should not match")
	}
	if !appMatchesFilter(app, "domain", "!=", "other.com") || appMatchesFilter(app, "domain", "!=", "shop.example.com") {
		t.Error("domain!= should compare against every hostname")
	}
}

type fakeEnvs struct{}

func (fakeEnvs) List(context.Context, string, ...rest.RequestOpt) ([]vertracloud.ApplicationEnvironment, error) {
	return nil, nil
}
func (fakeEnvs) Set(context.Context, string, []vertracloud.ApplicationEnvironmentInput, ...rest.RequestOpt) ([]vertracloud.ApplicationEnvironment, error) {
	return nil, nil
}
func (fakeEnvs) Delete(context.Context, string, string, ...rest.RequestOpt) error { return nil }

type fakeFiles struct {
	uploadResponse vertracloud.ApplicationFileUploadResponse
	uploadID       string
	uploadRestart  *bool
	uploadPath     string
	uploadMessage  string
	readContent    vertracloud.ApplicationFileContent
}

func (fakeFiles) List(context.Context, string, vertracloud.ApplicationFileListParams, ...rest.RequestOpt) ([]vertracloud.ApplicationFile, error) {
	return nil, nil
}
func (fakeFiles) Tree(context.Context, string, ...rest.RequestOpt) ([]vertracloud.ApplicationFileTree, error) {
	return nil, nil
}
func (f fakeFiles) Read(context.Context, string, vertracloud.ApplicationFileReadParams, ...rest.RequestOpt) (vertracloud.ApplicationFileContent, error) {
	return f.readContent, nil
}
func (fakeFiles) Write(context.Context, string, vertracloud.ApplicationFileWriteBody, ...rest.RequestOpt) error {
	return nil
}
func (fakeFiles) Move(context.Context, string, vertracloud.ApplicationFileMoveBody, ...rest.RequestOpt) error {
	return nil
}
func (fakeFiles) Delete(context.Context, string, vertracloud.ApplicationFileDeleteBody, ...rest.RequestOpt) error {
	return nil
}
func (f *fakeFiles) Upload(_ context.Context, id string, params vertracloud.ApplicationFileUploadParams, _ ...rest.RequestOpt) (vertracloud.ApplicationFileUploadResponse, error) {
	f.uploadID = id
	f.uploadRestart = params.Restart
	f.uploadPath = params.Path
	f.uploadMessage = params.Message
	return f.uploadResponse, nil
}

func TestCommitReturnsUploadResponse(t *testing.T) {
	apps := &fakeApps{files: fakeFiles{uploadResponse: vertracloud.ApplicationFileUploadResponse{
		AppID: "app-1", UpdatedAt: time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC),
	}}}
	client := &vertracloud.Client{Apps: apps}
	zipPath := filepath.Join(t.TempDir(), "app.zip")
	if err := os.WriteFile(zipPath, []byte("zip"), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := Run(context.Background(), client, "commit", []string{"app-1", "--file", zipPath, "--restart", "--path", "src/lib"})
	if err != nil {
		t.Fatal(err)
	}
	response, ok := got.(vertracloud.ApplicationFileUploadResponse)
	if !ok || response.AppID != "app-1" || !response.UpdatedAt.Equal(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("commit returned %#v", got)
	}
	if apps.files.uploadID != "app-1" || apps.files.uploadRestart == nil || !*apps.files.uploadRestart {
		t.Fatalf("upload id=%q restart=%v", apps.files.uploadID, apps.files.uploadRestart)
	}
	if apps.files.uploadPath != "src/lib" {
		t.Fatalf("upload path=%q, want src/lib", apps.files.uploadPath)
	}
}

// initGitRepo creates a git repository in a temp dir with one commit whose
// subject is subject (no commit when subject is empty).
func initGitRepo(t *testing.T, subject string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	if subject != "" {
		git("commit", "-q", "--allow-empty", "-m", subject)
	}
	return dir
}

func chdirTo(t *testing.T, dir string) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
}

// commitMessage runs `app commit app-1 <args>` against a fake and returns the message sent.
func commitMessage(t *testing.T, args ...string) string {
	t.Helper()
	apps := &fakeApps{}
	if _, err := Run(context.Background(), &vertracloud.Client{Apps: apps}, "commit", append([]string{"app-1"}, args...)); err != nil {
		t.Fatal(err)
	}
	return apps.files.uploadMessage
}

func writeZip(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "app.zip")
	if err := os.WriteFile(p, []byte("zip"), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCommitMessageFlagWinsOverGit(t *testing.T) {
	chdirTo(t, initGitRepo(t, "assunto do git"))
	zip := writeZip(t)
	if got := commitMessage(t, "--file", zip, "-m", "  explicita  "); got != "explicita" {
		t.Fatalf("-m sent %q", got)
	}
	if got := commitMessage(t, "--file", zip, "--message=longa"); got != "longa" {
		t.Fatalf("--message sent %q", got)
	}
}

func TestCommitMessageFallsBackToLastGitSubject(t *testing.T) {
	repo := initGitRepo(t, "assunto do git")
	chdirTo(t, repo)
	if got := commitMessage(t, "--file", writeZip(t)); got != "assunto do git" {
		t.Fatalf("fallback sent %q", got)
	}
	// --file pointing at a folder: the folder's repository is used, not the cwd's.
	chdirTo(t, t.TempDir())
	if got := commitMessage(t, "--file", repo); got != "assunto do git" {
		t.Fatalf("folder --file sent %q", got)
	}
}

func TestCommitMessageEmptyFlagSendsNothing(t *testing.T) {
	chdirTo(t, initGitRepo(t, "assunto do git"))
	if got := commitMessage(t, "--file", writeZip(t), "-m", ""); got != "" {
		t.Fatalf("empty -m sent %q", got)
	}
}

func TestCommitMessageOutsideRepoIsEmpty(t *testing.T) {
	dir := t.TempDir()
	// Stop git from finding a repository above the temp dir.
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	chdirTo(t, dir)
	if got := commitMessage(t, "--file", writeZip(t)); got != "" {
		t.Fatalf("outside a repo sent %q", got)
	}
}

func TestCommitMessageIsCutAt500Runes(t *testing.T) {
	chdirTo(t, t.TempDir())
	got := commitMessage(t, "--file", writeZip(t), "-m", strings.Repeat("ç", 600))
	if n := utf8.RuneCountInString(got); n != 500 || !utf8.ValidString(got) {
		t.Fatalf("got %d runes, valid=%v", n, utf8.ValidString(got))
	}
}

func TestMetricsAcceptsTableAndAllAliases(t *testing.T) {
	client := &vertracloud.Client{Apps: &fakeApps{}}
	for _, flags := range [][]string{{"app-1", "--table", "-A"}, {"--all", "app-1"}} {
		if _, err := Run(context.Background(), client, "metrics", flags); err != nil {
			t.Fatalf("metrics %v: %v", flags, err)
		}
	}
}

func runUpload(t *testing.T, apps *fakeApps, extra ...string) (any, error) {
	t.Helper()
	original, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(original)
	zipPath := filepath.Join(t.TempDir(), "a.zip")
	if err := os.WriteFile(zipPath, []byte("PK"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--name", "a", "--memory", "256", "--main", "index.js", "--file", zipPath}
	return RunWithOptions(context.Background(), &vertracloud.Client{Apps: apps}, "upload", append(args, extra...), true)
}

func TestUploadWaitsForBuild(t *testing.T) {
	building := vertracloud.Application{ID: "app-1", Status: vertracloud.ApplicationStatusBuilding}
	apps := &fakeApps{createdApp: building, waitApp: vertracloud.Application{ID: "app-1", Status: vertracloud.ApplicationStatusUp}}
	got, err := runUpload(t, apps)
	if err != nil || apps.waitCalls != 1 || got.(vertracloud.Application).Status != vertracloud.ApplicationStatusUp {
		t.Fatalf("got=%#v err=%v waits=%d", got, err, apps.waitCalls)
	}
	apps = &fakeApps{createdApp: building}
	if _, err := runUpload(t, apps, "--no-wait"); err != nil || apps.waitCalls != 0 {
		t.Fatalf("--no-wait: err=%v waits=%d", err, apps.waitCalls)
	}
	apps = &fakeApps{createdApp: building, waitErr: &vertracloud.AppNotReadyError{Code: "APP_DOWN"}}
	if _, err := runUpload(t, apps); err == nil || !strings.Contains(err.Error(), "APP_DOWN") {
		t.Fatalf("down err = %v", err)
	}
	apps = &fakeApps{createdApp: building, waitErr: &vertracloud.AppNotReadyError{Code: "INSTALL_OUT_OF_MEMORY"}}
	if _, err := runUpload(t, apps); err == nil || !strings.Contains(err.Error(), "(INSTALL_OUT_OF_MEMORY): raise the app memory") {
		t.Fatalf("oom err = %v", err)
	}
}

func TestListKeepsBuildingStatus(t *testing.T) {
	client := &vertracloud.Client{
		Apps: &fakeApps{live: []vertracloud.ApplicationStatusShort{{ID: "a1"}, {ID: "a2", Running: true}}},
		Account: &fakeAccount{apps: []vertracloud.Application{
			{ID: "a1", Status: vertracloud.ApplicationStatusBuilding},
			{ID: "a2", Status: vertracloud.ApplicationStatusUp},
		}},
	}
	got, err := list(context.Background(), client, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := got.(ui.Presentation).Human.([]map[string]any)
	if rows[0]["status"] != "building" || rows[1]["status"] != "up" {
		t.Fatalf("statuses = %v, %v", rows[0]["status"], rows[1]["status"])
	}
}
