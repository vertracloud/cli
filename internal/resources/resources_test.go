package resources

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

type snapshotFake struct {
	created  bool
	download bool
	restored bool
}

func (f *snapshotFake) ListAll(context.Context, vertracloud.SnapshotScope, ...rest.RequestOpt) ([]vertracloud.GroupedResourceSnapshots, error) {
	return []vertracloud.GroupedResourceSnapshots{}, nil
}
func (f *snapshotFake) List(context.Context, string, vertracloud.SnapshotScope, ...rest.RequestOpt) ([]vertracloud.ResourceSnapshot, error) {
	return []vertracloud.ResourceSnapshot{}, nil
}
func (f *snapshotFake) Download(context.Context, string, string, vertracloud.SnapshotScope, ...rest.RequestOpt) (io.ReadCloser, error) {
	f.download = true
	return io.NopCloser(strings.NewReader("zip")), nil
}
func (f *snapshotFake) Create(context.Context, string, vertracloud.SnapshotScope, ...rest.RequestOpt) (vertracloud.ResourceSnapshot, error) {
	f.created = true
	return vertracloud.ResourceSnapshot{ID: "snap-1"}, nil
}
func (f *snapshotFake) Restore(context.Context, string, string, vertracloud.SnapshotScope, ...rest.RequestOpt) (vertracloud.SnapshotRestoreResponse, error) {
	f.restored = true
	return vertracloud.SnapshotRestoreResponse{Message: "restored"}, nil
}
func (f *snapshotFake) RestoreTo(context.Context, string, string, vertracloud.SnapshotScope, string, ...rest.RequestOpt) (vertracloud.SnapshotRestoreResponse, error) {
	f.restored = true
	return vertracloud.SnapshotRestoreResponse{Message: "restored"}, nil
}

func TestSnapshotCreateDownloadUsesSDKAndWritesFile(t *testing.T) {
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	fake := &snapshotFake{}
	client := &vertracloud.Client{Snapshots: fake}
	got, err := Run(context.Background(), client, "snapshot", []string{"create", "app-1", "--download"})
	if err != nil {
		t.Fatal(err)
	}
	if !fake.created || !fake.download {
		t.Fatalf("SDK calls: create=%v download=%v", fake.created, fake.download)
	}
	if _, err := os.Stat(filepath.Join(dir, "snap-1.zip")); err != nil {
		t.Fatalf("snapshot file: %v", err)
	}
	result, ok := got.(map[string]any)
	if !ok || result["downloaded_file"] != "snap-1.zip" {
		t.Fatalf("result = %#v", got)
	}
}

func TestBooleanFlagsMayPrecedePositionals(t *testing.T) {
	fake := &snapshotFake{}
	client := &vertracloud.Client{Snapshots: fake}
	if _, err := Run(context.Background(), client, "snapshot", []string{"list", "--db", "database-1"}); err != nil {
		t.Fatal(err)
	}
}

func TestDestructiveCommandsRequireYes(t *testing.T) {
	fake := &snapshotFake{}
	client := &vertracloud.Client{Snapshots: fake}
	for _, tc := range []struct {
		group string
		args  []string
	}{
		{"snapshot", []string{"restore", "app", "snap"}},
		{"folder", []string{"delete", "folder"}},
	} {
		if _, err := Run(context.Background(), client, tc.group, tc.args); err == nil {
			t.Errorf("%s %v accepted without --yes", tc.group, tc.args)
		}
	}
	if _, err := Run(context.Background(), client, "snapshot", []string{"restore", "app", "snap", "--yes"}); err != nil {
		t.Fatalf("restore with trailing --yes: %v", err)
	}
	if !fake.restored {
		t.Fatal("restore was not sent through the SDK")
	}
}

func TestUnknownCommandsAreExplicit(t *testing.T) {
	client := &vertracloud.Client{}
	_, err := Run(context.Background(), client, "workspace", []string{"unknown"})
	if err == nil || !strings.Contains(err.Error(), "unknown workspace command") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInvalidAndTrailingArgumentsFailBeforeSDK(t *testing.T) {
	client := &vertracloud.Client{Snapshots: &snapshotFake{}}
	for _, tc := range [][]string{
		{"folder", "create", "name", "--color"},
		{"snapshot", "download", "app", "snap", "extra"},
	} {
		if _, err := Run(context.Background(), client, tc[0], tc[1:]); err == nil {
			t.Errorf("accepted invalid args: %v", tc)
		}
	}
}

type workspacesFake struct {
	vertracloud.WorkspacesService
	actionRequests *actionRequestsFake
}

func (f *workspacesFake) ActionRequests() vertracloud.WorkspacesActionRequestsService {
	return f.actionRequests
}

type actionRequestsFake struct {
	created *vertracloud.WorkspaceActionRequestCreateBody
}

func (f *actionRequestsFake) List(context.Context, string, *vertracloud.WorkspaceActionRequestListParams, ...rest.RequestOpt) ([]vertracloud.WorkspaceActionRequest, error) {
	return nil, nil
}
func (f *actionRequestsFake) Create(_ context.Context, _ string, body vertracloud.WorkspaceActionRequestCreateBody, _ ...rest.RequestOpt) (vertracloud.WorkspaceActionRequest, error) {
	f.created = &body
	return vertracloud.WorkspaceActionRequest{ID: "ar-1"}, nil
}

func TestWorkspaceActionRequestCreateValidatesParams(t *testing.T) {
	ar := &actionRequestsFake{}
	client := &vertracloud.Client{Workspaces: &workspacesFake{actionRequests: ar}}
	ctx := context.Background()
	for _, args := range [][]string{
		{"action-request", "create", "ws", "snapshot_restore", "app"},
		{"action-request", "create", "ws", "app_delete", "app", "--snapshot", "snap"},
		{"action-request", "create", "ws", "nope", "app"},
		{"action-request", "list", "ws", "--status", "nope"},
		{"invite", "decline", "tok"},
	} {
		if _, err := Run(ctx, client, "workspace", args); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	if ar.created != nil {
		t.Fatal("invalid input reached the SDK")
	}
	if _, err := Run(ctx, client, "workspace", []string{"action-request", "create", "ws", "snapshot_restore", "app", "--snapshot", "snap"}); err != nil {
		t.Fatal(err)
	}
	if ar.created == nil {
		t.Fatal("action request body was not passed to the SDK")
	}
	params, ok := ar.created.Params.(vertracloud.WorkspaceActionRequestSnapshotRestoreParams)
	if !ok || params.SnapshotID != "snap" || ar.created.ResourceID != "app" {
		t.Fatalf("body = %+v", ar.created)
	}
}
