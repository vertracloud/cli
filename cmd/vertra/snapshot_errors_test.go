package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/rest"
)

func TestSnapshotFailureTexts(t *testing.T) {
	old := ui.Locale
	t.Cleanup(func() { ui.Locale = old })
	cases := []struct {
		name     string
		apiErr   *rest.APIError
		wantMsg  string
		wantHint string
	}{
		{"restore in progress", &rest.APIError{Status: 409, Code: "RESTORE_IN_PROGRESS"}, "being restored", "snapshot jobs <id> --kind restore"},
		{"exceeds storage", &rest.APIError{Status: 400, Code: "SNAPSHOT_EXCEEDS_STORAGE", Details: map[string]any{"required_bytes": 3.0 * 1024 * 1024 * 1024, "available_bytes": 1024.0 * 1024 * 1024, "required_ram_mb": 2048.0}}, "does not fit", "needs 3.0 GiB and the target has 1.0 GiB. Use at least 2048 MB"},
		{"account quota", &rest.APIError{Status: 429, Code: "SNAPSHOT_RATE_LIMITED", Details: map[string]any{"scope": "account", "retry_after": 600.0, "resets_at": "2026-11-17T10:00:00Z"}}, "limit reached", "account's snapshot quota"},
		{"resource interval", &rest.APIError{Status: 429, Code: "SNAPSHOT_RATE_LIMITED", Details: map[string]any{"scope": "resource", "retry_after": 90.0}}, "limit reached", "in 90 s"},
		{"restore cooldown", &rest.APIError{Status: 429, Code: "RESTORE_COOLDOWN", Details: map[string]any{"retry_after": 60.0}}, "wait between restores", "in 60 s"},
		{"too large", &rest.APIError{Status: 413, Code: "SNAPSHOT_TOO_LARGE", Details: map[string]any{"max_bytes": 2.0 * 1024 * 1024 * 1024}}, "too large", "2.0 GiB"},
		{"app building", &rest.APIError{Status: 409, Code: "APP_BUILDING"}, "being deployed", "build to finish"},
		{"operation in progress", &rest.APIError{Status: 409, Code: "OPERATION_IN_PROGRESS"}, "snapshot is in progress", "moment"},
		{"corrupted", &rest.APIError{Code: "SNAPSHOT_CORRUPTED"}, "corrupted", "another snapshot"},
		{"timeout", &rest.APIError{Code: "SNAPSHOT_TIMEOUT"}, "took too long", "support"},
		{"import failed", &rest.APIError{Code: "SNAPSHOT_IMPORT_FAILED"}, "could not be imported", "unchanged"},
		{"engine mismatch", &rest.APIError{Code: "SNAPSHOT_ENGINE_MISMATCH"}, "different database engine", "same type"},
		{"database not ready", &rest.APIError{Status: 503, Code: "DATABASE_NOT_READY"}, "not ready", "online"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ui.Locale = "en"
			msg, hint, ok := snapshotFailure(tc.apiErr, tc.apiErr)
			if !ok || !strings.Contains(msg, tc.wantMsg) || !strings.Contains(hint, tc.wantHint) {
				t.Fatalf("msg = %q, hint = %q, ok = %v", msg, hint, ok)
			}
			if !strings.Contains(msg, tc.apiErr.Code) {
				t.Errorf("message lacks the code: %q", msg)
			}
			ui.Locale = "pt"
			ptMsg, _, _ := snapshotFailure(tc.apiErr, tc.apiErr)
			if ptMsg == msg {
				t.Errorf("pt text equals the en text: %q", ptMsg)
			}
		})
	}
}

func TestSnapshotFailureIgnoresOtherErrors(t *testing.T) {
	if _, _, ok := snapshotFailure(errors.New("x"), nil); ok {
		t.Error("plain error handled")
	}
	other := &rest.APIError{Code: "APP_NOT_FOUND"}
	if _, _, ok := snapshotFailure(other, other); ok {
		t.Error("unrelated code handled")
	}
}
