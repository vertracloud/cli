package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

func TestAPIBaseURLAppliesToAuthenticatedAndPublicSDKCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/users/me":
			if r.Header.Get("Authorization") != "Bearer local-key" {
				t.Errorf("unexpected Authorization header: %q", r.Header.Get("Authorization"))
			}
		case "/v1/status":
		default:
			t.Errorf("unexpected API path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"response":{}}`))
	}))
	defer server.Close()
	oldURL := apiBaseURL
	apiBaseURL = server.URL
	defer func() { apiBaseURL = oldURL }()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VERTRA_API_KEY", "local-key")
	for _, args := range [][]string{{"auth", "login", "--token", "local-key", "--json"}, {"auth", "whoami", "--json"}, {"status", "--json"}} {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), args, &out, &errOut); code != 0 {
			t.Fatalf("%v: exit %d, error %s", args, code, errOut.String())
		}
	}
}

func TestUnauthenticatedJSONError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VERTRA_API_KEY", "")
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"app", "list", "--json"}, &out, &errOut); code != 1 {
		t.Fatalf("exit %d", code)
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(errOut.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Code != "NOT_AUTHENTICATED" {
		t.Fatalf("error: %s", errOut.String())
	}
	if out.Len() != 0 {
		t.Fatalf("stdout: %s", out.String())
	}
}

func TestDoctorRunsWithoutAPIKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VERTRA_API_KEY", "")
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"doctor", "--path", t.TempDir(), "--json"}, &out, &errOut)
	if code != 0 || !strings.Contains(out.String(), `"checks"`) {
		t.Fatalf("exit=%d out=%s err=%s", code, out.String(), errOut.String())
	}
}

func TestLinkShowMissingConfigIsNull(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VERTRA_API_KEY", "")
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"link", "--show", "--json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit=%d err=%s", code, errOut.String())
	}
	if out.String() != "null\n" {
		t.Fatalf("output=%q", out.String())
	}
}

func TestKnownRuntime(t *testing.T) {
	runtimes := vertracloud.ApplicationRuntimes{"node": {Recommended: "22", Latest: "24", Specific: []string{"20"}}}
	if !knownRuntime(runtimes, "20") || knownRuntime(runtimes, "99") {
		t.Fatal("runtime lookup failed")
	}
}

func TestHelpDoesNotRequireAuthentication(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VERTRA_API_KEY", "")
	for _, args := range [][]string{{"--help"}, {"db", "--help"}} {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), args, &out, &errOut); code != 0 {
			t.Fatalf("args=%v exit=%d err=%s", args, code, errOut.String())
		}
		if !strings.Contains(out.String(), "--lang") || errOut.Len() != 0 {
			t.Fatalf("args=%v out=%s err=%s", args, out.String(), errOut.String())
		}
	}
}

func TestTopLevelPushAndDeployAreCommitAliases(t *testing.T) {
	for _, alias := range []string{"push", "deploy"} {
		if got := canonicalAppCommand(alias); got != "commit" {
			t.Fatalf("%s maps to %s", alias, got)
		}
	}
}

func TestListOutputOptions(t *testing.T) {
	if size, all := listOutputOptions([]string{"app", "list", "--page-size", "3"}); size != 3 || all {
		t.Fatalf("app options: size=%d all=%v", size, all)
	}
	if size, all := listOutputOptions([]string{"db", "ls", "--page-size=8", "-A"}); size != 8 || !all {
		t.Fatalf("db options: size=%d all=%v", size, all)
	}
	if size, all := listOutputOptions([]string{"app", "info", "id"}); size != 0 || !all {
		t.Fatalf("non-list options: size=%d all=%v", size, all)
	}
}

func TestHumanOutputUsesLocaleAndKeepsJSONDatesOutOfRenderer(t *testing.T) {
	var out bytes.Buffer
	ui.Render(&out, map[string]any{"name": "demo", "created_at": "2026-09-22T12:00:00Z"}, "app", "pt")
	if !strings.Contains(out.String(), "demo") || !strings.Contains(out.String(), "Criado em") || strings.Contains(out.String(), "2026-09-22T12:00:00Z") {
		t.Fatalf("human output: %s", out.String())
	}
}

func TestNoninteractiveAuthLoginRequiresToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VERTRA_API_KEY", "")
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"auth", "login"}, &out, &errOut); code != 1 {
		t.Fatalf("exit=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	if out.Len() != 0 || !strings.Contains(errOut.String(), "terminal") {
		t.Fatalf("out=%s err=%s", out.String(), errOut.String())
	}
}

func TestJsonOutputIsOneValueOnStdout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VERTRA_API_KEY", "")
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"doctor", "--path", t.TempDir(), "--json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit=%d err=%s", code, errOut.String())
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not one JSON value: %q: %v", out.String(), err)
	}
	if errOut.Len() != 0 || strings.Count(strings.TrimSpace(out.String()), "\n") != 0 {
		t.Fatalf("stdout=%q stderr=%q", out.String(), errOut.String())
	}
}

func TestDestructivePromptMatrix(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"app", "delete", "id"}, true},
		{[]string{"app", "file", "delete", "id", "/tmp/a"}, true},
		{[]string{"app", "network", "purge-cache", "id"}, true},
		{[]string{"db", "credentials", "reset", "id", "password"}, true},
		{[]string{"snapshot", "restore", "id"}, true},
		{[]string{"workspace", "member", "remove", "ws", "member"}, true},
		{[]string{"folder", "remove", "id"}, false},
		{[]string{"favorite", "remove", "app", "id"}, false},
		{[]string{"workspace", "app", "remove", "ws", "id"}, false},
	} {
		got, _ := destructivePath(tc.args, "en")
		if got != tc.want {
			t.Errorf("destructivePath(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestJsonDestructiveCommandFailsOnStderrBeforeNetwork(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VERTRA_API_KEY", "dummy")
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"app", "delete", "app-id", "--json"}, &out, &errOut); code != 1 {
		t.Fatalf("exit=%d", code)
	}
	if out.Len() != 0 || !strings.Contains(errOut.String(), `"code":"CONFIRMATION_REQUIRED"`) {
		t.Fatalf("stdout=%q stderr=%q", out.String(), errOut.String())
	}
}
