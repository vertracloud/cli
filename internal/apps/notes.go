package apps

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/vertracloud/cli/internal/ui"
)

var (
	noteStamp        = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z (.+)$`)
	strictUUID       = regexp.MustCompile(`^(?i:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)
	discardReason    = regexp.MustCompile(`^[a-z][a-z_]{0,39}$`)
	runtimeChanged   = regexp.MustCompile(`^runtime_note:line_changed:[a-z0-9_-]+:([0-9A-Za-z][0-9A-Za-z._+-]*):([0-9A-Za-z][0-9A-Za-z._+-]*):(?:retired|unavailable|requested)$`)
	portNotListening = regexp.MustCompile(`^run_note:port_not_listening:(\d{1,5})$`)
	imagePulling     = regexp.MustCompile(`^image_note:pulling:(\d{1,9})$`)
	installNote      = regexp.MustCompile(`^install_note:(oom|failed|build_failed|build_oom|build_timeout|timeout|shield)(?::(\d+))?$`)
	restartNote      = regexp.MustCompile(`^restart_note:(\w+)$`)
	shieldNote       = regexp.MustCompile(`^shield_note:(\w+):(in|out)$`)
)

const (
	noteLabelPrefix     = "[Vertra]"
	stepTimeoutSeconds  = "900"
	totalTimeoutSeconds = "1500"
)

func systemLine(data string) string {
	message := ui.Plain(data)
	if m := noteStamp.FindStringSubmatch(message); m != nil {
		message = m[1]
	}
	if text, ok := translateNote(ui.Locale, strings.TrimSpace(message)); ok {
		return ui.Dim(noteLabelPrefix) + " " + text
	}
	return ui.LogText(data)
}

func translateNote(locale, message string) (string, bool) {
	key, args, ok := parseNote(message)
	if !ok {
		return "", false
	}
	return noteText(locale, key, args...), true
}

func parseNote(message string) (string, []any, bool) {
	switch message {
	case "app_restarted":
		return "app_restarted", nil, true
	case "install_note:started":
		return "install_started", nil, true
	case "install_note:done":
		return "install_done", nil, true
	case "image_note:creating":
		return "image_creating", nil, true
	case "image_note:starting":
		return "image_starting", nil, true
	case "image_note:waiting_port":
		return "image_waiting_port", nil, true
	}
	if strings.HasPrefix(message, "deploy_note:") {
		return parseDeployNote(message)
	}
	if m := runtimeChanged.FindStringSubmatch(message); m != nil {
		return "runtime_changed", []any{m[1], m[2]}, true
	}
	if m := portNotListening.FindStringSubmatch(message); m != nil {
		return "port_not_listening", []any{m[1]}, true
	}
	if m := imagePulling.FindStringSubmatch(message); m != nil {
		percent, err := strconv.Atoi(m[1])
		if err != nil {
			return "", nil, false
		}
		return "image_pulling", []any{min(percent, 100)}, true
	}
	if m := installNote.FindStringSubmatch(message); m != nil {
		return parseInstallNote(m[1], m[2])
	}
	if m := restartNote.FindStringSubmatch(message); m != nil {
		switch m[1] {
		case "auto_restart_disabled":
			return "auto_restart_disabled", nil, true
		case "crash_loop":
			return "crash_loop", nil, true
		case "oom_killed":
			return "oom_killed", nil, true
		}
		return "restarted_automatically", nil, true
	}
	if m := shieldNote.FindStringSubmatch(message); m != nil {
		direction := "direction_" + m[2]
		if m[1] == "rate_limit" {
			return "shield_rate_limit", []any{direction}, true
		}
		return "shield_burst", []any{direction}, true
	}
	return "", nil, false
}

func parseInstallNote(kind, number string) (string, []any, bool) {
	switch kind {
	case "oom":
		return "install_oom", nil, true
	case "failed":
		if number == "" {
			number = "?"
		}
		return "install_failed", []any{number}, true
	case "build_failed":
		return "install_build_failed", nil, true
	case "build_oom":
		return "install_build_oom", nil, true
	case "build_timeout":
		return "install_build_timeout", nil, true
	case "timeout":
		switch number {
		case stepTimeoutSeconds:
			return "install_step_timeout", nil, true
		case totalTimeoutSeconds:
			return "install_total_timeout", nil, true
		}
		return "install_timeout", nil, true
	}
	return "install_shield", nil, true
}

func parseDeployNote(message string) (string, []any, bool) {
	parts := strings.Split(message, ":")
	short := func(id string) string { return strings.ToLower(id[:7]) }
	switch {
	case len(parts) == 3 && parts[1] == "failed" && strictUUID.MatchString(parts[2]):
		return "deploy_failed", []any{short(parts[2])}, true
	case len(parts) == 4 && parts[1] == "rollback" && strictUUID.MatchString(parts[2]) && strictUUID.MatchString(parts[3]):
		return "deploy_rollback", []any{short(parts[2]), short(parts[3])}, true
	case (len(parts) == 3 || (len(parts) == 4 && discardReason.MatchString(parts[3]))) &&
		parts[1] == "discarded" && strictUUID.MatchString(parts[2]):
		if len(parts) == 4 {
			if cause := "cause_" + parts[3]; knownNote(cause) {
				return "deploy_discarded_cause", []any{short(parts[2]), cause}, true
			}
		}
		return "deploy_discarded", []any{short(parts[2])}, true
	}
	return "", nil, false
}

func knownNote(key string) bool {
	_, ok := noteTexts["en"][key]
	return ok
}

func noteText(locale, key string, args ...any) string {
	table, ok := noteTexts[locale]
	if !ok {
		table = noteTexts["en"]
	}
	resolved := make([]any, len(args))
	for i, arg := range args {
		resolved[i] = arg
		if s, ok := arg.(string); ok {
			if text, found := table[s]; found && (strings.HasPrefix(s, "cause_") || strings.HasPrefix(s, "direction_")) {
				resolved[i] = text
			}
		}
	}
	format, ok := table[key]
	if !ok {
		format = noteTexts["en"][key]
	}
	return fmt.Sprintf(format, resolved...)
}

var noteTexts = map[string]map[string]string{
	"en": {
		"app_restarted":               "Application restarted",
		"install_started":             "Dependency install started",
		"install_done":                "Installation finished",
		"install_oom":                 "Dependency install stopped — plan memory exhausted.",
		"install_failed":              "Dependency install failed (exit code %s).",
		"install_build_failed":        "The build command failed; the app was not started.",
		"install_build_oom":           "The build command ran out of memory and was stopped. Raise the app memory or reduce what the build processes.",
		"install_build_timeout":       "The build exceeded the 10-minute time limit.",
		"install_step_timeout":        "A dependency install step exceeded the 15-minute limit.",
		"install_total_timeout":       "The installation exceeded the 25-minute total time limit.",
		"install_timeout":             "The installation exceeded the time limit.",
		"install_shield":              "Build stopped by network protection — traffic above the allowance during the deploy.",
		"runtime_changed":             "Runtime changed from %s to %s",
		"port_not_listening":          "The app started but did not listen on port %s. Make sure it listens on that port on all interfaces.",
		"image_pulling":               "Pulling the image (%d%%)",
		"image_creating":              "Creating the container",
		"image_starting":              "Starting the container",
		"image_waiting_port":          "Waiting for the app to open its port",
		"auto_restart_disabled":       "Auto-restart is turned off for this app.",
		"crash_loop":                  "Crash loop detected — auto-restart temporarily suspended.",
		"oom_killed":                  "App stopped — plan memory exhausted.",
		"restarted_automatically":     "Application restarted automatically.",
		"shield_rate_limit":           "Vertra Shield: app stopped for exceeding %s request limits.",
		"shield_burst":                "Vertra Shield: app stopped for an %s traffic burst.",
		"direction_in":                "inbound",
		"direction_out":               "outbound",
		"deploy_failed":               "Deploy %s crashed on start.",
		"deploy_rollback":             "Deploy %s crashed on start. We went back to %s.",
		"deploy_discarded":            "Deploy %s was discarded; the previous version is still online.",
		"deploy_discarded_cause":      "Deploy %s was discarded: %s. The previous version is still online.",
		"cause_install_oom":           "it ran out of memory while installing dependencies",
		"cause_install_build_oom":     "it ran out of memory during the build",
		"cause_build_oom":             "it ran out of memory during the build",
		"cause_install_failed":        "the dependency install failed",
		"cause_install_build_failed":  "the build command failed",
		"cause_install_build_timeout": "the build went over the time limit",
		"cause_install_timeout":       "the install went over the time limit",
		"cause_install_shield":        "network protection stopped the install",
		"cause_crashed":               "it stopped before it was ready",
		"cause_unhealthy":             "it did not become ready to receive traffic",
		"cause_timeout":               "it did not respond in time",
	},
	"pt": {
		"app_restarted":               "Aplicação reiniciada",
		"install_started":             "Instalação de dependências iniciada",
		"install_done":                "Instalação concluída",
		"install_oom":                 "Instalação de dependências interrompida — memória do plano esgotada.",
		"install_failed":              "Falha na instalação de dependências (código de saída %s).",
		"install_build_failed":        "O comando de build falhou; a aplicação não foi iniciada.",
		"install_build_oom":           "O comando de build ficou sem memória e foi encerrado. Aumente a memória do app ou reduza o que o build processa.",
		"install_build_timeout":       "O build ultrapassou o limite de 10 minutos.",
		"install_step_timeout":        "Uma etapa da instalação de dependências passou do limite de 15 minutos.",
		"install_total_timeout":       "A instalação passou do prazo total de 25 minutos.",
		"install_timeout":             "A instalação ultrapassou o limite de tempo.",
		"install_shield":              "Build interrompido pela proteção de rede — tráfego acima do permitido durante o deploy.",
		"runtime_changed":             "Runtime alterado de %s para %s",
		"port_not_listening":          "O app iniciou, mas não abriu a porta %s. Confirme que ele escuta nessa porta em todas as interfaces.",
		"image_pulling":               "Baixando a imagem (%d%%)",
		"image_creating":              "Criando o contêiner",
		"image_starting":              "Iniciando o contêiner",
		"image_waiting_port":          "Aguardando o app abrir a porta",
		"auto_restart_disabled":       "O reinício automático está desativado para este app.",
		"crash_loop":                  "Loop de falhas detectado — reinício automático suspenso temporariamente.",
		"oom_killed":                  "Aplicação parada — memória do plano esgotada.",
		"restarted_automatically":     "Aplicação reiniciada automaticamente.",
		"shield_rate_limit":           "Vertra Shield: aplicação parada por exceder limites de requisição de %s.",
		"shield_burst":                "Vertra Shield: aplicação parada por um pico de tráfego de %s.",
		"direction_in":                "entrada",
		"direction_out":               "saída",
		"deploy_failed":               "O deploy %s caiu ao iniciar.",
		"deploy_rollback":             "O deploy %s caiu ao iniciar. Voltamos para %s.",
		"deploy_discarded":            "O deploy %s foi descartado; a versão anterior continua no ar.",
		"deploy_discarded_cause":      "O deploy %s foi descartado: %s. A versão anterior continua no ar.",
		"cause_install_oom":           "faltou memória na instalação das dependências",
		"cause_install_build_oom":     "faltou memória no build",
		"cause_build_oom":             "faltou memória no build",
		"cause_install_failed":        "a instalação das dependências falhou",
		"cause_install_build_failed":  "o comando de build falhou",
		"cause_install_build_timeout": "o build passou do tempo limite",
		"cause_install_timeout":       "a instalação passou do tempo limite",
		"cause_install_shield":        "a proteção de rede interrompeu a instalação",
		"cause_crashed":               "ela parou antes de ficar pronta",
		"cause_unhealthy":             "ela não ficou pronta para receber acessos",
		"cause_timeout":               "ela não respondeu dentro do tempo",
	},
	"es": {
		"app_restarted":               "Aplicación reiniciada",
		"install_started":             "Instalación de dependencias iniciada",
		"install_done":                "Instalación finalizada",
		"install_oom":                 "Instalación de dependencias detenida — memoria del plan agotada.",
		"install_failed":              "Fallo en la instalación de dependencias (código de salida %s).",
		"install_build_failed":        "El comando de build falló; la aplicación no se inició.",
		"install_build_oom":           "El comando de build se quedó sin memoria y se detuvo. Aumenta la memoria de la app o reduce lo que procesa el build.",
		"install_build_timeout":       "El build superó el límite de 10 minutos.",
		"install_step_timeout":        "Un paso de la instalación de dependencias superó el límite de 15 minutos.",
		"install_total_timeout":       "La instalación superó el plazo total de 25 minutos.",
		"install_timeout":             "La instalación superó el límite de tiempo.",
		"install_shield":              "Build detenido por la protección de red — tráfico por encima del permitido durante el despliegue.",
		"runtime_changed":             "Runtime cambiado de %s a %s",
		"port_not_listening":          "La app inició, pero no abrió el puerto %s. Comprueba que escucha en ese puerto en todas las interfaces.",
		"image_pulling":               "Descargando la imagen (%d%%)",
		"image_creating":              "Creando el contenedor",
		"image_starting":              "Iniciando el contenedor",
		"image_waiting_port":          "Esperando a que la app abra el puerto",
		"auto_restart_disabled":       "El reinicio automático está desactivado para esta app.",
		"crash_loop":                  "Bucle de fallos detectado — reinicio automático suspendido temporalmente.",
		"oom_killed":                  "Aplicación detenida — memoria del plan agotada.",
		"restarted_automatically":     "Aplicación reiniciada automáticamente.",
		"shield_rate_limit":           "Vertra Shield: aplicación detenida por exceder los límites de solicitudes de %s.",
		"shield_burst":                "Vertra Shield: aplicación detenida por un pico de tráfico de %s.",
		"direction_in":                "entrante",
		"direction_out":               "saliente",
		"deploy_failed":               "El despliegue %s se cayó al iniciar.",
		"deploy_rollback":             "El despliegue %s se cayó al iniciar. Volvimos a %s.",
		"deploy_discarded":            "El despliegue %s fue descartado; la versión anterior sigue en línea.",
		"deploy_discarded_cause":      "El despliegue %s fue descartado: %s. La versión anterior sigue en línea.",
		"cause_install_oom":           "se quedó sin memoria al instalar las dependencias",
		"cause_install_build_oom":     "se quedó sin memoria durante el build",
		"cause_build_oom":             "se quedó sin memoria durante el build",
		"cause_install_failed":        "la instalación de las dependencias falló",
		"cause_install_build_failed":  "el comando de build falló",
		"cause_install_build_timeout": "el build superó el tiempo límite",
		"cause_install_timeout":       "la instalación superó el tiempo límite",
		"cause_install_shield":        "la protección de red interrumpió la instalación",
		"cause_crashed":               "se detuvo antes de estar lista",
		"cause_unhealthy":             "no quedó lista para recibir tráfico",
		"cause_timeout":               "no respondió a tiempo",
	},
}
