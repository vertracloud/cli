package ui

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRenderLocalizedTableAndDates(t *testing.T) {
	var out bytes.Buffer
	Render(&out, []any{
		map[string]any{"name": "demo", "created_at": "2026-09-22T12:00:00Z"},
		map[string]any{"name": "worker", "created_at": "2026-09-21T12:00:00Z"},
	}, "snapshot list", "pt")
	got := out.String()
	if !strings.Contains(got, "NOME") || !strings.Contains(got, "CRIADO EM") || !strings.Contains(got, "demo") || strings.Contains(got, "2026-09-22T12:00:00Z") {
		t.Fatalf("unexpected localized table:\n%s", got)
	}
}

func TestRenderSpanishDetail(t *testing.T) {
	var out bytes.Buffer
	Render(&out, map[string]any{"name": "demo", "status": "up", "updated_at": "2026-09-22T12:00:00Z"}, "app info", "es")
	if !strings.Contains(out.String(), "Estado") || !strings.Contains(out.String(), "● activa") || !strings.Contains(out.String(), "Actualizado") {
		t.Fatalf("unexpected Spanish detail:\n%s", out.String())
	}
}

func TestReadMaskedFailsWithoutTTY(t *testing.T) {
	t.Setenv("CI", "1")
	old := os.Stdin
	file, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	os.Stdin = file
	defer func() { os.Stdin = old }()
	if _, err := ReadMasked("Token: "); err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("expected terminal error, got %v", err)
	}
}

func TestHumanRenderRedactsCredentialFields(t *testing.T) {
	var out bytes.Buffer
	Render(&out, map[string]any{"name": "db", "password": "should-not-print", "private_key": "also-secret"}, "db credentials reset", "en")
	if strings.Contains(out.String(), "should-not-print") || strings.Contains(out.String(), "also-secret") || strings.Count(out.String(), "[redacted]") != 2 {
		t.Fatalf("credentials leaked in human output: %s", out.String())
	}
}

func TestPasswordResetShowsNewPassword(t *testing.T) {
	var out bytes.Buffer
	Render(&out, map[string]any{"password": "new-password", "private_key": "secret"}, "db credentials reset password db-1", "en")
	if !strings.Contains(out.String(), "new-password") || strings.Contains(out.String(), "secret") {
		t.Fatalf("reset output = %s", out.String())
	}
}

func TestLoginHumanOutputSummarizesAccount(t *testing.T) {
	var out bytes.Buffer
	Render(&out, map[string]any{
		"name":         "Teste",
		"applications": []any{map[string]any{"name": "private-app"}},
		"plan":         map[string]any{"name": "pro"},
	}, "auth login", "pt")
	if got := out.String(); got != "✓ Login realizado como Teste.\n" {
		t.Fatalf("login output = %q", got)
	}
}

func TestWhoamiHumanOutputSummarizesAccount(t *testing.T) {
	var out bytes.Buffer
	Render(&out, map[string]any{"name": "Teste", "plan": map[string]any{"name": "pro"}, "applications": []any{map[string]any{"name": "private-app"}}}, "auth whoami", "pt")
	if got := out.String(); !strings.Contains(got, "Teste") || !strings.Contains(got, "Plano") || !strings.Contains(got, "pro") || strings.Contains(got, "private-app") {
		t.Fatalf("whoami output = %q", got)
	}
}

func TestAppListRendersOnlyUsefulColumns(t *testing.T) {
	var out bytes.Buffer
	RenderPaged(&out, []any{map[string]any{
		"name": "demo", "id": "app-1", "ram": 512, "status": "down", "language": "go",
		"subdomain": "demo.vertraweb.app", "custom_domain": nil, "owner_id": "private-owner",
		"created_at": "2026-09-22T12:00:00Z", "start_command": nil,
	}}, "app list", "pt", 5, false)
	got := out.String()
	for _, want := range []string{"NOME", "ID", "MEMÓRIA", "ESTADO", "LINGUAGEM", "DOMÍNIO", "demo", "app-1", "512 MB", "demo.vertraweb.app"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"OWNER", "private-owner", "CRIADO", "START", "<nil>", "map["} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("unexpected %q in:\n%s", unwanted, got)
		}
	}
}

func TestDatabaseListUsesReadableTypeAndMemory(t *testing.T) {
	var out bytes.Buffer
	RenderPaged(&out, []any{map[string]any{"name": "db", "id": "db-1", "type": 3, "ram": 512, "status": "up", "owner_id": "private-owner"}}, "db list", "pt", 5, false)
	got := out.String()
	if !strings.Contains(got, "Redis") || !strings.Contains(got, "512 MB") || !strings.Contains(got, "online") || strings.Contains(got, "Owner") {
		t.Fatalf("database list output = %s", got)
	}
}

func TestAppInfoOmitsInactiveFeaturesAndFormatsMemoryAndSubdomain(t *testing.T) {
	var out bytes.Buffer
	Render(&out, map[string]any{"name": "next-build", "ram": float64(612), "subdomain": nil, "custom_domain": nil, "shield_cooldown": nil, "auto_restart": true}, "app info", "pt")
	got := out.String()
	for _, want := range []string{"612 MB", "Não configurado", "Ativado", "Domínio personalizado"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"shield_cooldown", "<nil>"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("unexpected %q in:\n%s", unwanted, got)
		}
	}
}

func TestAppListShowsFoldersAndFavorites(t *testing.T) {
	var out bytes.Buffer
	Render(&out, []any{map[string]any{"name": "demo", "id": "app-1", "ram": float64(512), "status": "up", "language": "go", "favorite": true, "folder": "Produção"}}, "app list", "pt")
	got := out.String()
	for _, want := range []string{"★ demo", "PASTA", "Produção", "● online"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestStatusAllDerivesStateFromRunning(t *testing.T) {
	var out bytes.Buffer
	Render(&out, []any{
		map[string]any{"id": "a", "cpu": "1%", "ram": "10 MB", "running": true, "uptime": float64(90000)},
		map[string]any{"id": "b", "cpu": "0%", "ram": "0 MB", "running": false, "uptime": nil},
	}, "app status --all", "en")
	got := out.String()
	for _, want := range []string{"STATUS", "● online", "○ offline", "1d 1h"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "RUNNING") || strings.Contains(got, "false") {
		t.Fatalf("raw running flag leaked:\n%s", got)
	}
}

func TestDetailSummarizesNestedValuesAndHidesInternals(t *testing.T) {
	var out bytes.Buffer
	Render(&out, map[string]any{
		"name": "ws", "owner_id": "secret-owner", "owner": map[string]any{"display_name": "alice"},
		"roles": []any{map[string]any{"name": "Admin"}, map[string]any{"name": "Viewer"}}, "description": nil,
	}, "workspace info ws-1", "en")
	got := out.String()
	for _, want := range []string{"alice", "Admin, Viewer"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"secret-owner", "map[", "Description"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("unexpected %q in:\n%s", unwanted, got)
		}
	}
}

func TestActionsPrintSuccessLine(t *testing.T) {
	var out bytes.Buffer
	Render(&out, map[string]any{"status": "success"}, "app restart abc", "pt")
	if got := out.String(); got != "✓ Aplicação reiniciada.\n" {
		t.Fatalf("restart output = %q", got)
	}
	out.Reset()
	Render(&out, nil, "folder delete f1 --yes", "en")
	if got := out.String(); got != "✓ Done.\n" {
		t.Fatalf("generic action output = %q", got)
	}
}

func TestNormalizeFoldsAliases(t *testing.T) {
	for in, want := range map[string]string{"apps ls --all": "app list", "deploy abc": "app commit", "me": "auth whoami", "app env unset K": "app env delete", "link --show": "link show", "database get x": "db info"} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAppListShowsLiveUsageFromStatusCall(t *testing.T) {
	var out bytes.Buffer
	Render(&out, []any{
		map[string]any{"name": "api", "id": "a", "status": "up", "cpu": "3.20%", "ram": float64(512), "ram_used": "45.10 MB", "language": "go"},
		map[string]any{"name": "bot", "id": "b", "status": "down", "cpu": "0.00%", "ram": float64(256), "ram_used": "0.00 MB", "language": "python"},
	}, "app list", "en")
	got := out.String()
	for _, want := range []string{"CPU", "3.20%", "45.10 / 512 MB", "256 MB", "● online", "○ offline", "2 applications · 1 online"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "0.00%") {
		t.Fatalf("stopped app should not show CPU:\n%s", got)
	}
}

func TestProjectsMergesAppsAndDatabases(t *testing.T) {
	var out bytes.Buffer
	Render(&out, []any{
		map[string]any{"name": "api", "id": "a", "kind": "application", "runtime": "go", "status": "up", "cpu": "1%", "ram": float64(512), "ram_used": "40 MB"},
		map[string]any{"name": "cache", "id": "d", "kind": "database", "type": float64(3), "status": "down", "ram": float64(256)},
	}, "projects", "pt")
	got := out.String()
	for _, want := range []string{"App", "Banco", "Redis", "go", "40 / 512 MB", "1 aplicação · 1 banco de dados · 1 online"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestStatusShowsNameAndMemoryLimit(t *testing.T) {
	var out bytes.Buffer
	status := map[string]any{"id": "a", "status": "up", "running": true, "cpu": "0.01%", "ram": "175.91 MB"}
	Render(&out, WithResources(status, map[string]Resource{"a": {Name: "api", RAM: 612}}), "app status a", "en")
	if got := out.String(); !strings.Contains(got, "175.91 / 612 MB") || !strings.Contains(got, "api") {
		t.Fatalf("status output:\n%s", got)
	}
	out.Reset()
	Render(&out, WithResources([]any{map[string]any{"id": "b", "running": false, "cpu": "0%", "ram": "0.00 MB"}}, map[string]Resource{"b": {Name: "bot", RAM: 256}}), "db status --all", "en")
	if got := out.String(); !strings.Contains(got, "bot") || !strings.Contains(got, "256 MB") || strings.Contains(got, "0.00 MB") {
		t.Fatalf("status --all output:\n%s", got)
	}
}

func TestShieldPauseIsShownInListAndDetail(t *testing.T) {
	until := time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339)
	shield := map[string]any{"until": until, "reason": "burst", "direction": "in", "strikes": float64(2)}
	var out bytes.Buffer
	Render(&out, []any{map[string]any{"name": "api", "id": "a", "status": "down", "ram": float64(512), "shield_cooldown": shield}}, "app list", "pt")
	if got := out.String(); !strings.Contains(got, "⛨ shield") || !strings.Contains(got, "1 pausado(s) pelo Vertra Shield") {
		t.Fatalf("list output:\n%s", got)
	}
	out.Reset()
	Render(&out, map[string]any{"name": "api", "id": "a", "status": "down", "shield_cooldown": shield}, "app info a", "pt")
	got := out.String()
	for _, want := range []string{"Vertra Shield", "Pausado até", "Ocorrência nº 2", "pico de tráfego de entrada", "aumentar a RAM"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	out.Reset()
	expired := map[string]any{"until": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), "reason": "burst", "direction": "in"}
	Render(&out, map[string]any{"name": "api", "id": "a", "status": "down", "shield_cooldown": expired}, "app info a", "pt")
	if strings.Contains(out.String(), "Shield") {
		t.Fatalf("expired cooldown shown:\n%s", out.String())
	}
}

func TestMemoryColorThresholds(t *testing.T) {
	if memoryColor("100 MB", 512) != nil || memoryColor("420 MB", 512) == nil || memoryColor("500.00 MB", 512) == nil {
		t.Fatal("unexpected memory thresholds")
	}
	old := colorOn
	colorOn = true
	defer func() { colorOn = old }()
	if memoryColor("420 MB", 512)("x") != Yellow("x") || memoryColor("500 MB", 512)("x") != Red("x") {
		t.Fatal("80% should be yellow and 95% red")
	}
}
