package apps

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/vertracloud/cli/internal/local"
	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// Run executes one app command. The caller owns JSON encoding and authentication;
// every API operation below goes through the Go SDK.
func Run(ctx context.Context, client *vertracloud.Client, command string, args []string) (any, error) {
	return RunWithOptions(ctx, client, command, args, false)
}

// RunWithOptions executes an app command with output-mode context from the dispatcher.
func RunWithOptions(ctx context.Context, client *vertracloud.Client, command string, args []string, jsonMode bool) (any, error) {
	if client == nil || client.Apps == nil {
		return nil, errors.New("apps: SDK client is unavailable")
	}
	switch strings.ToLower(command) {
	case "list", "ls":
		return list(ctx, client, args)
	case "info", "get":
		return oneID(ctx, client, args, "app info", func(id string) (any, error) { return client.Apps.Get(ctx, id) })
	case "status":
		return status(ctx, client, args)
	case "metrics":
		return metrics(ctx, client, args)
	case "logs":
		return logs(ctx, client, args)
	case "start":
		return oneID(ctx, client, args, "app start", func(id string) (any, error) { return client.Apps.Start(ctx, id) })
	case "restart":
		return restart(ctx, client, args)
	case "stop":
		return oneID(ctx, client, withoutYes(args), "app stop", func(id string) (any, error) { return client.Apps.Stop(ctx, id) })
	case "delete", "remove":
		return remove(ctx, client, args)
	case "download":
		return download(ctx, client, args)
	case "config":
		return config(ctx, client, args)
	case "releases":
		return releases(ctx, client, args)
	case "rollback":
		return rollback(ctx, client, args, jsonMode)
	case "upload", "create":
		return upload(ctx, client, args, jsonMode)
	case "commit", "push", "deploy":
		return commit(ctx, client, args, jsonMode)
	case "env":
		return env(ctx, client, args)
	case "file":
		return file(ctx, client, args, jsonMode)
	case "network":
		return network(ctx, client, args)
	case "domains":
		return domains(ctx, client, args)
	default:
		return nil, fmt.Errorf("unknown app command %q", command)
	}
}

func fs(name string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	return f
}

// reorderFlags lets the flag package accept the Commander-style "id --flag"
// form as well as the usual "--flag id" form.
func reorderFlags(args []string, values, bools map[string]bool) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		a = canonicalFlag(a)
		key := a
		if n := strings.IndexByte(a, '='); n >= 0 {
			key = a[:n]
		}
		if values[key] || bools[key] {
			flags = append(flags, a)
			if values[key] && !strings.Contains(a, "=") && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		positional = append(positional, a)
	}
	return append(flags, positional...)
}

func canonicalFlag(arg string) string {
	prefix, value := arg, ""
	if n := strings.IndexByte(arg, '='); n >= 0 {
		prefix, value = arg[:n], arg[n:]
	}
	switch prefix {
	case "-A":
		prefix = "--all"
	case "-f":
		prefix = "--follow"
	case "-o":
		prefix = "--output"
	case "-r":
		prefix = "--restart"
	case "-y":
		prefix = "--yes"
	}
	return prefix + value
}

func parse(f *flag.FlagSet, args []string, values, bools map[string]bool) ([]string, error) {
	for i := 0; i < len(args); i++ {
		arg := canonicalFlag(args[i])
		key := arg
		if n := strings.IndexByte(arg, '='); n >= 0 {
			key = arg[:n]
		}
		if key == "--" {
			break
		}
		if strings.HasPrefix(key, "-") && !values[key] && !bools[key] {
			return nil, fmt.Errorf("unknown flag %q", args[i])
		}
		if values[key] && !strings.Contains(arg, "=") {
			i++
		}
	}
	if err := f.Parse(reorderFlags(args, values, bools)); err != nil {
		return nil, err
	}
	return f.Args(), nil
}

func requireID(ctx context.Context, client *vertracloud.Client, args []string, command string) (string, []string, error) {
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		return args[0], args[1:], nil
	}
	id, err := resolveAppID(ctx, client, "", command)
	if err != nil {
		return "", nil, err
	}
	return id, args, nil
}

func resolveAppID(ctx context.Context, client *vertracloud.Client, explicit, command string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if id := projectID(); id != "" {
		return id, nil
	}
	if !ui.IsInteractive() {
		return "", fmt.Errorf("%s requires an app id in non-interactive mode", command)
	}
	if client == nil || client.Account == nil {
		return "", errors.New("apps: account service is unavailable")
	}
	account, err := client.Account.Get(ctx)
	if err != nil {
		return "", err
	}
	if len(account.Applications) == 0 {
		return "", errors.New("no applications available")
	}
	choices := make([]ui.Choice, 0, len(account.Applications))
	for _, app := range account.Applications {
		choices = append(choices, ui.Choice{ID: app.ID, Label: app.Name, Hint: app.ID + "  " + string(app.Status)})
	}
	return ui.PickID("Select an application:", choices)
}

func oneID(ctx context.Context, client *vertracloud.Client, args []string, name string, fn func(string) (any, error)) (any, error) {
	id, rest, err := requireID(ctx, client, args, name)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("%s accepts one app id", name)
	}
	return fn(id)
}

func list(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	if client.Account == nil {
		return nil, errors.New("apps: account service is unavailable")
	}
	f := fs("app list")
	all := f.Bool("all", false, "return all rows")
	pageSize := f.Int("page-size", 0, "compatibility option")
	workspaceID := f.String("workspace", "", "list apps in a workspace")
	var filters repeatFlag
	f.Var(&filters, "filter", "filter key=value")
	pos, err := parse(f, args, map[string]bool{"--page-size": true, "--filter": true, "--workspace": true}, map[string]bool{"--all": true, "-A": true})
	if err != nil || len(pos) != 0 {
		if err == nil {
			err = fmt.Errorf("unexpected arguments: %s", strings.Join(pos, " "))
		}
		return nil, err
	}
	if *pageSize < 0 {
		return nil, errors.New("--page-size must be positive")
	}
	for _, expr := range filters {
		key, _, _, err := parseFilter(expr)
		if err != nil {
			return nil, err
		}
		if !knownFilter[strings.ToLower(key)] {
			return nil, fmt.Errorf("unknown filter key %q", key)
		}
	}
	// Like the dashboard, live state comes from a second request that runs in
	// parallel; the list still renders if it fails.
	type liveResult struct {
		items []vertracloud.ApplicationStatusShort
		err   error
	}
	liveCh := make(chan liveResult, 1)
	go func() {
		var opts []rest.RequestOpt
		if *workspaceID != "" {
			opts = append(opts, rest.WithWorkspaceID(*workspaceID))
		}
		items, err := client.Apps.StatusAll(ctx, opts...)
		liveCh <- liveResult{items, err}
	}()
	var apps []vertracloud.Application
	var organization vertracloud.WorkspaceResourceOrganization
	if *workspaceID == "" {
		account, err := client.Account.Get(ctx)
		if err != nil {
			return nil, err
		}
		apps, organization = account.Applications, account.ResourceOrganization
	} else {
		workspace, err := client.Workspaces.Get(ctx, *workspaceID)
		if err != nil {
			return nil, err
		}
		apps, organization = workspace.Applications, workspace.ResourceOrganization
	}
	for _, expr := range filters {
		key, op, value, _ := parseFilter(expr)
		filtered := apps[:0]
		for _, app := range apps {
			if appMatchesFilter(app, key, op, value) {
				filtered = append(filtered, app)
			}
		}
		apps = filtered
	}
	_ = all // output is JSON; pagination is a human presentation concern.
	rows := presentApps(apps, organization)
	if live := <-liveCh; live.err == nil {
		byID := make(map[string]vertracloud.ApplicationStatusShort, len(live.items))
		for _, item := range live.items {
			byID[item.ID] = item
		}
		for i, app := range apps {
			item, ok := byID[app.ID]
			if !ok {
				continue
			}
			if app.Status != vertracloud.ApplicationStatusBuilding {
				rows[i]["status"] = map[bool]string{true: "up", false: "down"}[item.Running]
			}
			rows[i]["installing"] = item.Installing != nil && *item.Installing
			rows[i]["cpu"], rows[i]["ram_used"] = item.CPU, item.RAM
		}
	}
	return ui.Presentation{JSON: apps, Human: rows}, nil
}

func presentApps(apps []vertracloud.Application, organization vertracloud.WorkspaceResourceOrganization) []map[string]any {
	encoded, _ := json.Marshal(apps)
	var rows []map[string]any
	_ = json.Unmarshal(encoded, &rows)
	folders := make(map[string][]string)
	colors := make(map[string]string)
	for _, folder := range organization.Folders {
		for _, resource := range folder.Resources {
			if colors[resource.ResourceID] == "" {
				colors[resource.ResourceID] = string(folder.Color)
			}
			if resource.ResourceType == vertracloud.WorkspaceResourceTypeApplication {
				folders[resource.ResourceID] = append(folders[resource.ResourceID], folder.Name)
			}
		}
	}
	favorites := make(map[string]bool)
	for _, favorite := range organization.Favorites {
		if favorite.ResourceType == vertracloud.WorkspaceResourceTypeApplication {
			favorites[favorite.ResourceID] = true
		}
	}
	for i, app := range apps {
		rows[i]["folder"] = strings.Join(folders[app.ID], ", ")
		rows[i]["favorite"] = favorites[app.ID]
		rows[i]["folder_color"] = colors[app.ID]
	}
	return rows
}

type repeatFlag []string

var knownFilter = map[string]bool{"id": true, "name": true, "status": true, "lang": true, "language": true, "type": true, "domain": true, "ram": true}

func (r *repeatFlag) String() string     { return strings.Join(*r, ",") }
func (r *repeatFlag) Set(v string) error { *r = append(*r, v); return nil }

func parseFilter(raw string) (key, op, value string, err error) {
	for _, candidate := range []string{">=", "<=", "!=", "=", ">", "<"} {
		if i := strings.Index(raw, candidate); i > 0 {
			key, value = strings.TrimSpace(raw[:i]), strings.TrimSpace(raw[i+len(candidate):])
			if value == "" {
				return "", "", "", fmt.Errorf("invalid filter %q", raw)
			}
			return key, candidate, value, nil
		}
	}
	return "", "", "", fmt.Errorf("invalid filter %q (use key=value)", raw)
}

func appMatches(app vertracloud.Application, key, value string) bool {
	return appMatchesFilter(app, key, "=", value)
}

func appMatchesFilter(app vertracloud.Application, key, op, value string) bool {
	var got string
	switch strings.ToLower(key) {
	case "id":
		got = app.ID
	case "name":
		got = app.Name
	case "status":
		got = string(app.Status)
	case "lang", "language":
		got = string(app.Language)
	case "type":
		if app.Type == vertracloud.ApplicationTypeBot {
			got = "bot"
		} else {
			got = "website"
		}
	case "domain":
		hosts := make([]string, 0, len(app.Domains)+1)
		for _, d := range app.Domains {
			hosts = append(hosts, d.Hostname)
		}
		if app.Subdomain != nil {
			hosts = append(hosts, *app.Subdomain)
		}
		return domainFilterMatches(hosts, op, value)
	case "ram":
		got = strconv.Itoa(app.RAM)
	default:
		return false
	}
	if strings.EqualFold(key, "ram") && op != "=" {
		want, err := strconv.Atoi(value)
		if err != nil {
			return false
		}
		gotN := app.RAM
		switch op {
		case ">":
			return gotN > want
		case ">=":
			return gotN >= want
		case "<":
			return gotN < want
		case "<=":
			return gotN <= want
		case "!=":
			return gotN != want
		}
	}
	if op == "!=" {
		return !strings.EqualFold(got, value)
	}
	if op != "=" {
		return false
	}
	if strings.EqualFold(key, "name") || strings.EqualFold(key, "domain") {
		return strings.Contains(strings.ToLower(got), strings.ToLower(value))
	}
	return strings.EqualFold(got, value)
}

func domainFilterMatches(hosts []string, op, value string) bool {
	contains := false
	for _, host := range hosts {
		if strings.Contains(strings.ToLower(host), strings.ToLower(value)) {
			contains = true
			break
		}
	}
	switch op {
	case "=":
		return contains
	case "!=":
		for _, host := range hosts {
			if strings.EqualFold(host, value) {
				return false
			}
		}
		return true
	}
	return false
}

func status(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	f := fs("app status")
	all := f.Bool("all", false, "return the SDK status summary")
	pos, err := parse(f, args, nil, map[string]bool{"--all": true})
	if err != nil {
		return nil, err
	}
	if *all {
		if len(pos) != 0 {
			return nil, errors.New("app status --all does not take an id")
		}
		accountCh := make(chan vertracloud.AccountInfo, 1)
		go func() {
			account, _ := client.Account.Get(ctx)
			accountCh <- account
		}()
		items, err := client.Apps.StatusAll(ctx)
		if err != nil {
			return nil, err
		}
		resources := map[string]ui.Resource{}
		for _, app := range (<-accountCh).Applications {
			resources[app.ID] = shieldResource(app)
		}
		return ui.WithResources(items, resources), nil
	}
	id, rest, err := requireID(ctx, client, pos, "app status")
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, errors.New("app status accepts one app id")
	}
	// The status route has no name or RAM limit; fetch the app alongside it.
	appCh := make(chan vertracloud.Application, 1)
	go func() {
		app, _ := client.Apps.Get(ctx, id)
		appCh <- app
	}()
	item, err := client.Apps.Status(ctx, id)
	if err != nil {
		return nil, err
	}
	app := <-appCh
	return ui.WithResources(item, map[string]ui.Resource{app.ID: shieldResource(app)}), nil
}

func shieldResource(app vertracloud.Application) ui.Resource {
	return ui.Resource{Name: app.Name, RAM: app.RAM, Shield: app.ShieldCooldown}
}

func metrics(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	f := fs("app metrics")
	rangeParam := f.String("range", "", "10m, 30m, or 24h")
	f.Bool("table", false, "render metrics as a table")
	f.Bool("all", false, "show all metrics")
	pos, err := parse(f, args, map[string]bool{"--range": true}, map[string]bool{"--table": true, "--all": true, "-A": true})
	if err != nil {
		return nil, err
	}
	// These options are accepted for CLI parity; output remains the existing JSON response.
	if *rangeParam != "" && *rangeParam != "10m" && *rangeParam != "30m" && *rangeParam != "24h" {
		return nil, errors.New("--range must be 10m, 30m, or 24h")
	}
	id, rest, err := requireID(ctx, client, pos, "app metrics")
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, errors.New("app metrics accepts one app id")
	}
	return client.Apps.Metrics(ctx, id, &vertracloud.ApplicationMetricsParams{Range: *rangeParam})
}

func logs(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	f := fs("app logs")
	follow := f.Bool("follow", false, "follow realtime events")
	realtime := f.Bool("realtime", false, "follow realtime events")
	pos, err := parse(f, args, nil, map[string]bool{"--follow": true, "-f": true, "--realtime": true})
	if err != nil {
		return nil, err
	}
	id, extra, err := requireID(ctx, client, pos, "app logs")
	if err != nil {
		return nil, err
	}
	if len(extra) != 0 {
		return nil, errors.New("app logs accepts one app id")
	}
	if !*follow && !*realtime {
		return client.Apps.Logs(ctx, id)
	}
	return nil, errors.New("logs --follow/--realtime is unavailable in the JSON dispatcher")
}

// Follow streams realtime log events directly to out. It is separate from Run
// because the top-level dispatcher owns the JSON response envelope, while a
// live command must write each SDK event as soon as it arrives.
func Follow(ctx context.Context, client *vertracloud.Client, args []string, out io.Writer) error {
	if client == nil || client.Apps == nil {
		return errors.New("apps: SDK client is unavailable")
	}
	f := fs("app logs")
	follow := f.Bool("follow", false, "follow realtime events")
	realtime := f.Bool("realtime", false, "follow realtime events")
	pos, err := parse(f, args, nil, map[string]bool{"--follow": true, "-f": true, "--realtime": true})
	if err != nil {
		return err
	}
	id, extra, err := requireID(ctx, client, pos, "app logs")
	if err != nil {
		return err
	}
	if len(extra) != 0 {
		return errors.New("app logs accepts one app id")
	}
	if !*follow && !*realtime {
		return errors.New("logs follow requires --follow or --realtime")
	}
	stream, err := client.Apps.Realtime(ctx, id, nil)
	if err != nil {
		return err
	}
	defer stream.Close()
	for stream.Next(ctx) {
		if _, err := fmt.Fprintln(out, ui.LogText(stream.Event().Data)); err != nil {
			return err
		}
	}
	if err := stream.Err(); errors.Is(err, context.Canceled) {
		return nil
	} else {
		return err
	}
}

func restart(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	f := fs("app restart")
	reinstall := f.Bool("reinstall", false, "reinstall dependencies")
	forceBuild := f.Bool("force-build", false, "force a build")
	pos, err := parse(f, args, nil, map[string]bool{"--reinstall": true, "--force-build": true})
	if err != nil {
		return nil, err
	}
	id, rest, err := requireID(ctx, client, pos, "app restart")
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, errors.New("app restart accepts one app id")
	}
	body := &vertracloud.ApplicationRestartBody{}
	if *reinstall {
		body.ReinstallDependencies = reinstall
	}
	if *forceBuild {
		body.ForceBuild = forceBuild
	}
	return client.Apps.Restart(ctx, id, body)
}

func remove(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	f := fs("app delete")
	yes := f.Bool("yes", false, "skip confirmation")
	pos, err := parse(f, args, nil, map[string]bool{"--yes": true, "-y": true})
	if err != nil {
		return nil, err
	}
	id, rest, err := requireID(ctx, client, pos, "app delete")
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, errors.New("app delete accepts one app id")
	}
	if !*yes {
		ok, err := confirm(os.Stdin, fmt.Sprintf("Delete app %s? [y/N] ", id))
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrCancelled
		}
	}
	if err := client.Apps.Delete(ctx, id); err != nil {
		return nil, err
	}
	return map[string]string{"id": id}, nil
}

func confirm(r io.Reader, prompt string) (bool, error) {
	_, _ = fmt.Fprint(os.Stderr, prompt)
	s := bufio.NewScanner(r)
	if !s.Scan() {
		return false, s.Err()
	}
	switch strings.ToLower(strings.TrimSpace(s.Text())) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func promptText(prompt string) (string, error) {
	if !ui.IsInteractive() {
		return "", errors.New("interactive input requires a terminal; provide the option explicitly")
	}
	_, _ = fmt.Fprint(os.Stdout, prompt)
	var value strings.Builder
	for {
		var b [1]byte
		n, err := os.Stdin.Read(b[:])
		if err != nil {
			if errors.Is(err, io.EOF) && value.Len() > 0 {
				return strings.TrimSpace(value.String()), nil
			}
			return "", err
		}
		if n == 0 {
			continue
		}
		if b[0] == '\n' {
			return strings.TrimSpace(strings.TrimSuffix(value.String(), "\r")), nil
		}
		value.WriteByte(b[0])
	}
}

func download(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	f := fs("app download")
	out := f.String("output", "", "destination zip")
	pos, err := parse(f, args, map[string]bool{"--output": true, "-o": true}, nil)
	if err != nil {
		return nil, err
	}
	id, rest, err := requireID(ctx, client, pos, "app download")
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, errors.New("app download accepts one app id")
	}
	body, err := client.Apps.Download(ctx, id)
	if err != nil {
		return nil, err
	}
	dest := *out
	if dest == "" {
		dest = id + ".zip"
	}
	n, err := local.SaveStream(dest, body)
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": dest, "bytes": n}, nil
}

func config(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	f := fs("app config")
	name := f.String("name", "", "app name")
	ram := f.Int("ram", 0, "RAM in MB")
	main := f.String("main", "", "main file")
	version := f.String("version", "", "runtime version")
	start := f.String("start", "", "start command")
	zeroDowntime := f.String("zero-downtime", "", "auto, on or off")
	autoRollback := f.String("auto-rollback", "", "on or off")
	healthPath := f.String("health-path", "", "health check path")
	clearHealth := f.Bool("clear-health-path", false, "remove the health check path")
	pos, err := parse(f, args, map[string]bool{"--name": true, "--ram": true, "--main": true, "--version": true, "--start": true, "--zero-downtime": true, "--auto-rollback": true, "--health-path": true}, map[string]bool{"--clear-health-path": true})
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	f.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	deploy, err := deployUpdate(set, *zeroDowntime, *autoRollback, *healthPath, *clearHealth)
	if err != nil {
		return nil, err
	}
	id, rest, err := requireID(ctx, client, pos, "app config")
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, errors.New("app config accepts one app id")
	}
	body := vertracloud.ApplicationUpdateConfigBody{}
	if *name != "" {
		body.Name = name
	}
	if *ram != 0 {
		if *ram < 0 {
			return nil, errors.New("--ram must be positive")
		}
		body.RAM = ram
	}
	if *main != "" {
		body.MainFile = main
	}
	if *version != "" {
		v := vertracloud.ApplicationVersion(*version)
		body.Version = &v
	}
	if *start != "" {
		body.StartCommand = vertracloud.NullableValue(*start)
	}
	body.Deploy = deploy
	if body.Name == nil && body.RAM == nil && body.MainFile == nil && body.Version == nil && body.StartCommand == nil && body.Deploy == nil {
		return nil, errors.New("app config requires at least one option")
	}
	msg, err := client.Apps.UpdateConfig(ctx, id, body)
	return msg, friendly(err, false)
}

func upload(ctx context.Context, client *vertracloud.Client, args []string, jsonMode bool) (any, error) {
	f := fs("app upload")
	file := f.String("file", "", "zip file")
	name := f.String("name", "", "app name")
	memory := f.Int("memory", 0, "RAM in MB")
	main := f.String("main", "", "main file")
	version := f.String("version", "", "runtime version")
	description := f.String("description", "", "description")
	subdomain := f.String("subdomain", "", "subdomain")
	start := f.String("start", "", "start command")
	workspace := f.String("workspace", "", "workspace id")
	noWait := f.Bool("no-wait", false, "return right after the upload, without waiting for the build")
	language := ""
	pos, err := parse(f, args, map[string]bool{"--file": true, "--name": true, "--memory": true, "--main": true, "--version": true, "--description": true, "--subdomain": true, "--start": true, "--workspace": true}, map[string]bool{"--no-wait": true})
	if err != nil {
		return nil, err
	}
	if len(pos) != 0 {
		return nil, fmt.Errorf("unexpected arguments: %s", strings.Join(pos, " "))
	}
	if cwd, e := os.Getwd(); e == nil {
		if cfg, e := local.ReadProject(cwd); e == nil && cfg != nil {
			if *name == "" {
				*name = cfg["NAME"]
			}
			if *memory == 0 && cfg["MEMORY"] != "" {
				v, parseErr := strconv.Atoi(cfg["MEMORY"])
				if parseErr != nil {
					return nil, errors.New("MEMORY in vertracloud.config must be a number")
				}
				*memory = v
			}
			if *main == "" {
				*main = cfg["MAIN"]
			}
			if *version == "" {
				*version = cfg["VERSION"]
			}
			if *subdomain == "" {
				*subdomain = cfg["SUBDOMAIN"]
			}
			if *start == "" {
				*start = cfg["START"]
			}
			language = cfg["LANGUAGE"]
		}
	}
	if *name == "" && !jsonMode {
		*name, err = promptText("App name: ")
		if err != nil {
			return nil, err
		}
	}
	if *memory == 0 && !jsonMode {
		value, e := promptText("App memory in MB: ")
		if e != nil {
			return nil, e
		}
		if value != "" {
			*memory, e = strconv.Atoi(value)
			if e != nil {
				return nil, errors.New("memory must be a number")
			}
		}
	}
	if *main == "" && !jsonMode {
		*main, err = promptText("App main file: ")
		if err != nil {
			return nil, err
		}
	}
	if *name == "" || *memory <= 0 || *main == "" {
		return nil, errors.New("upload requires --name, --memory, and --main")
	}
	if *version == "" && !jsonMode && ui.IsInteractive() {
		*version, err = promptText("App version [recommended]: ")
		if err != nil {
			return nil, err
		}
	}
	if *version == "" {
		*version = "recommended"
	}
	stop := spin(jsonMode, "upload")
	defer stop()
	r, filename, err := sourceReader(*file)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var descriptionValue *vertracloud.Nullable[string]
	if *description != "" {
		descriptionValue = vertracloud.NullableValue(*description)
	}
	app, err := client.Apps.Create(ctx, vertracloud.ApplicationCreateParams{File: r, FileName: filename, Name: *name, Memory: *memory, Main: *main, Version: vertracloud.ApplicationVersion(*version), Language: vertracloud.ApplicationLanguage(language), Description: descriptionValue, Subdomain: *subdomain, Start: *start, WorkspaceID: *workspace})
	stop()
	if err != nil {
		return nil, err
	}
	if cwd, e := os.Getwd(); e == nil && app.ID != "" {
		if e := local.SetProjectID(cwd, app.ID); e != nil {
			return nil, e
		}
	}
	// Create answers 202 while the build runs; follow it until the app is up or down.
	if *noWait || app.Status != vertracloud.ApplicationStatusBuilding {
		return app, nil
	}
	stopBuild := spin(jsonMode, "build")
	defer stopBuild()
	ready, err := client.Apps.WaitUntilReady(ctx, app.ID, nil)
	stopBuild()
	var down *vertracloud.AppNotReadyError
	if errors.As(err, &down) {
		return nil, fmt.Errorf("the build of app %s failed and the app is down (%s)%s; see the cause with: vertra app logs %s", app.ID, down.Code, buildErrorHint(down.Code), app.ID)
	}
	if err != nil {
		return nil, err
	}
	return ready, nil
}

func commit(ctx context.Context, client *vertracloud.Client, args []string, jsonMode bool) (any, error) {
	f := fs("app commit")
	file := f.String("file", "", "zip file")
	restart := f.Bool("restart", false, "restart after upload")
	dest := f.String("path", "", "destination folder inside the application (default: root)")
	message := f.String("message", "", "commit message (default: subject of the last git commit)")
	f.StringVar(message, "m", "", "alias for --message")
	pos, err := parse(f, args, map[string]bool{"--file": true, "--path": true, "--message": true, "-m": true}, map[string]bool{"--restart": true, "-r": true})
	if err != nil {
		return nil, err
	}
	id, rest, err := requireID(ctx, client, pos, "app commit")
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("unexpected arguments: %s", strings.Join(rest, " "))
	}
	var msg string
	given := false
	f.Visit(func(fl *flag.Flag) { given = given || fl.Name == "message" || fl.Name == "m" })
	if given {
		msg = normalizeMessage(*message)
	} else {
		msg = normalizeMessage(gitSubject(ctx, commitDir(*file)))
	}
	stop := spin(jsonMode, "commit")
	defer stop()
	r, filename, err := sourceReader(*file)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	response, err := client.Apps.Files().Upload(ctx, id, vertracloud.ApplicationFileUploadParams{File: r, FileName: filename, Restart: restart, Path: *dest, Message: msg})
	if err != nil {
		return nil, err
	}
	if response.ImageBuild != nil || response.Message != "" {
		// App on a custom image: the server decides what the commit does; show what it said.
		human := map[string]any{"app_id": response.AppID, "updated_at": response.UpdatedAt}
		if response.Message != "" {
			human["message"] = ui.Plain(response.Message)
		}
		if b := response.ImageBuild; b != nil {
			human["image_id"], human["build_id"] = b.ImageID, b.BuildID
		}
		return ui.Presentation{JSON: response, Human: human}, nil
	}
	return response, nil
}

func env(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	if len(args) == 0 {
		return nil, errors.New("missing env command")
	}
	switch strings.ToLower(args[0]) {
	case "list":
		f := fs("app env list")
		app := f.String("app", "", "app id")
		pos, err := parse(f, args[1:], map[string]bool{"--app": true}, nil)
		if err != nil {
			return nil, err
		}
		if len(pos) != 0 {
			return nil, fmt.Errorf("unexpected arguments: %s", strings.Join(pos, " "))
		}
		id, err := resolveAppID(ctx, client, *app, "app env list")
		if err != nil {
			return nil, err
		}
		return client.Apps.Envs().List(ctx, id)
	case "set":
		f := fs("app env set")
		app := f.String("app", "", "app id")
		from := f.String("from-file", "", "dotenv file")
		pos, err := parse(f, args[1:], map[string]bool{"--app": true, "--from-file": true}, nil)
		if err != nil {
			return nil, err
		}
		id, err := resolveAppID(ctx, client, *app, "app env set")
		if err != nil {
			return nil, err
		}
		pairs := pos
		if *app == "" && projectID() == "" && len(pairs) > 0 {
			id, pairs = pairs[0], pairs[1:]
		}
		vars, err := envPairs(pairs)
		if err != nil {
			return nil, err
		}
		if *from != "" {
			more, e := readDotEnv(*from)
			if e != nil {
				return nil, e
			}
			vars = append(vars, more...)
		}
		if len(vars) == 0 {
			return nil, errors.New("env set requires KEY=VALUE")
		}
		return client.Apps.Envs().Set(ctx, id, vars)
	case "remove", "delete", "unset":
		f := fs("app env remove")
		app := f.String("app", "", "app id")
		pos, err := parse(f, withoutYes(args[1:]), map[string]bool{"--app": true}, nil)
		if err != nil {
			return nil, err
		}
		id, err := resolveAppID(ctx, client, *app, "app env remove")
		if err != nil {
			return nil, err
		}
		keys := pos
		if *app == "" && projectID() == "" && len(keys) > 0 {
			id, keys = keys[0], keys[1:]
		}
		if len(keys) == 0 {
			return nil, errors.New("env remove requires a key")
		}
		current, err := client.Apps.Envs().List(ctx, id)
		if err != nil {
			return nil, err
		}
		removed := []string{}
		for _, key := range keys {
			for _, e := range current {
				if e.Key == key {
					if err := client.Apps.Envs().Delete(ctx, id, e.ID); err != nil {
						return nil, err
					}
					removed = append(removed, key)
					break
				}
			}
		}
		return map[string]any{"removed": removed}, nil
	default:
		return nil, fmt.Errorf("unknown env command %q", args[0])
	}
}

func file(ctx context.Context, client *vertracloud.Client, args []string, jsonMode bool) (any, error) {
	if len(args) == 0 {
		return nil, errors.New("missing file command")
	}
	switch strings.ToLower(args[0]) {
	case "list":
		f := fs("app file list")
		app := f.String("app", "", "app id")
		pos, err := parse(f, args[1:], map[string]bool{"--app": true}, nil)
		if err != nil {
			return nil, err
		}
		if len(pos) > 1 {
			return nil, errors.New("app file list accepts one path")
		}
		id, err := resolveAppID(ctx, client, *app, "app file list")
		if err != nil {
			return nil, err
		}
		path := ""
		if len(pos) > 0 {
			path = pos[0]
		}
		return client.Apps.Files().List(ctx, id, vertracloud.ApplicationFileListParams{Path: path})
	case "read":
		f := fs("app file read")
		app := f.String("app", "", "app id")
		out := f.String("output", "", "destination")
		pos, err := parse(f, args[1:], map[string]bool{"--app": true, "--output": true, "-o": true}, nil)
		if err != nil {
			return nil, err
		}
		id, path, err := fileIDPath(ctx, client, pos, *app, "app file read")
		if err != nil {
			return nil, err
		}
		c, err := client.Apps.Files().Read(ctx, id, vertracloud.ApplicationFileReadParams{Path: path})
		if err != nil {
			return nil, err
		}
		b, err := c.Bytes()
		if err != nil {
			return nil, err
		}
		if jsonMode {
			return map[string]any{"path": path, "size": len(b), "encoding": "base64", "data": base64.StdEncoding.EncodeToString(b)}, nil
		}
		if *out != "" {
			if err := os.WriteFile(*out, b, 0600); err != nil {
				return nil, err
			}
			return map[string]any{"path": *out, "bytes": len(b)}, nil
		}
		return map[string]any{"path": path, "size": len(b), "encoding": "base64", "data": base64.StdEncoding.EncodeToString(b)}, nil
	case "write":
		f := fs("app file write")
		app := f.String("app", "", "app id")
		from := f.String("from", "", "source file")
		pos, err := parse(f, args[1:], map[string]bool{"--app": true, "--from": true}, nil)
		if err != nil {
			return nil, err
		}
		id, path, err := fileIDPath(ctx, client, pos, *app, "app file write")
		if err != nil {
			return nil, err
		}
		var b []byte
		if *from != "" {
			b, err = os.ReadFile(*from)
		} else {
			b, err = io.ReadAll(os.Stdin)
		}
		if err != nil {
			return nil, err
		}
		s := string(b)
		if err := client.Apps.Files().Write(ctx, id, vertracloud.ApplicationFileWriteBody{Path: path, Content: &s}); err != nil {
			return nil, err
		}
		return "success", nil
	case "move", "mv", "rename":
		f := fs("app file move")
		app := f.String("app", "", "app id")
		pos, err := parse(f, args[1:], map[string]bool{"--app": true}, nil)
		if err != nil {
			return nil, err
		}
		if len(pos) != 2 {
			return nil, errors.New("file move requires path and destination")
		}
		id, err := resolveAppID(ctx, client, *app, "app file move")
		if err != nil {
			return nil, err
		}
		from, to := pos[0], pos[1]
		if err := client.Apps.Files().Move(ctx, id, vertracloud.ApplicationFileMoveBody{Path: from, To: to}); err != nil {
			return nil, err
		}
		return map[string]any{"status": "success", "moved": map[string]string{"from": from, "to": to}}, nil
	case "delete", "rm":
		f := fs("app file delete")
		app := f.String("app", "", "app id")
		yes := f.Bool("yes", false, "skip confirmation")
		pos, err := parse(f, args[1:], map[string]bool{"--app": true}, map[string]bool{"--yes": true, "-y": true})
		if err != nil {
			return nil, err
		}
		id, path, err := fileIDPath(ctx, client, pos, *app, "app file delete")
		if err != nil {
			return nil, err
		}
		if !*yes {
			ok, e := confirm(os.Stdin, fmt.Sprintf("Delete file %s? [y/N] ", path))
			if e != nil {
				return nil, e
			}
			if !ok {
				return nil, ErrCancelled
			}
		}
		if err := client.Apps.Files().Delete(ctx, id, vertracloud.ApplicationFileDeleteBody{Path: path}); err != nil {
			return nil, err
		}
		return map[string]string{"path": path}, nil
	default:
		return nil, fmt.Errorf("unknown file command %q", args[0])
	}
}

func network(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	if len(args) == 0 {
		return nil, errors.New("missing network command")
	}
	switch strings.ToLower(args[0]) {
	case "subdomain":
		f := fs("app network subdomain")
		app := f.String("app", "", "app id")
		pos, err := parse(f, args[1:], map[string]bool{"--app": true}, nil)
		if err != nil {
			return nil, err
		}
		id, err := resolveAppID(ctx, client, *app, "app network subdomain")
		if err != nil {
			return nil, err
		}
		if len(pos) == 0 {
			return nil, errors.New("subdomain requires a value")
		}
		return client.Apps.Network().SetSubdomain(ctx, id, pos[0])
	case "publish":
		f := fs("app network publish")
		app := f.String("app", "", "app id")
		sub := f.String("subdomain", "", "subdomain")
		port := f.Int("port", 0, "port the image listens on (apps on a custom image)")
		pos, err := parse(f, args[1:], map[string]bool{"--app": true, "--subdomain": true, "--port": true}, nil)
		if err != nil {
			return nil, err
		}
		if len(pos) != 0 {
			return nil, fmt.Errorf("unexpected arguments: %s", strings.Join(pos, " "))
		}
		var portValue *int
		if visitedFlags(f)["port"] {
			if *port < 1 || *port > 65535 {
				return nil, errors.New("--port must be between 1 and 65535")
			}
			portValue = port
		}
		id, err := resolveAppID(ctx, client, *app, "app network publish")
		if err != nil {
			return nil, err
		}
		return client.Apps.Network().PublishWithPort(ctx, id, *sub, portValue)
	case "unpublish":
		return destructiveNetwork(ctx, client, args[1:], false)
	case "purge-cache":
		return destructiveNetwork(ctx, client, args[1:], true)
	default:
		return nil, fmt.Errorf("unknown network command %q", args[0])
	}
}

func domains(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	if len(args) == 0 {
		return nil, errors.New("missing domains command")
	}
	switch strings.ToLower(args[0]) {
	case "list", "ls":
		f := fs("app domains list")
		app := f.String("app", "", "app id")
		pos, err := parse(f, args[1:], map[string]bool{"--app": true}, nil)
		if err != nil {
			return nil, err
		}
		if *app == "" && len(pos) == 1 {
			*app, pos = pos[0], nil
		}
		if len(pos) != 0 {
			return nil, fmt.Errorf("unexpected arguments: %s", strings.Join(pos, " "))
		}
		id, err := resolveAppID(ctx, client, *app, "app domains list")
		if err != nil {
			return nil, err
		}
		return client.Apps.Domains().List(ctx, id)
	case "add":
		f := fs("app domains add")
		app := f.String("app", "", "app id")
		port := f.Int("port", 0, "port of the app that serves the domain")
		prefix := f.String("path-prefix", "", "literal path added to every request")
		redirectTo := f.String("redirect-to", "", "domain id or hostname to redirect to")
		redirectStatus := f.Int("redirect-status", 0, "redirect status: 301, 302, 307 or 308")
		noWWW := f.Bool("no-www", false, "do not create the automatic www/root companion")
		pos, err := parse(f, args[1:], domainValueFlags, map[string]bool{"--no-www": true})
		if err != nil {
			return nil, err
		}
		if len(pos) != 1 {
			return nil, errors.New("domains add requires a hostname")
		}
		set := visitedFlags(f)
		if err := validateDomainFlags(set, *port, *redirectStatus); err != nil {
			return nil, err
		}
		id, err := resolveAppID(ctx, client, *app, "app domains add")
		if err != nil {
			return nil, err
		}
		body := vertracloud.ApplicationDomainAddBody{Hostname: pos[0], Port: *port, PathPrefix: *prefix, RedirectStatus: vertracloud.ApplicationDomainRedirectStatus(*redirectStatus)}
		if *noWWW {
			off := false
			body.WWW = &off
		}
		if *redirectTo != "" {
			if body.RedirectTo, err = resolveDomainID(ctx, client, id, *redirectTo); err != nil {
				return nil, err
			}
		}
		return client.Apps.Domains().Add(ctx, id, body)
	case "update":
		f := fs("app domains update")
		app := f.String("app", "", "app id")
		port := f.Int("port", 0, "port of the app that serves the domain")
		prefix := f.String("path-prefix", "", "literal path added to every request; empty clears it")
		redirectTo := f.String("redirect-to", "", "domain id or hostname to redirect to; empty clears it")
		redirectStatus := f.Int("redirect-status", 0, "redirect status: 301, 302, 307 or 308")
		primary := f.Bool("primary", false, "make this domain the main one instead of its companion")
		pos, err := parse(f, args[1:], domainValueFlags, map[string]bool{"--primary": true})
		if err != nil {
			return nil, err
		}
		if len(pos) != 1 {
			return nil, errors.New("domains update requires a domain id or hostname")
		}
		set := visitedFlags(f)
		if len(set) == 0 || (len(set) == 1 && set["app"]) {
			return nil, errors.New("domains update requires at least one of --port, --path-prefix, --redirect-to, --redirect-status or --primary")
		}
		if err := validateDomainFlags(set, *port, *redirectStatus); err != nil {
			return nil, err
		}
		id, err := resolveAppID(ctx, client, *app, "app domains update")
		if err != nil {
			return nil, err
		}
		domainID, err := resolveDomainID(ctx, client, id, pos[0])
		if err != nil {
			return nil, err
		}
		body := vertracloud.ApplicationDomainUpdateBody{Primary: *primary}
		if set["port"] {
			body.Port = port
		}
		if set["path-prefix"] {
			body.PathPrefix = nullableString(*prefix)
		}
		if set["redirect-to"] {
			target := *redirectTo
			if target != "" {
				if target, err = resolveDomainID(ctx, client, id, target); err != nil {
					return nil, err
				}
			}
			body.RedirectTo = nullableString(target)
		}
		if set["redirect-status"] {
			body.RedirectStatus = vertracloud.ApplicationDomainRedirectStatus(*redirectStatus)
		}
		return client.Apps.Domains().Update(ctx, id, domainID, body)
	case "remove", "rm", "delete":
		f := fs("app domains remove")
		app := f.String("app", "", "app id")
		yes := f.Bool("yes", false, "skip confirmation")
		pos, err := parse(f, args[1:], map[string]bool{"--app": true}, map[string]bool{"--yes": true, "-y": true})
		if err != nil {
			return nil, err
		}
		if len(pos) != 1 {
			return nil, errors.New("domains remove requires a domain id or hostname")
		}
		id, err := resolveAppID(ctx, client, *app, "app domains remove")
		if err != nil {
			return nil, err
		}
		domain, err := findDomain(ctx, client, id, pos[0])
		if err != nil {
			return nil, err
		}
		if !*yes {
			companion := ""
			if domain.CompanionOf == nil {
				companion = " Its automatic companion is removed too."
			}
			prompt := fmt.Sprintf("Remove domain %s from app %s?%s [y/N] ", ui.Plain(domain.Hostname), appLabel(ctx, client, id), companion)
			ok, e := confirm(os.Stdin, prompt)
			if e != nil {
				return nil, e
			}
			if !ok {
				return nil, ErrCancelled
			}
		}
		if err := client.Apps.Domains().Remove(ctx, id, domain.ID); err != nil {
			return nil, err
		}
		return map[string]string{"id": domain.ID}, nil
	case "dns":
		f := fs("app domains dns")
		app := f.String("app", "", "app id")
		pos, err := parse(f, args[1:], map[string]bool{"--app": true}, nil)
		if err != nil {
			return nil, err
		}
		if len(pos) != 1 {
			return nil, errors.New("domains dns requires a domain id or hostname")
		}
		id, err := resolveAppID(ctx, client, *app, "app domains dns")
		if err != nil {
			return nil, err
		}
		domainID, err := resolveDomainID(ctx, client, id, pos[0])
		if err != nil {
			return nil, err
		}
		return client.Apps.Domains().DNS(ctx, id, domainID)
	default:
		return nil, fmt.Errorf("unknown domains command %q", args[0])
	}
}

var domainValueFlags = map[string]bool{"--app": true, "--port": true, "--path-prefix": true, "--redirect-to": true, "--redirect-status": true}

func visitedFlags(f *flag.FlagSet) map[string]bool {
	set := map[string]bool{}
	f.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	return set
}

func validateDomainFlags(set map[string]bool, port, redirectStatus int) error {
	if set["port"] && (port < 1 || port > 65535) {
		return errors.New("--port must be between 1 and 65535")
	}
	if set["redirect-status"] {
		switch redirectStatus {
		case 301, 302, 307, 308:
		default:
			return errors.New("--redirect-status must be 301, 302, 307 or 308")
		}
	}
	return nil
}

func nullableString(value string) *vertracloud.Nullable[string] {
	if value == "" {
		return vertracloud.NullableNull[string]()
	}
	return vertracloud.NullableValue(value)
}

// resolveDomainID accepts a domain id or a hostname of the app.
func findDomain(ctx context.Context, client *vertracloud.Client, appID, ref string) (vertracloud.ApplicationDomain, error) {
	list, err := client.Apps.Domains().List(ctx, appID)
	if err != nil {
		return vertracloud.ApplicationDomain{}, err
	}
	for _, d := range list {
		if d.ID == ref || strings.EqualFold(d.Hostname, ref) {
			return d, nil
		}
	}
	return vertracloud.ApplicationDomain{}, fmt.Errorf("domain %q not found in this app", ref)
}

func resolveDomainID(ctx context.Context, client *vertracloud.Client, appID, ref string) (string, error) {
	domain, err := findDomain(ctx, client, appID, ref)
	return domain.ID, err
}

func appLabel(ctx context.Context, client *vertracloud.Client, id string) string {
	app, err := client.Apps.Get(ctx, id)
	if err != nil || app.Name == "" {
		return id
	}
	return fmt.Sprintf("%s (%s)", ui.Plain(app.Name), id)
}

func destructiveNetwork(ctx context.Context, client *vertracloud.Client, args []string, purge bool) (any, error) {
	f := fs("app network")
	yes := f.Bool("yes", false, "skip confirmation")
	pos, err := parse(f, args, nil, map[string]bool{"--yes": true, "-y": true})
	if err != nil {
		return nil, err
	}
	id, _, err := requireID(ctx, client, pos, "app network")
	if err != nil {
		return nil, err
	}
	if !*yes {
		ok, e := confirm(os.Stdin, fmt.Sprintf("Continue for app %s? [y/N] ", id))
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, ErrCancelled
		}
	}
	if purge {
		return nil, client.Apps.Network().PurgeCache(ctx, id, nil)
	}
	return client.Apps.Network().Unpublish(ctx, id)
}

func fileIDPath(ctx context.Context, client *vertracloud.Client, pos []string, app, name string) (string, string, error) {
	if len(pos) != 1 {
		return "", "", fmt.Errorf("%s requires one path", name)
	}
	id, err := resolveAppID(ctx, client, app, name)
	if err != nil {
		return "", "", err
	}
	return id, pos[0], nil
}

// buildErrorHint is the short "what to do" for an application's last_build_error code
// (empty for APP_DOWN or unknown codes), in the CLI language.
func buildErrorHint(code string) string {
	hints := map[string]map[string]string{
		"INSTALL_OUT_OF_MEMORY":  {"pt": "aumente a memória do app", "en": "raise the app memory", "es": "aumenta la memoria de la app"},
		"BUILD_FAILED":           {"pt": "leia o log e corrija o projeto", "en": "read the log and fix the project", "es": "lee el log y corrige el proyecto"},
		"INSTALL_FAILED":         {"pt": "leia o log e corrija o projeto", "en": "read the log and fix the project", "es": "lee el log y corrige el proyecto"},
		"BUILD_TIMEOUT":          {"pt": "reduza o tempo do build", "en": "make the build shorter", "es": "reduce el tiempo del build"},
		"INSTALL_TIMEOUT":        {"pt": "reduza o tempo da instalação", "en": "make the install shorter", "es": "reduce el tiempo de la instalación"},
		"BUILD_INTERRUPTED":      {"pt": "tente de novo", "en": "try again", "es": "inténtalo de nuevo"},
		"INSTALL_SHIELD_BLOCKED": {"pt": "o tráfego foi bloqueado pela proteção de rede durante o build", "en": "network protection blocked traffic during the build", "es": "la protección de red bloqueó el tráfico durante el build"},
	}
	h := hints[code]
	if h == nil {
		return ""
	}
	return ": " + h[ui.Locale]
}

// spin shows progress on stderr for slow transfers; JSON runs stay silent.
func spin(jsonMode bool, action string) func() {
	if jsonMode {
		return func() {}
	}
	labels := map[string]map[string]string{
		"upload": {"pt": "Compactando e enviando a aplicação…", "en": "Packing and uploading the application…", "es": "Comprimiendo y subiendo la aplicación…"},
		"build":  {"pt": "Construindo a aplicação…", "en": "Building the application…", "es": "Construyendo la aplicación…"},
		"commit": {"pt": "Compactando e enviando os arquivos…", "en": "Packing and uploading files…", "es": "Comprimiendo y subiendo los archivos…"},
	}
	return ui.Spin(labels[action][ui.Locale])
}

func withoutYes(args []string) []string {
	kept := make([]string, 0, len(args))
	for _, arg := range args {
		if arg != "--yes" && arg != "-y" {
			kept = append(kept, arg)
		}
	}
	return kept
}

func projectID() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	cfg, err := local.ReadProject(cwd)
	if err != nil || cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg["ID"])
}
func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// commitDir is where git is asked for the last commit: the --file folder when
// one is given, otherwise the current directory (the one sourceReader zips).
func commitDir(file string) string {
	if file != "" {
		if st, err := os.Stat(file); err == nil && st.IsDir() {
			return file
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

// gitSubject returns the subject of the last commit of the repository that
// contains dir. Any failure (no git, not a repo, no commits) yields "".
func gitSubject(ctx context.Context, dir string) string {
	if dir == "" {
		return ""
	}
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "log", "-1", "--format=%s").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// normalizeMessage trims a commit message and keeps at most 500 characters,
// the limit the API accepts.
func normalizeMessage(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 500 {
		return string(r[:500])
	}
	return s
}

func sourceReader(file string) (io.ReadCloser, string, error) {
	if file != "" {
		f, err := os.Open(file)
		return f, filepath.Base(file), err
	}
	tmp, err := os.CreateTemp("", "vertra-app-*.zip")
	if err != nil {
		return nil, "", err
	}
	root, err := os.Getwd()
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, "", err
	}
	name := filepath.Base(root) + ".zip"
	if err := local.Zip(root, tmp.Name()); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, "", err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, "", err
	}
	return &removeOnClose{File: tmp}, name, nil
}

type removeOnClose struct{ *os.File }

func (r *removeOnClose) Close() error {
	name := r.Name()
	err := r.File.Close()
	_ = os.Remove(name)
	return err
}
func envPairs(pairs []string) ([]vertracloud.ApplicationEnvironmentInput, error) {
	out := make([]vertracloud.ApplicationEnvironmentInput, 0, len(pairs))
	for _, pair := range pairs {
		k, v, ok := strings.Cut(pair, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid environment %q", pair)
		}
		out = append(out, vertracloud.ApplicationEnvironmentInput{Key: k, Value: v})
	}
	return out, nil
}
func readDotEnv(path string) ([]vertracloud.ApplicationEnvironmentInput, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pairs []string
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pairs = append(pairs, line)
	}
	return envPairs(pairs)
}
