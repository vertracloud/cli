package apps

import (
	"strings"
	"testing"

	"github.com/vertracloud/cli/internal/ui"
)

const (
	deployID   = "0A1B2C3D-0000-4000-8000-000000000001"
	previousID = "4e5f6a7b-0000-4000-8000-000000000002"
)

func withLocale(t *testing.T, locale string) {
	old := ui.Locale
	t.Cleanup(func() { ui.Locale = old })
	ui.Locale = locale
}

func TestSystemLineTranslatesWithAndWithoutTimestamp(t *testing.T) {
	withLocale(t, "en")
	want := "[Vertra] Dependency install stopped — plan memory exhausted."
	for _, in := range []string{
		"install_note:oom",
		"2026-11-24T10:00:00Z install_note:oom",
		"2026-11-24T10:00:00.123456789Z install_note:oom",
	} {
		if got := systemLine(in); got != want {
			t.Fatalf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestSystemLineInstallNotes(t *testing.T) {
	withLocale(t, "en")
	cases := map[string]string{
		"install_note:started":       "Dependency install started",
		"install_note:done":          "Installation finished",
		"install_note:build_oom":     "The build command ran out of memory and was stopped. Raise the app memory or reduce what the build processes.",
		"install_note:failed:137":    "Dependency install failed (exit code 137).",
		"install_note:failed":        "Dependency install failed (exit code ?).",
		"install_note:build_failed":  "The build command failed; the app was not started.",
		"install_note:build_timeout": "The build exceeded the 10-minute time limit.",
		"install_note:timeout:900":   "A dependency install step exceeded the 15-minute limit.",
		"install_note:timeout:1500":  "The installation exceeded the 25-minute total time limit.",
		"install_note:timeout:7":     "The installation exceeded the time limit.",
		"install_note:timeout":       "The installation exceeded the time limit.",
		"install_note:shield":        "Build stopped by network protection — traffic above the allowance during the deploy.",
	}
	for in, want := range cases {
		if got := systemLine(in); got != "[Vertra] "+want {
			t.Fatalf("%q: got %q", in, got)
		}
	}
}

func TestSystemLineDeployDiscarded(t *testing.T) {
	withLocale(t, "en")
	causes := map[string]string{
		"install_oom":           "it ran out of memory while installing dependencies",
		"install_build_oom":     "it ran out of memory during the build",
		"install_failed":        "the dependency install failed",
		"install_build_failed":  "the build command failed",
		"install_build_timeout": "the build went over the time limit",
		"install_timeout":       "the install went over the time limit",
		"install_shield":        "network protection stopped the install",
		"crashed":               "it stopped before it was ready",
		"unhealthy":             "it did not become ready to receive traffic",
		"timeout":               "it did not respond in time",
	}
	for reason, cause := range causes {
		want := "[Vertra] Deploy 0a1b2c3 was discarded: " + cause + ". The previous version is still online."
		if got := systemLine("deploy_note:discarded:" + deployID + ":" + reason); got != want {
			t.Fatalf("%s: got %q", reason, got)
		}
	}
	plain := "[Vertra] Deploy 0a1b2c3 was discarded; the previous version is still online."
	for _, in := range []string{
		"deploy_note:discarded:" + deployID,
		"deploy_note:discarded:" + deployID + ":something_new",
	} {
		if got := systemLine(in); got != plain {
			t.Fatalf("%q: got %q", in, got)
		}
	}
	if got := systemLine("deploy_note:failed:" + deployID); got != "[Vertra] Deploy 0a1b2c3 crashed on start." {
		t.Fatal(got)
	}
	if got := systemLine("deploy_note:rollback:" + deployID + ":" + previousID); got != "[Vertra] Deploy 0a1b2c3 crashed on start. We went back to 4e5f6a7." {
		t.Fatal(got)
	}
}

func TestSystemLineOtherNotes(t *testing.T) {
	withLocale(t, "en")
	cases := map[string]string{
		"app_restarted": "Application restarted",
		"runtime_note:line_changed:node:20.1:22.0:retired": "Runtime changed from 20.1 to 22.0",
		"run_note:port_not_listening:3000":                 "The app started but did not listen on port 3000. Make sure it listens on that port on all interfaces.",
		"image_note:pulling:40":                            "Pulling the image (40%)",
		"image_note:pulling:900":                           "Pulling the image (100%)",
		"image_note:creating":                              "Creating the container",
		"image_note:starting":                              "Starting the container",
		"image_note:waiting_port":                          "Waiting for the app to open its port",
		"restart_note:auto_restart_disabled":               "Auto-restart is turned off for this app.",
		"restart_note:crash_loop":                          "Crash loop detected — auto-restart temporarily suspended.",
		"restart_note:oom_killed":                          "App stopped — plan memory exhausted.",
		"shield_note:rate_limit:in":                        "Vertra Shield: app stopped for exceeding inbound request limits.",
		"shield_note:burst:out":                            "Vertra Shield: app stopped for an outbound traffic burst.",
	}
	for in, want := range cases {
		if got := systemLine(in); got != "[Vertra] "+want {
			t.Fatalf("%q: got %q", in, got)
		}
	}
}

func TestSystemLineLeavesUnknownAndMalformedRaw(t *testing.T) {
	withLocale(t, "en")
	for _, in := range []string{
		"install_note:brand_new",
		"2026-11-24T10:00:00Z run_note:started:2026-11-24T10:00:00Z",
		"deploy_note:discarded:not-a-uuid",
		"deploy_note:discarded:" + deployID + ":Bad-Reason",
		"deploy_note:discarded:" + deployID + ":a:b",
		"runtime_note:line_changed:node:20:22:other",
		"run_note:port_not_listening:abc",
		"free text from the app",
		"",
	} {
		if got := systemLine(in); got != in {
			t.Fatalf("%q: got %q", in, got)
		}
	}
}

func TestSystemLineSanitizesUntranslatedText(t *testing.T) {
	withLocale(t, "en")
	if got := systemLine("note\x1b]52;c;Zm9v\x07 done"); got != "note done" {
		t.Fatalf("got %q", got)
	}
	if got := systemLine("install_note:oom\x1b[2J"); got != "[Vertra] Dependency install stopped — plan memory exhausted." {
		t.Fatalf("got %q", got)
	}
}

func TestSystemLineInEachLanguage(t *testing.T) {
	notes := map[string]map[string]string{
		"pt": {
			"2026-11-24T10:00:00Z install_note:build_oom":           "O comando de build ficou sem memória e foi encerrado. Aumente a memória do app ou reduza o que o build processa.",
			"deploy_note:discarded:" + deployID + ":crashed":        "O deploy 0a1b2c3 foi descartado: ela parou antes de ficar pronta. A versão anterior continua no ar.",
			"deploy_note:discarded:" + deployID + ":unknown_reason": "O deploy 0a1b2c3 foi descartado; a versão anterior continua no ar.",
			"image_note:pulling:5":                                  "Baixando a imagem (5%)",
			"shield_note:burst:in":                                  "Vertra Shield: aplicação parada por um pico de tráfego de entrada.",
		},
		"es": {
			"2026-11-24T10:00:00Z install_note:build_oom":           "El comando de build se quedó sin memoria y se detuvo. Aumenta la memoria de la app o reduce lo que procesa el build.",
			"deploy_note:discarded:" + deployID + ":crashed":        "El despliegue 0a1b2c3 fue descartado: se detuvo antes de estar lista. La versión anterior sigue en línea.",
			"deploy_note:discarded:" + deployID + ":unknown_reason": "El despliegue 0a1b2c3 fue descartado; la versión anterior sigue en línea.",
			"image_note:pulling:5":                                  "Descargando la imagen (5%)",
			"shield_note:burst:in":                                  "Vertra Shield: aplicación detenida por un pico de tráfico de entrante.",
		},
	}
	for locale, cases := range notes {
		withLocale(t, locale)
		for in, want := range cases {
			if got := systemLine(in); got != "[Vertra] "+want {
				t.Fatalf("%s %q: got %q", locale, in, got)
			}
		}
	}
}

func TestNoteTablesCoverTheSameKeys(t *testing.T) {
	for locale, table := range noteTexts {
		for key := range noteTexts["en"] {
			if strings.TrimSpace(table[key]) == "" {
				t.Fatalf("%s is missing %s", locale, key)
			}
		}
		if len(table) != len(noteTexts["en"]) {
			t.Fatalf("%s has extra keys", locale)
		}
	}
}
