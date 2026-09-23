package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vertracloud/cli/internal/apps"
	"github.com/vertracloud/cli/internal/config"
	"github.com/vertracloud/cli/internal/databases"
	"github.com/vertracloud/cli/internal/local"
	"github.com/vertracloud/cli/internal/resources"
	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

var version = "0.1.0-dev"
var apiBaseURL = "https://api.vertracloud.app"

func currentAPIBaseURL() string {
	if value := os.Getenv("VERTRA_API_URL"); value != "" {
		return value
	}
	return apiBaseURL
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, out, errOut io.Writer) int {
	jsonMode := false
	explicitLocale := ""
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--json":
			jsonMode = true
		case args[i] == "--lang" && i+1 < len(args):
			i++
			explicitLocale = args[i]
		case strings.HasPrefix(args[i], "--lang="):
			explicitLocale = strings.TrimPrefix(args[i], "--lang=")
		default:
			filtered = append(filtered, args[i])
		}
	}
	args = filtered
	helpConfig, _ := config.Load()
	locale := config.Locale(helpConfig, explicitLocale)
	ui.Locale = locale
	if wantsHelp(args) {
		printHelp(out, args, locale)
		return 0
	}
	if len(args) == 0 {
		printHelp(out, args, locale)
		return 0
	}
	if len(args) == 1 && (args[0] == "-v" || args[0] == "--version") {
		fmt.Fprintln(out, version)
		return 0
	}
	cfg, err := config.Load()
	if err != nil {
		return fail(errOut, jsonMode, "CONFIG_ERROR", err)
	}
	locale = config.Locale(cfg, explicitLocale)
	ui.Locale = locale
	if args[0] == "auth" && len(args) > 1 && args[1] == "logout" {
		cfg.APIKey = ""
		if err := config.Save(cfg); err != nil {
			return fail(errOut, jsonMode, "CONFIG_ERROR", err)
		}
		return emit(out, map[string]bool{"logged_out": true}, jsonMode, locale, args)
	}
	if args[0] == "unlink" {
		cwd, err := os.Getwd()
		if err != nil {
			return fail(errOut, jsonMode, "LOCAL_ERROR", err)
		}
		if err := local.SetProjectID(cwd, ""); err != nil {
			return fail(errOut, jsonMode, "LOCAL_ERROR", err)
		}
		return emit(out, json.RawMessage("null"), jsonMode, locale, args)
	}
	if args[0] == "link" && len(args) > 1 && args[1] == "--show" {
		cwd, err := os.Getwd()
		if err != nil {
			return fail(errOut, jsonMode, "LOCAL_ERROR", err)
		}
		data, err := local.ReadProject(cwd)
		if err != nil {
			return fail(errOut, jsonMode, "LOCAL_ERROR", err)
		}
		if data == nil {
			return emit(out, json.RawMessage("null"), jsonMode, locale, args)
		}
		return emit(out, data, jsonMode, locale, args)
	}
	if args[0] == "zip" {
		cwd, err := os.Getwd()
		if err != nil {
			return fail(errOut, jsonMode, "LOCAL_ERROR", err)
		}
		name := filepath.Base(cwd) + ".zip"
		if err := local.Zip(cwd, filepath.Join(cwd, name)); err != nil {
			return fail(errOut, jsonMode, "LOCAL_ERROR", err)
		}
		return emit(out, map[string]string{"file": name}, jsonMode, locale, args)
	}
	if args[0] == "doctor" {
		cwd, err := os.Getwd()
		if err != nil {
			return fail(errOut, jsonMode, "LOCAL_ERROR", err)
		}
		if len(args) == 3 && args[1] == "--path" {
			cwd = args[2]
		} else if len(args) != 1 {
			return fail(errOut, jsonMode, "INVALID_ARGUMENT", errors.New("use doctor [--path <dir>]"))
		}
		result, err := local.Doctor(cwd)
		if err != nil {
			return fail(errOut, jsonMode, "LOCAL_ERROR", err)
		}
		project, err := local.ReadProject(cwd)
		if err != nil {
			return fail(errOut, jsonMode, "LOCAL_ERROR", err)
		}
		if runtime := project["VERSION"]; runtime != "" && runtime != "recommended" && runtime != "latest" && runtime != "auto" && config.APIKey(cfg) != "" {
			if rc, err := rest.NewClient(config.APIKey(cfg), rest.WithBaseURL(currentAPIBaseURL()), rest.WithTimeout(1500*time.Millisecond)); err == nil {
				runtimes, err := vertracloud.New(rc).Apps.Runtimes(ctx)
				if err == nil && !knownRuntime(runtimes, runtime) {
					result.Checks = append(result.Checks, local.Check{ID: "config_version", Level: "warn", Message: "VERSION not found in available runtimes: " + runtime})
					result.Summary.Warnings++
				}
			}
		}
		code := emit(out, result, jsonMode, locale, args)
		if result.Summary.Errors > 0 {
			return 1
		}
		return code
	}
	if args[0] == "status" {
		if len(args) != 1 {
			return fail(errOut, jsonMode, "INVALID_ARGUMENT", errors.New("status takes no arguments"))
		}
		data, err := vertracloud.New(rest.NewPublicClient(rest.WithBaseURL(currentAPIBaseURL()), rest.WithUserAgent("vertra-cli/"+version))).Status.Get(ctx)
		if err != nil {
			return fail(errOut, jsonMode, "COMMAND_FAILED", err)
		}
		return emit(out, data, jsonMode, locale, args)
	}
	if args[0] == "lang" || args[0] == "language" || args[0] == "locale" {
		if len(args) == 1 {
			return emit(out, map[string]any{"current": config.Locale(cfg, explicitLocale), "available": []string{"pt", "en", "es"}}, jsonMode, locale, args)
		}
		if args[1] != "pt" && args[1] != "en" && args[1] != "es" {
			return fail(errOut, jsonMode, "INVALID_ARGUMENT", errors.New("locale must be pt, en or es"))
		}
		cfg.Locale = args[1]
		if err := config.Save(cfg); err != nil {
			return fail(errOut, jsonMode, "CONFIG_ERROR", err)
		}
		return emit(out, map[string]string{"locale": cfg.Locale}, jsonMode, locale, args)
	}
	if args[0] == "auth" && len(args) > 1 && args[1] == "login" {
		key := ""
		for i := 2; i < len(args); i++ {
			if args[i] == "--token" && i+1 < len(args) {
				key = args[i+1]
				i++
			} else if strings.HasPrefix(args[i], "--token=") {
				key = strings.TrimPrefix(args[i], "--token=")
			}
		}
		if key == "" {
			if jsonMode {
				return fail(errOut, true, "INVALID_ARGUMENT", errors.New("use auth login --token <key>"))
			}
			var promptErr error
			key, promptErr = ui.ReadMasked(loginPrompt(locale))
			if promptErr != nil {
				return fail(errOut, false, "INVALID_ARGUMENT", promptErr)
			}
			if key == "" {
				return fail(errOut, false, "INVALID_ARGUMENT", errors.New(loginRequired(locale)))
			}
		}
		rc, err := rest.NewClient(key, rest.WithBaseURL(currentAPIBaseURL()))
		if err != nil {
			return fail(errOut, jsonMode, "INVALID_ARGUMENT", err)
		}
		user, err := vertracloud.New(rc).Account.Get(ctx)
		if err != nil {
			return fail(errOut, jsonMode, "LOGIN_FAILED", err)
		}
		cfg.APIKey = key
		if err := config.Save(cfg); err != nil {
			return fail(errOut, jsonMode, "CONFIG_ERROR", err)
		}
		return emit(out, user, jsonMode, locale, args)
	}
	key := config.APIKey(cfg)
	if key == "" {
		return fail(errOut, jsonMode, "NOT_AUTHENTICATED", errors.New("set VERTRA_API_KEY or run auth login --token <key>"))
	}
	if destructive, label := destructivePath(args, locale); destructive && !hasYes(args) {
		if jsonMode || !ui.IsInteractive() {
			return fail(errOut, jsonMode, "CONFIRMATION_REQUIRED", errors.New(label))
		}
		confirmed, confirmErr := ui.Confirm(label)
		if confirmErr != nil {
			return fail(errOut, false, "CONFIRMATION_REQUIRED", confirmErr)
		}
		if !confirmed {
			fmt.Fprintln(out, cancelled(locale))
			return 0
		}
		args = append(args, "--yes")
	}
	rc, err := rest.NewClient(key, rest.WithBaseURL(currentAPIBaseURL()), rest.WithUserAgent("vertra-cli/"+version))
	if err != nil {
		return fail(errOut, jsonMode, "INVALID_ARGUMENT", err)
	}
	client := vertracloud.New(rc)
	var data any
	switch args[0] {
	case "auth":
		if len(args) != 2 || args[1] != "whoami" {
			err = errors.New("unknown auth command")
		} else {
			data, err = client.Account.Get(ctx)
		}
	case "profile", "me":
		data, err = client.Account.Get(ctx)
	case "link":
		id := ""
		if len(args) == 2 {
			id = args[1]
		}
		if len(args) == 1 {
			if !ui.IsInteractive() {
				err = errors.New("link requires an app id or an interactive terminal")
				break
			}
			var account vertracloud.AccountInfo
			account, err = client.Account.Get(ctx)
			if err != nil {
				break
			}
			choices := make([]ui.Choice, 0, len(account.Applications))
			for _, app := range account.Applications {
				choices = append(choices, ui.Choice{ID: app.ID, Label: app.Name, Hint: app.ID})
			}
			id, err = ui.PickID(linkPickerLabel(locale), choices)
			if err != nil {
				break
			}
		} else if len(args) != 2 {
			err = errors.New("use link [<app-id>] or link --show")
			break
		}
		var app vertracloud.Application
		app, err = client.Apps.Get(ctx, id)
		if err == nil {
			var cwd string
			cwd, err = os.Getwd()
			if err == nil {
				err = local.SetProjectID(cwd, app.ID)
				data = map[string]string{"id": app.ID, "name": app.Name, "path": filepath.Join(cwd, local.ProjectFile)}
			}
		}
	case "app", "apps":
		if len(args) < 2 {
			err = errors.New("missing app command")
		} else {
			if args[1] == "logs" && follows(args[2:]) {
				if jsonMode {
					data, err = apps.RunWithOptions(ctx, client, "logs", args[2:], true)
					break
				}
				if err := apps.Follow(ctx, client, args[2:], out); err != nil {
					return fail(errOut, jsonMode, "COMMAND_FAILED", err)
				}
				return 0
			}
			data, err = apps.RunWithOptions(ctx, client, args[1], args[2:], jsonMode)
		}
	case "logs":
		if follows(args[1:]) {
			if jsonMode {
				data, err = apps.RunWithOptions(ctx, client, "logs", args[1:], true)
				break
			}
			if err := apps.Follow(ctx, client, args[1:], out); err != nil {
				return fail(errOut, jsonMode, "COMMAND_FAILED", err)
			}
			return 0
		}
		data, err = apps.RunWithOptions(ctx, client, "logs", args[1:], jsonMode)
	case "upload":
		data, err = apps.RunWithOptions(ctx, client, args[0], args[1:], jsonMode)
	case "commit", "push", "deploy":
		data, err = apps.RunWithOptions(ctx, client, canonicalAppCommand(args[0]), args[1:], jsonMode)
	case "db", "database":
		if len(args) < 2 {
			err = errors.New("missing db command")
		} else {
			data, err = databases.Run(ctx, client, args[1], args[2:])
		}
	case "projects", "project", "proj":
		data, err = projects(ctx, client, args[1:])
	case "workspace", "ws", "snapshot", "snapshots", "folder", "favorite", "fav":
		data, err = resources.Run(ctx, client, args[0], args[1:])
	default:
		err = fmt.Errorf("unknown command %q", args[0])
	}
	if err != nil {
		return fail(errOut, jsonMode, "COMMAND_FAILED", err)
	}
	return emit(out, data, jsonMode, locale, args)
}

// projects lists applications and databases together, reusing both list
// commands (each already merges the live /status call) in parallel.
func projects(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	if len(args) > 0 && (args[0] == "list" || args[0] == "ls") {
		args = args[1:]
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "--filter") {
			return nil, errors.New("projects does not support --filter; use app list or db list")
		}
	}
	type result struct {
		data any
		err  error
	}
	dbCh := make(chan result, 1)
	go func() {
		data, err := databases.Run(ctx, client, "list", args)
		dbCh <- result{data, err}
	}()
	appData, appErr := apps.RunWithOptions(ctx, client, "list", args, false)
	db := <-dbCh
	if appErr != nil {
		return nil, appErr
	}
	if db.err != nil {
		return nil, db.err
	}
	appList, dbList := appData.(ui.Presentation), db.data.(ui.Presentation)
	var rows []map[string]any
	for _, row := range appList.Human.([]map[string]any) {
		row["kind"], row["runtime"] = "application", row["language"]
		rows = append(rows, row)
	}
	for _, row := range dbList.Human.([]map[string]any) {
		row["kind"] = "database"
		rows = append(rows, row)
	}
	return ui.Presentation{JSON: map[string]any{"applications": appList.JSON, "databases": dbList.JSON}, Human: rows}, nil
}

func knownRuntime(runtimes vertracloud.ApplicationRuntimes, version string) bool {
	for _, runtime := range runtimes {
		if runtime.Recommended == version || runtime.Latest == version {
			return true
		}
		for _, specific := range runtime.Specific {
			if specific == version {
				return true
			}
		}
	}
	return false
}

func follows(args []string) bool {
	for _, arg := range args {
		if arg == "-f" || arg == "--follow" || arg == "--realtime" {
			return true
		}
	}
	return false
}

func canonicalAppCommand(command string) string {
	if command == "push" || command == "deploy" {
		return "commit"
	}
	return command
}

func write(out io.Writer, data any) int {
	if data == nil {
		data = map[string]bool{"ok": true}
	}
	if presentation, ok := data.(ui.Presentation); ok {
		data = presentation.JSON
	}
	if err := json.NewEncoder(out).Encode(data); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func emit(out io.Writer, data any, jsonMode bool, locale string, args []string) int {
	if jsonMode {
		return write(out, data)
	}
	command := strings.Join(args, " ")
	pageSize, all := listOutputOptions(args)
	ui.RenderPaged(out, data, command, locale, pageSize, all)
	return 0
}

func listOutputOptions(args []string) (int, bool) {
	if len(args) < 2 || (args[0] != "app" && args[0] != "apps" && args[0] != "db" && args[0] != "database") || args[1] != "list" && args[1] != "ls" {
		return 0, true
	}
	pageSize, all := 0, false
	for i := 2; i < len(args); i++ {
		if args[i] == "--all" || args[i] == "-A" {
			all = true
		}
		if args[i] == "--page-size" && i+1 < len(args) {
			if n, err := strconv.Atoi(args[i+1]); err == nil {
				pageSize = n
			}
			i++
		} else if strings.HasPrefix(args[i], "--page-size=") {
			if n, err := strconv.Atoi(strings.TrimPrefix(args[i], "--page-size=")); err == nil {
				pageSize = n
			}
		}
	}
	return pageSize, all
}

func wantsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" || arg == "help" {
			return true
		}
	}
	return false
}

type helpEntry struct {
	use  string
	desc [3]string // pt, en, es
}

type helpSection struct {
	title   [3]string
	entries []helpEntry
}

var topHelp = []helpSection{
	{[3]string{"Primeiros passos", "Getting started", "Primeros pasos"}, []helpEntry{
		{"auth login", [3]string{"Entra com sua chave de API", "Sign in with your API key", "Inicia sesión con tu clave de API"}},
		{"link [<app-id>]", [3]string{"Vincula esta pasta a uma aplicação", "Link this directory to an application", "Vincula esta carpeta a una aplicación"}},
		{"deploy [--restart]", [3]string{"Envia os arquivos da pasta para a aplicação", "Upload this directory to the application", "Sube esta carpeta a la aplicación"}},
		{"logs [--follow]", [3]string{"Mostra os logs da aplicação", "Show application logs", "Muestra los logs de la aplicación"}},
	}},
	{[3]string{"Recursos", "Resources", "Recursos"}, []helpEntry{
		{"projects", [3]string{"Lista apps e bancos juntos, com estado", "List apps and databases together, with status", "Lista apps y bases de datos juntas, con estado"}},
		{"app", [3]string{"Aplicações: deploy, estado, logs, variáveis, arquivos e rede", "Applications: deploy, status, logs, env, files and network", "Aplicaciones: deploy, estado, logs, variables, archivos y red"}},
		{"db", [3]string{"Bancos de dados: criar, estado, credenciais", "Databases: create, status, credentials", "Bases de datos: crear, estado, credenciales"}},
		{"workspace", [3]string{"Workspaces, membros, convites e funções", "Workspaces, members, invites and roles", "Espacios de trabajo, miembros, invitaciones y roles"}},
		{"snapshot", [3]string{"Cria, baixa e restaura snapshots", "Create, download and restore snapshots", "Crea, descarga y restaura snapshots"}},
		{"folder", [3]string{"Organiza recursos em pastas", "Organize resources in folders", "Organiza recursos en carpetas"}},
		{"favorite", [3]string{"Marca recursos como favoritos", "Mark resources as favorites", "Marca recursos como favoritos"}},
	}},
	{[3]string{"Conta e utilitários", "Account and tools", "Cuenta y utilidades"}, []helpEntry{
		{"auth whoami", [3]string{"Mostra a conta conectada", "Show the signed-in account", "Muestra la cuenta conectada"}},
		{"auth logout", [3]string{"Remove a chave salva", "Remove the saved key", "Elimina la clave guardada"}},
		{"doctor", [3]string{"Verifica o projeto antes do deploy", "Check the project before deploying", "Revisa el proyecto antes del deploy"}},
		{"zip", [3]string{"Compacta a pasta atual", "Zip the current directory", "Comprime la carpeta actual"}},
		{"status", [3]string{"Estado da plataforma", "Platform status", "Estado de la plataforma"}},
		{"lang [pt|en|es]", [3]string{"Mostra ou troca o idioma", "Show or change the language", "Muestra o cambia el idioma"}},
	}},
}

var groupHelp = map[string][]helpEntry{
	"auth": {
		{"login [--token <key>]", [3]string{"Entra com sua chave de API", "Sign in with your API key", "Inicia sesión con tu clave de API"}},
		{"whoami", [3]string{"Mostra a conta conectada", "Show the signed-in account", "Muestra la cuenta conectada"}},
		{"logout", [3]string{"Remove a chave salva", "Remove the saved key", "Elimina la clave guardada"}},
	},
	"app": {
		{"list [--workspace <id>] [--filter k=v]", [3]string{"Lista suas aplicações", "List your applications", "Lista tus aplicaciones"}},
		{"info [<id>]", [3]string{"Detalhes da aplicação", "Application details", "Detalles de la aplicación"}},
		{"status [<id>|--all]", [3]string{"CPU, memória e tempo ativo", "CPU, memory and uptime", "CPU, memoria y tiempo activo"}},
		{"logs [<id>] [--follow]", [3]string{"Logs, ou acompanha em tempo real", "Logs, or follow them live", "Logs, o síguelos en vivo"}},
		{"metrics [<id>] [--range 10m|30m|24h]", [3]string{"Histórico de uso", "Usage history", "Historial de uso"}},
		{"start|stop [<id>]", [3]string{"Liga ou desliga a aplicação", "Start or stop the application", "Enciende o apaga la aplicación"}},
		{"restart [<id>] [--reinstall] [--force-build]", [3]string{"Reinicia a aplicação", "Restart the application", "Reinicia la aplicación"}},
		{"upload [--name --memory --main]", [3]string{"Cria uma aplicação a partir desta pasta", "Create an application from this directory", "Crea una aplicación desde esta carpeta"}},
		{"commit [<id>] [--restart]", [3]string{"Envia os arquivos desta pasta (alias: push, deploy)", "Upload this directory (aliases: push, deploy)", "Sube esta carpeta (alias: push, deploy)"}},
		{"config [<id>] [--ram --main --start ...]", [3]string{"Altera a configuração", "Change the configuration", "Cambia la configuración"}},
		{"download [<id>] [-o <file>]", [3]string{"Baixa o código como .zip", "Download the code as .zip", "Descarga el código como .zip"}},
		{"env list|set|remove", [3]string{"Variáveis de ambiente", "Environment variables", "Variables de entorno"}},
		{"file list|read|write|move|delete", [3]string{"Arquivos da aplicação", "Application files", "Archivos de la aplicación"}},
		{"network dns|domain|subdomain|publish|unpublish|purge-cache", [3]string{"Domínios e publicação", "Domains and publishing", "Dominios y publicación"}},
		{"delete [<id>] [--yes]", [3]string{"Exclui a aplicação", "Delete the application", "Elimina la aplicación"}},
	},
	"db": {
		{"list [--workspace <id>] [--filter k=v]", [3]string{"Lista seus bancos de dados", "List your databases", "Lista tus bases de datos"}},
		{"create --type <postgres|mongo|redis|mysql>", [3]string{"Cria um banco de dados", "Create a database", "Crea una base de datos"}},
		{"info [<id>]", [3]string{"Detalhes e conexão", "Details and connection", "Detalles y conexión"}},
		{"status [<id>|--all]", [3]string{"CPU, memória e armazenamento", "CPU, memory and storage", "CPU, memoria y almacenamiento"}},
		{"metrics [<id>]", [3]string{"Histórico de uso", "Usage history", "Historial de uso"}},
		{"start|stop [<id>]", [3]string{"Liga ou desliga o banco", "Start or stop the database", "Enciende o apaga la base de datos"}},
		{"update [<id>] [flags]", [3]string{"Altera nome, memória ou descrição", "Change name, memory or description", "Cambia nombre, memoria o descripción"}},
		{"credentials certificate|reset <target>", [3]string{"Certificado e redefinição de credenciais", "Certificate and credential reset", "Certificado y restablecimiento de credenciales"}},
		{"reset [<id>] [--yes]", [3]string{"Apaga todos os dados do banco", "Erase all database data", "Borra todos los datos"}},
		{"delete [<id>] [--yes]", [3]string{"Exclui o banco de dados", "Delete the database", "Elimina la base de datos"}},
	},
	"workspace": {
		{"list", [3]string{"Lista seus workspaces", "List your workspaces", "Lista tus espacios de trabajo"}},
		{"create <name>", [3]string{"Cria um workspace", "Create a workspace", "Crea un espacio de trabajo"}},
		{"info <id>", [3]string{"Detalhes, recursos e funções", "Details, resources and roles", "Detalles, recursos y roles"}},
		{"update <id> [flags]", [3]string{"Altera nome ou descrição", "Change name or description", "Cambia nombre o descripción"}},
		{"invite list|revoke <id> ...", [3]string{"Convites pendentes do workspace", "Pending workspace invites", "Invitaciones pendientes del espacio"}},
		{"invite preview|accept|decline <token>", [3]string{"Vê, aceita ou recusa um convite recebido", "View, accept or decline an invite you received", "Ve, acepta o rechaza una invitación recibida"}},
		{"action-request list|create <id> ...", [3]string{"Pedidos de ação para quem tem a permissão", "Action requests for members with the permission", "Solicitudes de acción para quien tiene el permiso"}},
		{"member <id> list|remove", [3]string{"Membros do workspace", "Workspace members", "Miembros del espacio"}},
		{"role <id> ...", [3]string{"Funções e permissões", "Roles and permissions", "Roles y permisos"}},
		{"delete <id> [--yes]", [3]string{"Exclui o workspace", "Delete the workspace", "Elimina el espacio de trabajo"}},
	},
	"snapshot": {
		{"list [<resource-id>] [--db]", [3]string{"Lista snapshots de uma aplicação ou banco", "List snapshots of an application or database", "Lista snapshots de una aplicación o base de datos"}},
		{"create [<resource-id>] [--db]", [3]string{"Cria um snapshot agora", "Create a snapshot now", "Crea un snapshot ahora"}},
		{"download <resource-id> <snapshot-id> [--db]", [3]string{"Baixa um snapshot", "Download a snapshot", "Descarga un snapshot"}},
		{"restore <resource-id> <snapshot-id> [--to <id>]", [3]string{"Restaura um snapshot", "Restore a snapshot", "Restaura un snapshot"}},
	},
	"folder": {
		{"list", [3]string{"Lista suas pastas", "List your folders", "Lista tus carpetas"}},
		{"create <name>", [3]string{"Cria uma pasta", "Create a folder", "Crea una carpeta"}},
		{"update <id> [flags]", [3]string{"Renomeia ou muda a cor", "Rename or recolor", "Renombra o cambia el color"}},
		{"delete <id> [--yes]", [3]string{"Exclui a pasta", "Delete the folder", "Elimina la carpeta"}},
	},
	"favorite": {
		{"list", [3]string{"Lista seus favoritos", "List your favorites", "Lista tus favoritos"}},
		{"add <app|db> <id>", [3]string{"Adiciona aos favoritos", "Add to favorites", "Añade a favoritos"}},
		{"remove <app|db> <id>", [3]string{"Remove dos favoritos", "Remove from favorites", "Quita de favoritos"}},
	},
}

var groupAliases = map[string]string{"apps": "app", "database": "db", "ws": "workspace", "snapshots": "snapshot", "fav": "favorite"}

func printHelp(out io.Writer, args []string, locale string) {
	var words []string
	for _, arg := range args {
		if arg != "--help" && arg != "-h" && arg != "help" && !strings.HasPrefix(arg, "-") {
			words = append(words, arg)
		}
	}
	i := map[string]int{"pt": 0, "en": 1, "es": 2}[locale]
	label := func(pt, en, es string) string { return [3]string{pt, en, es}[i] }
	heading := func(title string) { fmt.Fprintf(out, "\n%s\n", ui.Bold(strings.ToUpper(title))) }
	list := func(prefix string, entries []helpEntry) {
		width := 0
		for _, e := range entries {
			width = max(width, len(prefix+e.use))
		}
		for _, e := range entries {
			fmt.Fprintf(out, "  %s%s  %s\n", ui.Cyan(prefix+e.use), strings.Repeat(" ", width-len(prefix+e.use)), e.desc[i])
		}
	}
	if child := map[string]string{"deploy": "commit", "push": "commit", "commit": "commit", "upload": "upload", "logs": "logs"}[firstOf(words)]; child != "" {
		words = []string{"app", child}
	}
	group := ""
	if len(words) > 0 {
		group = words[0]
		if alias := groupAliases[group]; alias != "" {
			group = alias
		}
	}
	entries, isGroup := groupHelp[group]
	switch {
	case isGroup && len(words) > 1:
		for _, e := range entries {
			for _, name := range strings.Split(strings.Fields(e.use)[0], "|") {
				if name == words[1] {
					heading(label("Uso", "Usage", "Uso"))
					fmt.Fprintf(out, "  vertra %s %s\n\n  %s\n", group, e.use, e.desc[i])
					printGlobalFlags(out, locale, heading)
					return
				}
			}
		}
		fallthrough
	case isGroup:
		heading(label("Uso", "Usage", "Uso"))
		fmt.Fprintf(out, "  vertra %s <%s> [flags]\n", group, label("comando", "command", "comando"))
		heading(label("Comandos", "Commands", "Comandos"))
		list("", entries)
	default:
		fmt.Fprintf(out, "%s %s\n", ui.Bold(label("CLI da Vertra Cloud", "Vertra Cloud CLI", "CLI de Vertra Cloud")), ui.Dim(version))
		heading(label("Uso", "Usage", "Uso"))
		fmt.Fprintf(out, "  vertra <%s> [flags]\n", label("comando", "command", "comando"))
		for _, section := range topHelp {
			heading(section.title[i])
			list("", section.entries)
		}
	}
	printGlobalFlags(out, locale, heading)
	fmt.Fprintf(out, "\n%s\n", ui.Dim(label("Use \"vertra <comando> --help\" para ver os detalhes de um comando.", "Run \"vertra <command> --help\" for details on a command.", "Usa \"vertra <comando> --help\" para ver los detalles de un comando.")))
}

func firstOf(words []string) string {
	if len(words) == 0 {
		return ""
	}
	return words[0]
}

func printGlobalFlags(out io.Writer, locale string, heading func(string)) {
	i := map[string]int{"pt": 0, "en": 1, "es": 2}[locale]
	heading([3]string{"Flags globais", "Global flags", "Flags globales"}[i])
	for _, flag := range [][4]string{
		{"--json", "Saída em JSON para scripts", "JSON output for scripts", "Salida JSON para scripts"},
		{"--lang <pt|en|es>", "Idioma desta execução", "Language for this run", "Idioma de esta ejecución"},
		{"-y, --yes", "Confirma ações destrutivas", "Confirm destructive actions", "Confirma acciones destructivas"},
		{"-h, --help", "Mostra a ajuda", "Show help", "Muestra la ayuda"},
	} {
		fmt.Fprintf(out, "  %s%s  %s\n", ui.Cyan(flag[0]), strings.Repeat(" ", 17-len(flag[0])), flag[i+1])
	}
}

func loginPrompt(locale string) string {
	if locale == "pt" {
		return "Token da API: "
	}
	if locale == "es" {
		return "Token de API: "
	}
	return "API token: "
}
func linkPickerLabel(locale string) string {
	if locale == "pt" {
		return "Selecione uma aplicação para vincular:"
	}
	if locale == "es" {
		return "Selecciona una aplicación para vincular:"
	}
	return "Select an application to link:"
}
func loginRequired(locale string) string {
	if locale == "pt" {
		return "o token da API é obrigatório"
	}
	if locale == "es" {
		return "el token de API es obligatorio"
	}
	return "API token is required"
}
func cancelled(locale string) string {
	if locale == "pt" {
		return "Operação cancelada."
	}
	if locale == "es" {
		return "Operación cancelada."
	}
	return "Operation cancelled."
}

func hasYes(args []string) bool {
	for _, arg := range args {
		if arg == "--yes" || arg == "-y" {
			return true
		}
	}
	return false
}

func destructivePath(args []string, locale string) (bool, string) {
	if len(args) < 2 {
		return false, ""
	}
	group, action := args[0], args[1]
	index := 2
	if group == "app" && action == "file" && len(args) > 2 {
		action = "file " + args[2]
		index = 3
	}
	if group == "app" && action == "network" && len(args) > 2 {
		action = "network " + args[2]
		index = 3
	}
	if group == "db" && action == "credentials" && len(args) > 2 {
		action = "credentials " + args[2]
		index = 3
	}
	if group == "workspace" && (action == "invite" || action == "role" || action == "member" || action == "action-request") && len(args) > 2 {
		action = action + " " + args[2]
		index = 3
	}
	allowed := map[string]bool{
		"app delete": true, "app file delete": true, "app network unpublish": true, "app network purge-cache": true,
		"db delete": true, "db reset": true, "db credentials reset": true,
		"workspace delete": true, "workspace invite revoke": true, "workspace invite decline": true, "workspace role delete": true, "workspace member remove": true,
		"snapshot restore": true, "folder delete": true,
	}
	path := group + " " + action
	if !allowed[path] {
		return false, ""
	}
	label := map[string]map[string]string{
		"pt": {"app delete": "Excluir aplicação " + strings.Join(args[index:], " ") + "?", "app file delete": "Excluir arquivo " + strings.Join(args[index:], " ") + "?", "app network unpublish": "Despublicar aplicação?", "app network purge-cache": "Limpar o cache da CDN?", "db delete": "Excluir banco de dados?", "db reset": "Redefinir banco de dados?", "db credentials reset": "Redefinir credenciais?", "workspace delete": "Excluir workspace?", "workspace invite revoke": "Revogar convite?", "workspace invite decline": "Recusar convite?", "workspace role delete": "Excluir função?", "workspace member remove": "Remover membro?", "snapshot restore": "Restaurar snapshot " + strings.Join(args[index:], " ") + "?", "folder delete": "Excluir pasta " + strings.Join(args[index:], " ") + "?"},
		"es": {"app delete": "¿Eliminar aplicación " + strings.Join(args[index:], " ") + "?", "app file delete": "¿Eliminar archivo " + strings.Join(args[index:], " ") + "?", "app network unpublish": "¿Despublicar aplicación?", "app network purge-cache": "¿Limpiar la caché CDN?", "db delete": "¿Eliminar base de datos?", "db reset": "¿Restablecer base de datos?", "db credentials reset": "¿Restablecer credenciales?", "workspace delete": "¿Eliminar espacio de trabajo?", "workspace invite revoke": "¿Revocar invitación?", "workspace invite decline": "¿Rechazar invitación?", "workspace role delete": "¿Eliminar rol?", "workspace member remove": "¿Eliminar miembro?", "snapshot restore": "¿Restaurar snapshot " + strings.Join(args[index:], " ") + "?", "folder delete": "¿Eliminar carpeta " + strings.Join(args[index:], " ") + "?"},
		"en": {"app delete": "Delete application " + strings.Join(args[index:], " ") + "?", "app file delete": "Delete file " + strings.Join(args[index:], " ") + "?", "app network unpublish": "Unpublish application?", "app network purge-cache": "Purge CDN cache?", "db delete": "Delete database?", "db reset": "Reset database?", "db credentials reset": "Reset database credentials?", "workspace delete": "Delete workspace?", "workspace invite revoke": "Revoke invitation?", "workspace invite decline": "Decline invitation?", "workspace role delete": "Delete role?", "workspace member remove": "Remove member?", "snapshot restore": "Restore snapshot " + strings.Join(args[index:], " ") + "?", "folder delete": "Delete folder " + strings.Join(args[index:], " ") + "?"},
	}[locale][path]
	return true, label
}

func fail(out io.Writer, jsonMode bool, code string, err error) int {
	var apiErr *rest.APIError
	message := err.Error()
	if errors.As(err, &apiErr) {
		code = apiErr.Code
		message = apiErr.Message + " " + ui.Dim("("+apiErr.Code+")")
		if apiErr.Code == "APP_SHIELD_COOLDOWN" {
			message = ui.Text("shield_blocked")
			if until := apiErr.Until(); until != nil {
				message += " " + ui.Dim("("+fmt.Sprintf(ui.Text("shield_until"), until.Local().Format("15:04"))+")")
			}
		}
	}
	if jsonMode {
		_ = json.NewEncoder(out).Encode(map[string]any{"error": map[string]string{"code": code, "message": err.Error()}})
		return 1
	}
	ui.Failure(out, message, errorHint(code, apiErr))
	return 1
}

func errorHint(code string, apiErr *rest.APIError) string {
	switch {
	case code == "NOT_AUTHENTICATED" || (apiErr != nil && apiErr.IsAuthenticationError()):
		return localizedHint("auth")
	case apiErr != nil && apiErr.IsPermissionError():
		return localizedHint("permission")
	case code == "APP_SHIELD_COOLDOWN":
		return ui.Text("shield_ram_hint")
	case strings.HasPrefix(code, "unknown") || code == "COMMAND_FAILED" && apiErr == nil:
		return localizedHint("help")
	}
	return ""
}

func localizedHint(kind string) string {
	hints := map[string]map[string]string{
		"auth":       {"pt": "Faça login com: vertra auth login", "en": "Sign in with: vertra auth login", "es": "Inicia sesión con: vertra auth login"},
		"permission": {"pt": "Sua chave não tem permissão para isso. Confira os escopos no painel.", "en": "Your key lacks permission for this. Check its scopes in the dashboard.", "es": "Tu clave no tiene permiso para esto. Revisa sus alcances en el panel."},
		"help":       {"pt": "Veja os comandos com: vertra --help", "en": "See available commands with: vertra --help", "es": "Mira los comandos con: vertra --help"},
	}
	return hints[kind][ui.Locale]
}
