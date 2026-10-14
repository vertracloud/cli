package blob

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// newClient points the SDK at a test server: API paths and Blob paths share it.
func newClient(t *testing.T, handler http.HandlerFunc) *vertracloud.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	rc, err := rest.NewClient("test-key", rest.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	return vertracloud.New(rc, vertracloud.WithBlobBaseURL(srv.URL+"/v1"))
}

// jsonOf unwraps the value the root command serializes for --json.
func jsonOf(data any) any {
	if p, ok := data.(ui.Presentation); ok {
		return p.JSON
	}
	return data
}

func TestHumanOutputIsReadable(t *testing.T) {
	if got := humanSize(1536); got != "1.5 KB" {
		t.Errorf("humanSize(1536) = %q", got)
	}
	if got := humanSize(10 << 30); got != "10.0 GB" {
		t.Errorf("humanSize(10 GiB) = %q", got)
	}
	if got := listText(nil, nil); !strings.Contains(got, "No files.") && !strings.Contains(got, "Nenhum arquivo.") && !strings.Contains(got, "Ningún archivo.") {
		t.Errorf("empty listing = %q", got)
	}
}

func reply(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"response": body}); err != nil {
		t.Fatal(err)
	}
}

var sampleObject = map[string]any{
	"id": "9", "path": "fotos/a.png", "size": 4, "content_type": "image/png", "etag": "e",
	"visibility": "private", "url": nil, "content_disposition": "inline", "metadata": map[string]string{},
	"created_at": "2026-10-14T10:00:00Z", "updated_at": "2026-10-14T10:00:00Z",
}

func TestListSendsPrefixAndDelimiterAndReturnsFolders(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/objects" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("prefix"); got != "fotos/" {
			t.Errorf("prefix = %q", got)
		}
		if got := r.URL.Query().Get("delimiter"); got != "/" {
			t.Errorf("delimiter = %q", got)
		}
		reply(t, w, map[string]any{"objects": []any{sampleObject}, "folders": []string{"fotos/2026/"}, "cursor": nil})
	})

	data, err := Run(context.Background(), client, "ls", []string{"fotos/"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	out := jsonOf(data).(map[string]any)
	if objects := out["objects"].([]vertracloud.BlobObject); len(objects) != 1 || objects[0].ID != "9" {
		t.Fatalf("objects = %+v", out["objects"])
	}
	if folders := out["folders"].([]string); len(folders) != 1 || folders[0] != "fotos/2026/" {
		t.Fatalf("folders = %v", out["folders"])
	}
}

func TestPutAcceptsFlagsAfterPathAndSendsPublicVisibility(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(file, []byte("oi!!"), 0o600); err != nil {
		t.Fatal(err)
	}
	var progress bytes.Buffer
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/objects" {
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("path") != "docs/a.txt" || q.Get("visibility") != "public" {
			t.Errorf("query = %v", q)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(f)
		if string(body) != "oi!!" {
			t.Errorf("uploaded bytes = %q", body)
		}
		reply(t, w, sampleObject)
	})

	data, err := Run(context.Background(), client, "put", []string{file, "docs/a.txt", "--public"}, &progress)
	if err != nil {
		t.Fatal(err)
	}
	if obj := jsonOf(data).(vertracloud.BlobObject); obj.ID != "9" {
		t.Fatalf("object = %+v", obj)
	}
	if progress.Len() != 0 {
		t.Errorf("progress written off a terminal: %q", progress.String())
	}
}

func TestGetDownloadsThroughTemporaryLinkAndNeverOverwrites(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/objects/9" && r.Method == http.MethodGet:
			reply(t, w, sampleObject)
		case r.URL.Path == "/v1/objects/9/download":
			if r.URL.Query().Get("redirect") != "false" {
				t.Errorf("redirect = %q", r.URL.Query().Get("redirect"))
			}
			reply(t, w, map[string]any{"url": "http://" + r.Host + "/file/token", "expires_at": "2026-10-14T11:00:00Z"})
		case r.URL.Path == "/file/token":
			_, _ = w.Write([]byte("oi!!"))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})

	dest := filepath.Join(t.TempDir(), "saida.png")
	data, err := Run(context.Background(), client, "get", []string{"9", dest}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got := jsonOf(data).(map[string]any)["bytes"]; got != int64(4) {
		t.Fatalf("bytes = %v", got)
	}
	if body, _ := os.ReadFile(dest); string(body) != "oi!!" {
		t.Fatalf("file = %q", body)
	}
	if _, err := Run(context.Background(), client, "get", []string{"9", dest}, io.Discard); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second get err = %v, want already exists", err)
	}
}

func TestGetResolvesExactPath(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/objects":
			reply(t, w, map[string]any{"objects": []any{sampleObject}, "folders": []string{}, "cursor": nil})
		case "/v1/objects/9/download":
			reply(t, w, map[string]any{"url": "http://" + r.Host + "/file/x", "expires_at": "2026-10-14T11:00:00Z"})
		case "/file/x":
			_, _ = w.Write([]byte("abc"))
		}
	})

	dest := filepath.Join(t.TempDir(), "a.png")
	if _, err := Run(context.Background(), client, "get", []string{"fotos/a.png", dest}, io.Discard); err != nil {
		t.Fatalf("path lookup failed: %v", err)
	}
	if _, err := Run(context.Background(), client, "get", []string{"fotos/inexistente.png", dest}, io.Discard); err == nil {
		t.Fatal("missing path should be an error")
	}
}

func TestRemoveDeletesByIDs(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v1/objects" {
			t.Fatalf("%s %s", r.Method, r.URL.Path)
		}
		var body struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.IDs) != 2 {
			t.Errorf("ids = %v", body.IDs)
		}
		reply(t, w, map[string]any{"deleted": body.IDs, "not_found": []string{}, "remaining": false})
	})

	data, err := Run(context.Background(), client, "rm", []string{"1", "2"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if deleted := jsonOf(data).(vertracloud.BlobDeleteResult).Deleted; len(deleted) != 2 {
		t.Fatalf("deleted = %v", deleted)
	}
}

func TestLinkRejectsOutOfRangeExpiryWithoutRequest(t *testing.T) {
	calls := 0
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	if _, err := Run(context.Background(), client, "link", []string{"9", "--expires", "10"}, io.Discard); err == nil {
		t.Fatal("expires 10 should be rejected")
	}
	if calls != 0 {
		t.Fatalf("%d requests sent", calls)
	}
}

func TestUnknownCommandAndNilClient(t *testing.T) {
	if _, err := Run(context.Background(), nil, "ls", nil, io.Discard); err == nil {
		t.Fatal("nil client should fail")
	}
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {})
	if _, err := Run(context.Background(), client, "nope", nil, io.Discard); err == nil {
		t.Fatal("unknown command should fail")
	}
}
