package databases

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

type lifecycleFake struct {
	vertracloud.DatabasesService
	called string
}

func (f *lifecycleFake) Start(_ context.Context, id string, _ ...rest.RequestOpt) (vertracloud.DatabaseOperationResponse, error) {
	f.called = "start:" + id
	return vertracloud.DatabaseOperationResponse{}, nil
}

func (f *lifecycleFake) Stop(_ context.Context, id string, _ ...rest.RequestOpt) (vertracloud.DatabaseOperationResponse, error) {
	f.called = "stop:" + id
	return vertracloud.DatabaseOperationResponse{}, nil
}

func TestLifecycleReturnsNodeCompatibleShape(t *testing.T) {
	fake := &lifecycleFake{}
	client := &vertracloud.Client{Databases: fake}
	for _, action := range []string{"start", "stop"} {
		got, err := Run(context.Background(), client, action, []string{"db-1"})
		if err != nil {
			t.Fatal(err)
		}
		value, ok := got.(map[string]string)
		if !ok || value["id"] != "db-1" || value["action"] != action || fake.called != action+":db-1" {
			t.Fatalf("%s returned %v and called %q", action, got, fake.called)
		}
	}
}

func TestPresentDatabasesIncludesSavedFoldersAndFavorites(t *testing.T) {
	databases := []vertracloud.Database{{ID: "db-1", Name: "primary"}}
	organization := vertracloud.WorkspaceResourceOrganization{
		Folders:   []vertracloud.WorkspaceResourceFolder{{Name: "Data", Resources: []vertracloud.WorkspaceFolderItem{{WorkspaceResourceRef: vertracloud.WorkspaceResourceRef{ResourceType: vertracloud.WorkspaceResourceTypeDatabase, ResourceID: "db-1"}}}}},
		Favorites: []vertracloud.WorkspaceFavorite{{WorkspaceResourceRef: vertracloud.WorkspaceResourceRef{ResourceType: vertracloud.WorkspaceResourceTypeDatabase, ResourceID: "db-1"}}},
	}
	rows := presentDatabases(databases, organization)
	if len(rows) != 1 || rows[0]["folder"] != "Data" || rows[0]["favorite"] != true {
		t.Fatalf("human rows = %#v", rows)
	}
}

func TestMatchesDatabaseFilters(t *testing.T) {
	database := vertracloud.Database{
		Name:   "Production PostgreSQL",
		Type:   vertracloud.DatabaseTypePostgreSQL,
		Status: vertracloud.DatabaseStatusUp,
		RAM:    2048,
	}
	for _, test := range []struct {
		name    string
		filters []string
		want    bool
	}{
		{"name substring", []string{"name=postgres"}, true},
		{"type exact", []string{"type=POSTGRES"}, true},
		{"ram comparison", []string{"ram>=1024", "ram<4096"}, true},
		{"status mismatch", []string{"status=down"}, false},
		{"multiple predicates", []string{"name=prod", "status=up", "ram=2048"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := matches(database, test.filters)
			if err != nil {
				t.Fatalf("matches returned error: %v", err)
			}
			if got != test.want {
				t.Fatalf("matches(%v) = %v, want %v", test.filters, got, test.want)
			}
		})
	}
}

func TestSplitFilterRejectsMalformedExpressions(t *testing.T) {
	for _, expression := range []string{"", "status", "=up"} {
		if _, _, _, err := splitFilter(expression); err == nil {
			t.Fatalf("splitFilter(%q) accepted malformed expression", expression)
		}
	}
}

func TestMatchesRejectsUnsupportedFilterOperatorsAndValues(t *testing.T) {
	database := vertracloud.Database{RAM: 1024, Status: vertracloud.DatabaseStatusUp}
	for _, expression := range []string{"status>up", "ram>=not-a-number", "status!=up"} {
		if _, err := matches(database, []string{expression}); err == nil {
			t.Fatalf("matches(%q) accepted invalid filter", expression)
		}
	}
}

func TestDatabaseType(t *testing.T) {
	if got, err := dbType("MySQL"); err != nil || got != vertracloud.DatabaseTypeMySQL {
		t.Fatalf("dbType(MySQL) = %v, %v", got, err)
	}
	if _, err := dbType("sqlite"); err == nil {
		t.Fatal("dbType(sqlite) accepted unsupported type")
	}
}

func TestIntermixFlagsAcceptsOptionsAfterID(t *testing.T) {
	got := intermixFlags([]string{"db-1", "--name", "new-name", "--ram=2048"}, map[string]bool{"--name": true, "--ram": true}, nil)
	want := []string{"--name", "new-name", "--ram=2048", "db-1"}
	if len(got) != len(want) {
		t.Fatalf("intermixFlags returned %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("intermixFlags returned %v, want %v", got, want)
		}
	}
}

func TestProjectDatabaseID(t *testing.T) {
	directory := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.WriteFile(filepath.Join(directory, "vertracloud.config"), []byte("# local project\nID= db-42\n"), 0600); err != nil {
		t.Fatal(err)
	}
	id, err := projectDatabaseID()
	if err != nil || id != "db-42" {
		t.Fatalf("projectDatabaseID() = %q, %v", id, err)
	}
}

func TestDestructiveCommandsRequireYes(t *testing.T) {
	client := &vertracloud.Client{}
	if _, err := Run(context.Background(), client, "delete", []string{"db-1"}); err == nil {
		t.Fatal("delete without --yes was accepted")
	}
	if _, err := Run(context.Background(), client, "reset", []string{"db-1"}); err == nil {
		t.Fatal("reset without --yes was accepted")
	}
	if _, err := Run(context.Background(), client, "credentials", []string{"reset", "password", "db-1"}); err == nil {
		t.Fatal("credential reset without --yes was accepted")
	}
}

type versionsFake struct {
	vertracloud.DatabasesService
	catalog vertracloud.DatabaseVersions
	created *vertracloud.DatabaseCreateBody
}

func (f *versionsFake) Versions(_ context.Context, _ ...rest.RequestOpt) (vertracloud.DatabaseVersions, error) {
	return f.catalog, nil
}

func (f *versionsFake) Create(_ context.Context, params vertracloud.DatabaseCreateBody, _ ...rest.RequestOpt) (vertracloud.Database, error) {
	f.created = &params
	return vertracloud.Database{ID: "db-1", Version: params.Version}, nil
}

func TestVersionsOrdersByTypeAndKeepsCatalog(t *testing.T) {
	fake := &versionsFake{catalog: vertracloud.DatabaseVersions{
		"3": {Recommended: "8.2", Available: []string{"8.2", "7.2"}},
		"1": {Recommended: "18", Available: []string{"18", "17"}},
	}}
	got, err := Run(context.Background(), &vertracloud.Client{Databases: fake}, "versions", nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := got.(ui.Presentation).Human.([]map[string]any)
	if len(rows) != 2 || rows[0]["type"] != float64(1) || rows[0]["available"] != "18, 17" || rows[1]["recommended"] != "8.2" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestCreateSendsVersionOnlyWhenGiven(t *testing.T) {
	for _, test := range []struct {
		name    string
		args    []string
		version string
		sent    bool
	}{
		{"explicit version", []string{"--version", " 17 "}, "17", true},
		{"no version", nil, "", false},
		{"snapshot keeps its line", []string{"--snapshot", "snap-1"}, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &versionsFake{}
			args := append([]string{"--name", "a", "--memory", "512", "--type", "postgres"}, test.args...)
			if _, err := Run(context.Background(), &vertracloud.Client{Databases: fake}, "create", args); err != nil {
				t.Fatal(err)
			}
			if fake.created == nil || fake.created.Version != test.version {
				t.Fatalf("created = %#v, want version %q", fake.created, test.version)
			}
			payload, err := json.Marshal(fake.created)
			if err != nil {
				t.Fatal(err)
			}
			if has := strings.Contains(string(payload), `"version"`); has != test.sent {
				t.Fatalf("body = %s, version sent = %v", payload, has)
			}
		})
	}
}

func TestPresentVersionsKeepsUnknownKeys(t *testing.T) {
	rows := presentVersions(vertracloud.DatabaseVersions{"x": {Recommended: "1"}})
	if rows[0]["type"] != "x" {
		t.Fatalf("rows = %#v", rows)
	}
}
