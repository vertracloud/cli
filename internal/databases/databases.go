package databases

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// Run dispatches the token immediately after "db" (for example, "list" or
// "credentials") and returns values that the root command can JSON encode.
// A missing database ID resolves from the local project or an interactive picker.
func Run(ctx context.Context, client *vertracloud.Client, command string, args []string) (any, error) {
	if client == nil {
		return nil, errors.New("database client is nil")
	}
	switch command {
	case "list", "ls":
		return list(ctx, client, args)
	case "create":
		return create(ctx, client, args)
	case "info", "show":
		return get(ctx, client, args)
	case "update":
		return update(ctx, client, args)
	case "delete", "remove":
		return remove(ctx, client, args)
	case "start", "stop":
		return lifecycle(ctx, client, command, args)
	case "reset":
		return reset(ctx, client, args)
	case "status":
		return status(ctx, client, args)
	case "metrics":
		return metrics(ctx, client, args)
	case "credentials":
		return credentials(ctx, client, args)
	default:
		return nil, fmt.Errorf("unknown db command %q", command)
	}
}

func flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

func requireID(fs *flag.FlagSet) (string, error) {
	if fs.NArg() > 1 {
		return "", fmt.Errorf("unexpected argument %q", fs.Arg(1))
	}
	if fs.NArg() == 1 && strings.TrimSpace(fs.Arg(0)) != "" {
		return fs.Arg(0), nil
	}
	if id, err := projectDatabaseID(); err == nil && id != "" {
		return id, nil
	} else if err != nil {
		return "", err
	}
	return "", errors.New("database id is required")
}

func resolveID(ctx context.Context, client *vertracloud.Client, fs *flag.FlagSet) (string, error) {
	id, err := requireID(fs)
	if err == nil || err.Error() != "database id is required" || !ui.IsInteractive() {
		return id, err
	}
	return pickDatabase(ctx, client)
}

func pickDatabase(ctx context.Context, client *vertracloud.Client) (string, error) {
	account, err := client.Account.Get(ctx)
	if err != nil {
		return "", err
	}
	choices := make([]ui.Choice, 0, len(account.Databases))
	for _, db := range account.Databases {
		choices = append(choices, ui.Choice{ID: db.ID, Label: db.Name, Hint: db.ID})
	}
	return ui.PickID("Select database", choices)
}

func rejectExtra(fs *flag.FlagSet) error {
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return nil
}

// intermixFlags lets flags appear after positional arguments; flag.FlagSet
// itself stops parsing when it reaches the first positional argument.
func intermixFlags(args []string, valueFlags, boolFlags map[string]bool) []string {
	options := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		name := arg
		if index := strings.IndexByte(name, '='); index >= 0 {
			name = name[:index]
		}
		if boolFlags[name] {
			options = append(options, arg)
			continue
		}
		if valueFlags[name] {
			options = append(options, arg)
			if !strings.Contains(arg, "=") && i+1 < len(args) {
				i++
				options = append(options, args[i])
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			options = append(options, arg)
			continue
		}
		positionals = append(positionals, arg)
	}
	return append(options, positionals...)
}

func projectDatabaseID() (string, error) {
	data, err := os.ReadFile("vertracloud.config")
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read vertracloud.config: %w", err)
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		index := strings.IndexByte(line, '=')
		if index < 0 || !strings.EqualFold(strings.TrimSpace(line[:index]), "ID") {
			continue
		}
		if id := strings.TrimSpace(line[index+1:]); id != "" {
			return id, nil
		}
	}
	return "", nil
}

func parsePositive(value, name string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return n, nil
}

func dbType(value string) (vertracloud.DatabaseType, error) {
	switch strings.ToLower(value) {
	case "postgres":
		return vertracloud.DatabaseTypePostgreSQL, nil
	case "mongo":
		return vertracloud.DatabaseTypeMongoDB, nil
	case "redis":
		return vertracloud.DatabaseTypeRedis, nil
	case "mysql":
		return vertracloud.DatabaseTypeMySQL, nil
	default:
		return 0, fmt.Errorf("invalid database type %q (use postgres, mongo, redis, or mysql)", value)
	}
}

func list(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	fs := flags("db list")
	var filters stringList
	fs.Var(&filters, "filter", "filter expression (repeatable)")
	fs.String("page-size", "5", "page size")
	workspaceID := fs.String("workspace", "", "list databases in a workspace")
	all := fs.Bool("all", false, "return all databases")
	fs.BoolVar(all, "A", false, "return all databases")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if err := rejectExtra(fs); err != nil {
		return nil, err
	}
	// Like the dashboard, live state comes from a second request that runs in
	// parallel; the list still renders if it fails.
	type liveResult struct {
		items []vertracloud.DatabaseStatusShort
		err   error
	}
	liveCh := make(chan liveResult, 1)
	go func() {
		var opts []rest.RequestOpt
		if *workspaceID != "" {
			opts = append(opts, rest.WithWorkspaceID(*workspaceID))
		}
		items, err := client.Databases.StatusAll(ctx, opts...)
		liveCh <- liveResult{items, err}
	}()
	var databases []vertracloud.Database
	var organization vertracloud.WorkspaceResourceOrganization
	if *workspaceID == "" {
		info, err := client.Account.Get(ctx)
		if err != nil {
			return nil, err
		}
		databases, organization = info.Databases, info.ResourceOrganization
	} else {
		workspace, err := client.Workspaces.Get(ctx, *workspaceID)
		if err != nil {
			return nil, err
		}
		databases, organization = workspace.Databases, workspace.ResourceOrganization
	}
	filtered := make([]vertracloud.Database, 0, len(databases))
	for _, database := range databases {
		match, err := matches(database, filters)
		if err != nil {
			return nil, err
		}
		if match {
			filtered = append(filtered, database)
		}
	}
	rows := presentDatabases(filtered, organization)
	if live := <-liveCh; live.err == nil {
		byID := make(map[string]vertracloud.DatabaseStatusShort, len(live.items))
		for _, item := range live.items {
			byID[item.ID] = item
		}
		for i, database := range filtered {
			if item, ok := byID[database.ID]; ok {
				rows[i]["status"] = map[bool]string{true: "up", false: "down"}[item.Running]
				rows[i]["cpu"], rows[i]["ram_used"] = item.CPU, item.RAM
			}
		}
	}
	return ui.Presentation{JSON: filtered, Human: rows}, nil
}

func presentDatabases(databases []vertracloud.Database, organization vertracloud.WorkspaceResourceOrganization) []map[string]any {
	encoded, _ := json.Marshal(databases)
	var rows []map[string]any
	_ = json.Unmarshal(encoded, &rows)
	folders := make(map[string][]string)
	colors := make(map[string]string)
	for _, folder := range organization.Folders {
		for _, resource := range folder.Resources {
			if colors[resource.ResourceID] == "" {
				colors[resource.ResourceID] = string(folder.Color)
			}
			if resource.ResourceType == vertracloud.WorkspaceResourceTypeDatabase {
				folders[resource.ResourceID] = append(folders[resource.ResourceID], folder.Name)
			}
		}
	}
	favorites := make(map[string]bool)
	for _, favorite := range organization.Favorites {
		if favorite.ResourceType == vertracloud.WorkspaceResourceTypeDatabase {
			favorites[favorite.ResourceID] = true
		}
	}
	for i, database := range databases {
		rows[i]["folder"] = strings.Join(folders[database.ID], ", ")
		rows[i]["favorite"] = favorites[database.ID]
		rows[i]["folder_color"] = colors[database.ID]
	}
	return rows
}

func create(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	fs := flags("db create")
	name := fs.String("name", "", "database name")
	memory := fs.String("memory", "", "memory in MB")
	typeName := fs.String("type", "", "database type")
	description := fs.String("description", "", "database description")
	workspace := fs.String("workspace", "", "workspace id")
	snapshot := fs.String("snapshot", "", "snapshot id")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if err := rejectExtra(fs); err != nil {
		return nil, err
	}
	if *name == "" || *memory == "" || *typeName == "" {
		return nil, errors.New("--name, --memory, and --type are required")
	}
	ram, err := parsePositive(*memory, "--memory")
	if err != nil {
		return nil, err
	}
	typeID, err := dbType(*typeName)
	if err != nil {
		return nil, err
	}
	params := vertracloud.DatabaseCreateBody{Name: *name, RAM: ram, Type: &typeID, WorkspaceID: *workspace, SnapshotID: *snapshot}
	if *description != "" {
		params.Description = vertracloud.NullableValue(*description)
	}
	return client.Databases.Create(ctx, params)
}

func get(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	fs := flags("db info")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	id, err := resolveID(ctx, client, fs)
	if err != nil {
		return nil, err
	}
	return client.Databases.Get(ctx, id)
}

func update(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	fs := flags("db update")
	name := fs.String("name", "", "new database name")
	description := fs.String("description", "", "new database description")
	ramText := fs.String("ram", "", "new memory in MB")
	args = intermixFlags(args, map[string]bool{"--name": true, "--description": true, "--ram": true}, nil)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	id, err := resolveID(ctx, client, fs)
	if err != nil {
		return nil, err
	}
	body := vertracloud.DatabaseUpdateBody{}
	set := false
	if *name != "" {
		body.Name = name
		set = true
	}
	if *description != "" {
		body.Description = vertracloud.NullableValue(*description)
		set = true
	}
	if *ramText != "" {
		ram, parseErr := parsePositive(*ramText, "--ram")
		if parseErr != nil {
			return nil, parseErr
		}
		body.RAM = &ram
		set = true
	}
	if !set {
		return nil, errors.New("at least one of --name, --description, or --ram is required")
	}
	return client.Databases.Update(ctx, id, body)
}

func remove(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	fs := flags("db delete")
	yes := fs.Bool("yes", false, "confirm deletion")
	fs.BoolVar(yes, "y", false, "confirm deletion")
	args = intermixFlags(args, nil, map[string]bool{"--yes": true, "-y": true})
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	id, err := resolveID(ctx, client, fs)
	if err != nil {
		return nil, err
	}
	if !*yes {
		return nil, errors.New("db delete requires --yes")
	}
	if _, err := client.Databases.Delete(ctx, id); err != nil {
		return nil, err
	}
	return map[string]string{"id": id}, nil
}

func lifecycle(ctx context.Context, client *vertracloud.Client, action string, args []string) (any, error) {
	fs := flags("db " + action)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	id, err := resolveID(ctx, client, fs)
	if err != nil {
		return nil, err
	}
	if action == "start" {
		_, err = client.Databases.Start(ctx, id)
	} else {
		_, err = client.Databases.Stop(ctx, id)
	}
	if err != nil {
		return nil, err
	}
	return map[string]string{"id": id, "action": action}, nil
}

func reset(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	fs := flags("db reset")
	yes := fs.Bool("yes", false, "confirm reset")
	fs.BoolVar(yes, "y", false, "confirm reset")
	args = intermixFlags(args, nil, map[string]bool{"--yes": true, "-y": true})
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	id, err := resolveID(ctx, client, fs)
	if err != nil {
		return nil, err
	}
	if !*yes {
		return nil, errors.New("db reset requires --yes")
	}
	if _, err := client.Databases.Reset(ctx, id); err != nil {
		return nil, err
	}
	return map[string]string{"id": id}, nil
}

func status(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	fs := flags("db status")
	all := fs.Bool("all", false, "return status for all databases")
	args = intermixFlags(args, nil, map[string]bool{"--all": true})
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if *all {
		if fs.NArg() > 1 {
			return nil, fmt.Errorf("unexpected argument %q", fs.Arg(1))
		}
		// The SDK returns one aggregate status object here, following the REST
		// contract, so preserve that shape verbatim.
		accountCh := make(chan vertracloud.AccountInfo, 1)
		go func() {
			account, _ := client.Account.Get(ctx)
			accountCh <- account
		}()
		items, err := client.Databases.StatusAll(ctx)
		if err != nil {
			return nil, err
		}
		resources := map[string]ui.Resource{}
		for _, database := range (<-accountCh).Databases {
			resources[database.ID] = ui.Resource{Name: database.Name, RAM: database.RAM}
		}
		return ui.WithResources(items, resources), nil
	}
	id, err := resolveID(ctx, client, fs)
	if err != nil {
		return nil, err
	}
	// The status route has no name or RAM limit; fetch the database alongside it.
	databaseCh := make(chan vertracloud.Database, 1)
	go func() {
		database, _ := client.Databases.Get(ctx, id)
		databaseCh <- database
	}()
	item, err := client.Databases.Status(ctx, id)
	if err != nil {
		return nil, err
	}
	database := <-databaseCh
	return ui.WithResources(item, map[string]ui.Resource{database.ID: {Name: database.Name, RAM: database.RAM}}), nil
}

func metrics(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	fs := flags("db metrics")
	fs.Bool("table", false, "render as a table")
	all := fs.Bool("all", false, "include all chart points")
	fs.BoolVar(all, "A", false, "include all chart points")
	args = intermixFlags(args, nil, map[string]bool{"--table": true, "--all": true, "-A": true})
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	id, err := resolveID(ctx, client, fs)
	if err != nil {
		return nil, err
	}
	return client.Databases.Metrics(ctx, id)
}

func credentials(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	if len(args) == 0 {
		return nil, errors.New("missing credentials command")
	}
	switch args[0] {
	case "certificate", "cert":
		return certificate(ctx, client, args[1:])
	case "reset":
		return credentialsReset(ctx, client, args[1:])
	default:
		return nil, fmt.Errorf("unknown db credentials command %q", args[0])
	}
}

func certificate(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	fs := flags("db credentials certificate")
	output := fs.String("output", "", "write certificate JSON to a file")
	fs.StringVar(output, "o", "", "write certificate JSON to a file")
	args = intermixFlags(args, map[string]bool{"--output": true, "-o": true}, nil)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	id, err := resolveID(ctx, client, fs)
	if err != nil {
		return nil, err
	}
	certificate, err := client.Databases.Credentials().Certificate().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if *output == "" {
		return certificate, nil
	}
	data, err := json.MarshalIndent(certificate, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(*output, append(data, '\n'), 0600); err != nil {
		return nil, fmt.Errorf("write certificate: %w", err)
	}
	return map[string]string{"file": *output}, nil
}

func credentialsReset(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	fs := flags("db credentials reset")
	yes := fs.Bool("yes", false, "confirm credential reset")
	fs.BoolVar(yes, "y", false, "confirm credential reset")
	args = intermixFlags(args, nil, map[string]bool{"--yes": true, "-y": true})
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return nil, errors.New("credential target and optional database id are required")
	}
	target := fs.Arg(0)
	id := ""
	if fs.NArg() == 2 {
		id = fs.Arg(1)
	}
	if id == "" {
		var err error
		id, err = projectDatabaseID()
		if err != nil {
			return nil, err
		}
		if id == "" {
			if !ui.IsInteractive() {
				return nil, errors.New("database id is required")
			}
			id, err = pickDatabase(ctx, client)
			if err != nil {
				return nil, err
			}
		}
	}
	if target != "password" && target != "certificate" {
		return nil, fmt.Errorf("invalid credential reset target %q", target)
	}
	if !*yes {
		return nil, errors.New("db credentials reset requires --yes")
	}
	if target == "password" {
		return client.Databases.Credentials().Password().Reset(ctx, id)
	}
	certificate, err := client.Databases.Credentials().Certificate().Reset(ctx, id)
	if err != nil {
		return nil, err
	}
	return certificate, nil
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("filter cannot be empty")
	}
	*s = append(*s, value)
	return nil
}

func matches(database vertracloud.Database, filters []string) (bool, error) {
	for _, expression := range filters {
		key, operator, value, err := splitFilter(expression)
		if err != nil {
			return false, err
		}
		if key != "name" && key != "status" && key != "type" && key != "ram" {
			return false, fmt.Errorf("invalid filter key %q (use name, status, type, or ram)", key)
		}
		if key != "ram" && operator != "=" {
			return false, fmt.Errorf("invalid filter value %q", expression)
		}
		if key == "ram" {
			if _, err := strconv.ParseFloat(value, 64); err != nil {
				return false, fmt.Errorf("invalid filter value %q", expression)
			}
		}
		if !matchFilter(database, key, operator, value) {
			return false, nil
		}
	}
	return true, nil
}

func splitFilter(expression string) (string, string, string, error) {
	expression = strings.TrimSpace(expression)
	for _, operator := range []string{">=", "<=", ">", "<", "="} {
		if index := strings.Index(expression, operator); index >= 1 {
			key := strings.ToLower(strings.TrimSpace(expression[:index]))
			value := strings.TrimSpace(expression[index+len(operator):])
			if key == "" {
				break
			}
			return key, operator, value, nil
		}
	}
	return "", "", "", fmt.Errorf("invalid filter %q", expression)
}

func matchFilter(database vertracloud.Database, key, operator, value string) bool {
	if key == "name" {
		return compareText(database.Name, operator, value, true)
	}
	if key == "status" {
		return compareText(string(database.Status), operator, value, false)
	}
	if key == "type" {
		return compareText(databaseTypeName(database.Type), operator, value, false)
	}
	if key == "ram" {
		wanted, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return false
		}
		switch operator {
		case "=":
			return float64(database.RAM) == wanted
		case ">":
			return float64(database.RAM) > wanted
		case ">=":
			return float64(database.RAM) >= wanted
		case "<":
			return float64(database.RAM) < wanted
		case "<=":
			return float64(database.RAM) <= wanted
		}
	}
	return false
}

func compareText(actual, operator, expected string, substring bool) bool {
	actual = strings.ToLower(actual)
	expected = strings.ToLower(expected)
	switch operator {
	case "=":
		if substring {
			return strings.Contains(actual, expected)
		}
		return actual == expected
	default:
		return false
	}
}

func databaseTypeName(databaseType vertracloud.DatabaseType) string {
	switch databaseType {
	case vertracloud.DatabaseTypePostgreSQL:
		return "postgres"
	case vertracloud.DatabaseTypeMongoDB:
		return "mongo"
	case vertracloud.DatabaseTypeRedis:
		return "redis"
	case vertracloud.DatabaseTypeMySQL:
		return "mysql"
	default:
		return "unknown"
	}
}
