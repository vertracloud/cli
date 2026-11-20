package resources

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

type snapshotFake struct {
	created  bool
	download bool
	restored bool

	createErr  error
	createJob  string
	restoreJob string
	restoreTo  string
	waitJob    vertracloud.SnapshotJob
	waitErr    error
	waitOpts   *vertracloud.WaitOptions
	waited     []string
	jobsKind   vertracloud.SnapshotJobKind
}

// jobsFake is the SnapshotJobsService over the state of a snapshotFake.
type jobsFake struct{ f *snapshotFake }

func (j jobsFake) List(_ context.Context, _ string, _ vertracloud.SnapshotScope, params *vertracloud.SnapshotJobsParams, _ ...rest.RequestOpt) ([]vertracloud.SnapshotJob, error) {
	j.f.jobsKind = params.Kind
	return nil, nil
}
func (j jobsFake) Get(_ context.Context, resourceID, jobID string, _ vertracloud.SnapshotScope, _ ...rest.RequestOpt) (vertracloud.SnapshotJob, error) {
	return vertracloud.SnapshotJob{ID: jobID, ResourceID: resourceID, Status: vertracloud.SnapshotJobStatusRunning}, nil
}
func (j jobsFake) Wait(_ context.Context, resourceID, jobID string, _ vertracloud.SnapshotScope, opts *vertracloud.WaitOptions, _ ...rest.RequestOpt) (vertracloud.SnapshotJob, error) {
	f := j.f
	f.waited = append(f.waited, resourceID+"/"+jobID)
	f.waitOpts = opts
	job := f.waitJob
	job.ID = jobID
	if f.waitErr != nil {
		return job, f.waitErr
	}
	if job.Status == vertracloud.SnapshotJobStatusFailed || job.Status == vertracloud.SnapshotJobStatusSkipped {
		reason := job.Reason
		if reason == "" {
			reason = vertracloud.ErrCodeSnapshotFailed
		}
		return job, &vertracloud.SnapshotJobError{Status: job.Status, Reason: reason, Details: job.Details, Job: job}
	}
	return job, nil
}

func clientFor(f *snapshotFake) *vertracloud.Client {
	return &vertracloud.Client{Snapshots: f, SnapshotJobs: jobsFake{f}}
}

func (f *snapshotFake) CreateAndWait(ctx context.Context, resourceID string, scope vertracloud.SnapshotScope, opts *vertracloud.WaitOptions, _ ...rest.RequestOpt) (vertracloud.ResourceSnapshot, error) {
	created, err := f.Create(ctx, resourceID, scope)
	if err != nil || !created.Accepted() {
		return created.Snapshot, err
	}
	job, err := jobsFake{f}.Wait(ctx, resourceID, created.JobID, scope, opts)
	if err != nil {
		return vertracloud.ResourceSnapshot{}, err
	}
	return vertracloud.ResourceSnapshot{ID: job.SnapshotID, ResourceID: resourceID}, nil
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
func (f *snapshotFake) DownloadToFile(ctx context.Context, resourceID, snapshotID string, scope vertracloud.SnapshotScope, path string, opts ...rest.RequestOpt) (int64, error) {
	body, err := f.Download(ctx, resourceID, snapshotID, scope, opts...)
	if err != nil {
		return 0, err
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return 0, err
	}
	return int64(len(data)), os.WriteFile(path, data, 0o600)
}
func (f *snapshotFake) Create(context.Context, string, vertracloud.SnapshotScope, ...rest.RequestOpt) (vertracloud.SnapshotCreateResult, error) {
	f.created = true
	if f.createErr != nil {
		return vertracloud.SnapshotCreateResult{}, f.createErr
	}
	if f.createJob != "" {
		return vertracloud.SnapshotCreateResult{JobID: f.createJob}, nil
	}
	return vertracloud.SnapshotCreateResult{Snapshot: vertracloud.ResourceSnapshot{ID: "snap-1"}}, nil
}
func (f *snapshotFake) Restore(context.Context, string, string, vertracloud.SnapshotScope, ...rest.RequestOpt) (vertracloud.SnapshotRestoreResponse, error) {
	f.restored = true
	return vertracloud.SnapshotRestoreResponse{JobID: f.restoreJob}, nil
}
func (f *snapshotFake) RestoreWith(_ context.Context, _, _ string, _ vertracloud.SnapshotScope, params vertracloud.SnapshotRestoreParams, _ ...rest.RequestOpt) (vertracloud.SnapshotRestoreResponse, error) {
	f.restored = true
	f.restoreTo = params.TargetResourceID
	return vertracloud.SnapshotRestoreResponse{JobID: f.restoreJob}, nil
}
func (f *snapshotFake) RestoreTo(context.Context, string, string, vertracloud.SnapshotScope, string, ...rest.RequestOpt) (vertracloud.SnapshotRestoreResponse, error) {
	f.restored = true
	return vertracloud.SnapshotRestoreResponse{JobID: f.restoreJob}, nil
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
	client := clientFor(fake)
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
	client := clientFor(fake)
	if _, err := Run(context.Background(), client, "snapshot", []string{"list", "--db", "database-1"}); err != nil {
		t.Fatal(err)
	}
}

func TestDestructiveCommandsRequireYes(t *testing.T) {
	fake := &snapshotFake{}
	client := clientFor(fake)
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

func TestSnapshotRestoreWaitsForTheJobByDefault(t *testing.T) {
	fake := &snapshotFake{restoreJob: "job-1", waitJob: vertracloud.SnapshotJob{Status: vertracloud.SnapshotJobStatusReady}}
	got, err := Run(context.Background(), clientFor(fake), "snapshot", []string{"restore", "app-1", "snap-1", "--yes"})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := got.(map[string]any)
	if !ok || result["job_id"] != "job-1" || result["status"] != "ready" {
		t.Fatalf("result = %#v", got)
	}
	if len(fake.waited) != 1 || fake.waited[0] != "app-1/job-1" {
		t.Fatalf("waited = %v, want app-1/job-1", fake.waited)
	}
}

func TestSnapshotRestoreNoWaitPrintsJobIDAndDoesNotWait(t *testing.T) {
	fake := &snapshotFake{restoreJob: "job-1"}
	got, err := Run(context.Background(), clientFor(fake), "snapshot", []string{"restore", "app-1", "snap-1", "--no-wait", "--yes"})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := got.(map[string]any)
	if !ok || result["job_id"] != "job-1" {
		t.Fatalf("result = %#v", got)
	}
	if len(fake.waited) != 0 {
		t.Fatalf("waited with --no-wait: %v", fake.waited)
	}
}

func TestSnapshotRestoreWaitFollowsTheJobOfTheTarget(t *testing.T) {
	fake := &snapshotFake{restoreJob: "job-1", waitJob: vertracloud.SnapshotJob{Status: vertracloud.SnapshotJobStatusReady}}
	got, err := Run(context.Background(), clientFor(fake), "snapshot", []string{"restore", "app-1", "snap-1", "--to", "app-2", "--yes"})
	if err != nil {
		t.Fatal(err)
	}
	result := got.(map[string]any)
	if result["job_id"] != "job-1" || result["status"] != "ready" {
		t.Fatalf("result = %#v", result)
	}
	if len(fake.waited) != 1 || fake.waited[0] != "app-2/job-1" {
		t.Fatalf("waited = %v, want the target app-2", fake.waited)
	}
}

func TestSnapshotRestoreWaitFailedJobIsAnErrorWithTheReason(t *testing.T) {
	fake := &snapshotFake{restoreJob: "job-1", waitJob: vertracloud.SnapshotJob{Status: vertracloud.SnapshotJobStatusFailed, Reason: "SNAPSHOT_CORRUPTED"}}
	client := clientFor(fake)
	_, err := Run(context.Background(), client, "snapshot", []string{"restore", "app-1", "snap-1", "--yes"})
	var apiErr *rest.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "SNAPSHOT_CORRUPTED" {
		t.Fatalf("err = %v, want SNAPSHOT_CORRUPTED", err)
	}
}

func TestSnapshotWaitFailedAndSkippedJobsKeepReasonAndDetails(t *testing.T) {
	details := &vertracloud.SnapshotJobDetails{SnapshotExceedsStorage: vertracloud.SnapshotExceedsStorage{RequiredBytes: 900, AvailableBytes: 100}}
	fake := &snapshotFake{waitJob: vertracloud.SnapshotJob{Status: vertracloud.SnapshotJobStatusFailed, Reason: vertracloud.ErrCodeSnapshotExceedsStorage, Details: details}}
	client := clientFor(fake)
	_, err := Run(context.Background(), client, "snapshot", []string{"job", "app-1", "job-1", "--wait"})
	got, ok := vertracloud.SnapshotExceedsStorageDetails(err)
	if !ok || got.RequiredBytes != 900 || got.AvailableBytes != 100 {
		t.Fatalf("err = %v, details = %#v", err, got)
	}

	fake = &snapshotFake{waitJob: vertracloud.SnapshotJob{Status: vertracloud.SnapshotJobStatusSkipped, Reason: vertracloud.ErrCodeDatabaseNotReady}}
	client = clientFor(fake)
	_, err = Run(context.Background(), client, "snapshot", []string{"job", "db-1", "job-1", "--db", "--wait"})
	var apiErr *rest.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != vertracloud.ErrCodeDatabaseNotReady {
		t.Fatalf("a skipped job must be an error with its reason, got %v", err)
	}
}

func TestSnapshotTimeoutFlagReachesTheSDKAndATimedOutWaitSaysHowToFollow(t *testing.T) {
	fake := &snapshotFake{waitJob: vertracloud.SnapshotJob{Status: vertracloud.SnapshotJobStatusRunning}}
	client := clientFor(fake)
	if _, err := Run(context.Background(), client, "snapshot", []string{"job", "app-1", "job-1", "--wait", "--timeout", "45m"}); err != nil {
		t.Fatal(err)
	}
	if fake.waitOpts == nil || fake.waitOpts.Timeout != 45*time.Minute || fake.waitOpts.OnProgress == nil {
		t.Fatalf("options = %#v", fake.waitOpts)
	}
	if _, err := Run(context.Background(), client, "snapshot", []string{"job", "app-1", "job-1", "--wait"}); err != nil || fake.waitOpts.Timeout != 0 {
		t.Fatalf("default must leave the SDK default (0), got %v, err = %v", fake.waitOpts.Timeout, err)
	}
	if _, err := Run(context.Background(), client, "snapshot", []string{"job", "app-1", "job-1", "--wait", "--timeout", "0"}); err != nil || fake.waitOpts.Timeout >= 0 {
		t.Fatalf("--timeout 0 must remove the limit, got %v, err = %v", fake.waitOpts.Timeout, err)
	}
	for _, bad := range []string{"soon", "-5m", "90"} {
		if _, err := Run(context.Background(), client, "snapshot", []string{"job", "app-1", "job-1", "--wait", "--timeout", bad}); err == nil {
			t.Fatalf("--timeout %q accepted", bad)
		}
	}

	job := vertracloud.SnapshotJob{ID: "job-1", Status: vertracloud.SnapshotJobStatusRunning}
	fake.waitErr = &vertracloud.WaitTimeoutError{Timeout: time.Minute, Job: &job}
	_, err := Run(context.Background(), client, "snapshot", []string{"job", "db-1", "job-1", "--db", "--wait"})
	if err == nil || !strings.Contains(err.Error(), "vertra snapshot job db-1 job-1 --db --wait") {
		t.Fatalf("err = %v, want the command to follow the job", err)
	}
}

func TestProgressText(t *testing.T) {
	pct, eta := 42.0, 185.0
	cases := []struct {
		job  vertracloud.SnapshotJob
		want string
	}{
		{vertracloud.SnapshotJob{Status: vertracloud.SnapshotJobStatusQueued}, "queued"},
		{vertracloud.SnapshotJob{Status: vertracloud.SnapshotJobStatusRunning, Progress: &vertracloud.SnapshotJobProgress{Phase: vertracloud.SnapshotJobPhasePreparing}}, "preparing"},
		{vertracloud.SnapshotJob{Status: vertracloud.SnapshotJobStatusRunning, Progress: &vertracloud.SnapshotJobProgress{Phase: vertracloud.SnapshotJobPhaseCopying, Percent: &pct, DoneBytes: 1 << 30, TotalBytes: 2 << 30, ETASeconds: &eta}}, "copying · 42% · 1.0 GiB / 2.0 GiB · ~3 min left"},
		{vertracloud.SnapshotJob{Status: vertracloud.SnapshotJobStatusRunning, Progress: &vertracloud.SnapshotJobProgress{Phase: vertracloud.SnapshotJobPhaseCopying, DoneBytes: 5 << 20}}, "copying · 5.0 MiB"},
	}
	for _, tc := range cases {
		if got := progressText(tc.job); got != tc.want {
			t.Errorf("progressText = %q, want %q", got, tc.want)
		}
	}
}

func TestSnapshotJobCommand(t *testing.T) {
	fake := &snapshotFake{waitJob: vertracloud.SnapshotJob{Status: vertracloud.SnapshotJobStatusReady}}
	client := clientFor(fake)
	if _, err := Run(context.Background(), client, "snapshot", []string{"job", "db-1", "job-1", "--db"}); err != nil {
		t.Fatal(err)
	}
	if len(fake.waited) != 0 {
		t.Fatal("job without --wait must not poll")
	}
	if _, err := Run(context.Background(), client, "snapshot", []string{"job", "db-1", "job-1", "--db", "--wait"}); err != nil || len(fake.waited) != 1 {
		t.Fatalf("err = %v, waited = %v", err, fake.waited)
	}
}

func TestSnapshotCreateFollowsTheJobOnAcceptedResult(t *testing.T) {
	old, _ := os.Getwd()
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	fake := &snapshotFake{createJob: "job-5", waitJob: vertracloud.SnapshotJob{Status: vertracloud.SnapshotJobStatusReady, SnapshotID: "snap-9"}}
	client := clientFor(fake)
	got, err := Run(context.Background(), client, "snapshot", []string{"create", "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	if snap, ok := got.(vertracloud.ResourceSnapshot); !ok || snap.ID != "snap-9" {
		t.Fatalf("result = %#v", got)
	}
	if len(fake.waited) != 1 || fake.waited[0] != "app-1/job-5" {
		t.Fatalf("waited = %v", fake.waited)
	}
}

func TestSnapshotJobsListsByKind(t *testing.T) {
	fake := &snapshotFake{}
	client := clientFor(fake)
	if _, err := Run(context.Background(), client, "snapshot", []string{"jobs", "app-1", "--kind", "restore"}); err != nil {
		t.Fatal(err)
	}
	if fake.jobsKind != vertracloud.SnapshotJobKindRestore {
		t.Fatalf("kind = %q", fake.jobsKind)
	}
	if _, err := Run(context.Background(), client, "snapshot", []string{"jobs", "app-1", "--kind", "nope"}); err == nil {
		t.Fatal("invalid kind accepted")
	}
}

// The real SDK flow: the zip is written through a temporary file, so a
// download that breaks halfway leaves nothing behind (and keeps an existing
// file untouched), and a good one replaces it.
func TestSnapshotDownloadWritesThroughATemporaryFile(t *testing.T) {
	broken := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if broken {
			w.Header().Set("Content-Length", "1000")
			_, _ = w.Write([]byte("half"))
			w.(http.Flusher).Flush()
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				_ = conn.Close()
			}
			return
		}
		_, _ = w.Write([]byte("whole-zip"))
	}))
	defer srv.Close()
	rc, err := rest.NewClient("test-key", rest.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	client := vertracloud.New(rc)

	dir := t.TempDir()
	file := filepath.Join(dir, "out.zip")
	if err := os.WriteFile(file, []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), client, "snapshot", []string{"download", "res-1", "snap-1", "-o", file}); err == nil {
		t.Fatal("a broken download must fail")
	}
	if got, _ := os.ReadFile(file); string(got) != "previous" {
		t.Fatalf("existing file = %q, want it untouched", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("leftovers in %s: %v", dir, entries)
	}

	broken = false
	if _, err := Run(context.Background(), client, "snapshot", []string{"download", "res-1", "snap-1", "-o", file}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(file); string(got) != "whole-zip" {
		t.Fatalf("file = %q", got)
	}
}

// workspaceRecorder answers the snapshot routes and records, per call, the
// workspace_id query the CLI sent.
type workspaceRecorder struct {
	mu    sync.Mutex
	calls []string // "METHOD path -> workspace_id"
}

func (w *workspaceRecorder) handler(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	w.calls = append(w.calls, r.Method+" "+r.URL.Path+" -> "+r.URL.Query().Get("workspace_id"))
	w.mu.Unlock()
	path := r.URL.Path
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/restore"):
		rw.WriteHeader(http.StatusAccepted)
		_, _ = rw.Write([]byte(`{"response":{"job_id":"job-r"}}`))
	case r.Method == http.MethodPost:
		rw.WriteHeader(http.StatusAccepted)
		_, _ = rw.Write([]byte(`{"response":{"job_id":"job-1"}}`))
	case strings.HasSuffix(path, "/download"):
		_, _ = rw.Write([]byte("zip-bytes"))
	case strings.Contains(path, "/snapshots/jobs/"):
		_, _ = rw.Write([]byte(`{"response":{"id":"job-1","kind":"snapshot","resource_id":"app-1","status":"ready","snapshot_id":"snap-1","created_at":"2026-11-19T10:00:00Z"}}`))
	case strings.HasSuffix(path, "/snapshots/jobs"):
		_, _ = rw.Write([]byte(`{"response":[]}`))
	default:
		_, _ = rw.Write([]byte(`{"response":[{"id":"snap-1","resource_id":"app-1","resource_type":null,"size":"1","date":"2026-11-19T10:00:00Z","resource_name":null}]}`))
	}
}

func (w *workspaceRecorder) assertAll(t *testing.T, want string, minCalls int) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.calls) < minCalls {
		t.Fatalf("calls = %v, want at least %d", w.calls, minCalls)
	}
	for _, c := range w.calls {
		if !strings.HasSuffix(c, " -> "+want) {
			t.Fatalf("call %q lacks workspace_id=%q (all: %v)", c, want, w.calls)
		}
	}
}

func recordingClient(t *testing.T) (*vertracloud.Client, *workspaceRecorder) {
	t.Helper()
	rec := &workspaceRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(rec.handler))
	t.Cleanup(srv.Close)
	rc, err := rest.NewClient("test-key", rest.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	return vertracloud.New(rc), rec
}

func TestSnapshotCommandsSendWorkspaceID(t *testing.T) {
	old := jobPollInterval
	jobPollInterval = time.Millisecond
	t.Cleanup(func() { jobPollInterval = old })

	cases := []struct {
		name  string
		args  []string
		calls int
	}{
		{"list all", []string{"list", "--workspace", "ws-1"}, 1},
		{"list resource", []string{"list", "app-1", "--workspace", "ws-1"}, 1},
		{"create", []string{"create", "app-1", "--workspace", "ws-1"}, 3},
		{"create download", []string{"create", "app-1", "--download", "--workspace", "ws-1"}, 4},
		{"download", []string{"download", "app-1", "snap-1", "--workspace", "ws-1"}, 1},
		{"job", []string{"job", "app-1", "job-1", "--workspace", "ws-1"}, 1},
		{"job wait", []string{"job", "app-1", "job-1", "--wait", "--workspace", "ws-1"}, 1},
		{"jobs", []string{"jobs", "app-1", "--workspace", "ws-1"}, 1},
		{"restore", []string{"restore", "app-1", "snap-1", "--yes", "--workspace", "ws-1"}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			old, _ := os.Getwd()
			if err := os.Chdir(dir); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chdir(old) })
			client, rec := recordingClient(t)
			if _, err := Run(context.Background(), client, "snapshot", tc.args); err != nil {
				t.Fatal(err)
			}
			rec.assertAll(t, "ws-1", tc.calls)
		})
	}
}

func TestSnapshotWithoutWorkspaceSendsNone(t *testing.T) {
	client, rec := recordingClient(t)
	if _, err := Run(context.Background(), client, "snapshot", []string{"list", "app-1"}); err != nil {
		t.Fatal(err)
	}
	rec.assertAll(t, "", 1)
}

func TestSnapshotCreateWithWorkspaceNeedsResourceID(t *testing.T) {
	client, rec := recordingClient(t)
	_, err := Run(context.Background(), client, "snapshot", []string{"create", "--workspace", "ws-1"})
	if err == nil || !strings.Contains(err.Error(), "--workspace") {
		t.Fatalf("err = %v, want a resource id required with --workspace", err)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("calls = %v, want none", rec.calls)
	}
}
