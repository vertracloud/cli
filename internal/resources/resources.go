package resources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/vertracloud/cli/internal/local"
	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// Run dispatches resource-oriented commands. All network calls go through the
// typed SDK; this package intentionally has no HTTP client of its own.
func Run(ctx context.Context, client *vertracloud.Client, group string, args []string) (any, error) {
	if client == nil {
		return nil, errors.New("nil SDK client")
	}
	switch group {
	case "workspace", "ws":
		return workspace(ctx, client, args)
	case "snapshot", "snapshots":
		return snapshot(ctx, client, args)
	case "folder":
		return folder(ctx, client, args)
	case "favorite", "fav":
		return favorite(ctx, client, args)
	default:
		return nil, fmt.Errorf("unknown resource group %q", group)
	}
}

type options struct {
	values map[string]string
	yes    bool
}

func parse(args []string) ([]string, options, error) {
	pos := make([]string, 0, len(args))
	opts := options{values: map[string]string{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-y" || a == "--yes" {
			opts.yes = true
			continue
		}
		if strings.HasPrefix(a, "--") {
			name, value, found := strings.Cut(strings.TrimPrefix(a, "--"), "=")
			if !found && (name == "db" || name == "download") {
				opts.values[name] = "true"
				continue
			}
			if !found {
				if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					i++
					value = args[i]
				} else {
					value = "true"
				}
			}
			switch name {
			case "db", "download":
				if found || value == "true" {
					opts.values[name] = value
					continue
				}
			case "description", "name", "color", "workspace", "permissions", "output", "to", "status", "snapshot":
				if value == "true" {
					return nil, options{}, fmt.Errorf("--%s requires a value", name)
				}
				opts.values[name] = value
				continue
			default:
				return nil, options{}, fmt.Errorf("unknown option %q", a)
			}
			return nil, options{}, fmt.Errorf("invalid option %q", a)
		}
		if strings.HasPrefix(a, "-") {
			if a == "-o" {
				if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
					return nil, options{}, errors.New("-o requires a value")
				}
				i++
				opts.values["output"] = args[i]
				continue
			}
			return nil, options{}, fmt.Errorf("unknown option %q", a)
		}
		pos = append(pos, a)
	}
	return pos, opts, nil
}

func requireYes(opts options) error {
	if !opts.yes {
		return errors.New("destructive command requires --yes")
	}
	return nil
}

func exact(pos []string, n int, usage string) error {
	if len(pos) != n {
		return fmt.Errorf("usage: %s", usage)
	}
	return nil
}

func ptr(s string) *string { return &s }

func parseType(value string) (vertracloud.WorkspaceResourceType, error) {
	switch value {
	case "application":
		return vertracloud.WorkspaceResourceTypeApplication, nil
	case "database":
		return vertracloud.WorkspaceResourceTypeDatabase, nil
	default:
		return "", fmt.Errorf("invalid resource type %q (use application or database)", value)
	}
}

func parseColor(value string) (vertracloud.WorkspaceFolderColor, error) {
	switch value {
	case "neutral":
		return vertracloud.WorkspaceFolderColorNeutral, nil
	case "red":
		return vertracloud.WorkspaceFolderColorRed, nil
	case "orange":
		return vertracloud.WorkspaceFolderColorOrange, nil
	case "yellow":
		return vertracloud.WorkspaceFolderColorYellow, nil
	case "green":
		return vertracloud.WorkspaceFolderColorGreen, nil
	case "blue":
		return vertracloud.WorkspaceFolderColorBlue, nil
	case "purple":
		return vertracloud.WorkspaceFolderColorPurple, nil
	default:
		return "", fmt.Errorf("invalid color %q", value)
	}
}

func workspace(ctx context.Context, c *vertracloud.Client, args []string) (any, error) {
	pos, opts, err := parse(args)
	if err != nil {
		return nil, err
	}
	if len(pos) == 0 {
		return nil, errors.New("missing workspace command")
	}
	switch pos[0] {
	case "list", "ls":
		if len(pos) != 1 {
			return nil, errors.New("usage: workspace list")
		}
		return c.Workspaces.List(ctx)
	case "create":
		if err := exact(pos, 2, "workspace create <name>"); err != nil {
			return nil, err
		}
		body := vertracloud.WorkspaceCreateBody{Name: pos[1]}
		if v, ok := opts.values["description"]; ok {
			body.Description = ptr(v)
		}
		return c.Workspaces.Create(ctx, body)
	case "info":
		id, err := resolveWorkspace(ctx, c, pos[1:])
		if err != nil {
			return nil, err
		}
		return c.Workspaces.Get(ctx, id)
	case "update":
		id, err := resolveWorkspace(ctx, c, pos[1:])
		if err != nil {
			return nil, err
		}
		name, hasName := opts.values["name"]
		description, hasDescription := opts.values["description"]
		if !hasName && !hasDescription {
			return nil, errors.New("workspace update requires --name or --description")
		}
		body := vertracloud.WorkspaceUpdateBody{}
		if hasName {
			body.Name = ptr(name)
		}
		if hasDescription {
			body.Description = ptr(description)
		}
		return c.Workspaces.Update(ctx, id, body)
	case "delete":
		id, err := resolveWorkspace(ctx, c, pos[1:])
		if err != nil {
			return nil, err
		}
		if err := requireYes(opts); err != nil {
			return nil, err
		}
		if err := c.Workspaces.Delete(ctx, id); err != nil {
			return nil, err
		}
		return map[string]string{"id": id}, nil
	case "invite":
		return workspaceInvite(ctx, c, pos[1:], opts)
	case "action-request", "request":
		return workspaceActionRequest(ctx, c, pos[1:], opts)
	case "role":
		return workspaceRole(ctx, c, pos[1:], opts)
	case "member":
		return workspaceMember(ctx, c, pos[1:], opts)
	case "app":
		return workspaceApp(ctx, c, pos[1:], opts)
	case "db", "database":
		return workspaceDB(ctx, c, pos[1:], opts)
	default:
		return nil, fmt.Errorf("unknown workspace command %q", pos[0])
	}
}

func workspaceInvite(ctx context.Context, c *vertracloud.Client, pos []string, opts options) (any, error) {
	if len(pos) == 0 {
		return nil, errors.New("missing workspace invite command")
	}
	switch pos[0] {
	case "list":
		if err := exact(pos, 2, "workspace invite list <workspaceId>"); err != nil {
			return nil, err
		}
		return c.Workspaces.Invites().List(ctx, pos[1])
	case "revoke":
		if err := exact(pos, 3, "workspace invite revoke <workspaceId> <inviteId> --yes"); err != nil {
			return nil, err
		}
		if err := requireYes(opts); err != nil {
			return nil, err
		}
		if err := c.Workspaces.Invites().Delete(ctx, pos[1], pos[2]); err != nil {
			return nil, err
		}
		return map[string]string{"id": pos[2]}, nil
	case "preview":
		if err := exact(pos, 2, "workspace invite preview <token>"); err != nil {
			return nil, err
		}
		return c.Workspaces.Invites().Preview(ctx, pos[1])
	case "accept":
		if err := exact(pos, 2, "workspace invite accept <token>"); err != nil {
			return nil, err
		}
		return c.Workspaces.Invites().Accept(ctx, pos[1])
	case "decline":
		if err := exact(pos, 2, "workspace invite decline <token> --yes"); err != nil {
			return nil, err
		}
		if err := requireYes(opts); err != nil {
			return nil, err
		}
		if err := c.Workspaces.Invites().Decline(ctx, pos[1]); err != nil {
			return nil, err
		}
		return map[string]string{"status": "declined"}, nil
	default:
		return nil, fmt.Errorf("unknown workspace invite command %q", pos[0])
	}
}

func workspaceActionRequest(ctx context.Context, c *vertracloud.Client, pos []string, opts options) (any, error) {
	if len(pos) == 0 {
		return nil, errors.New("missing workspace action-request command")
	}
	switch pos[0] {
	case "list", "ls":
		if err := exact(pos, 2, "workspace action-request list <workspaceId> [--status pending|approved|rejected|expired]"); err != nil {
			return nil, err
		}
		params := &vertracloud.WorkspaceActionRequestListParams{}
		if raw := opts.values["status"]; raw != "" {
			switch status := vertracloud.WorkspaceActionRequestStatus(raw); status {
			case vertracloud.WorkspaceActionRequestStatusPending, vertracloud.WorkspaceActionRequestStatusApproved,
				vertracloud.WorkspaceActionRequestStatusRejected, vertracloud.WorkspaceActionRequestStatusExpired:
				params.Status = status
			default:
				return nil, fmt.Errorf("invalid --status %q (use pending, approved, rejected or expired)", raw)
			}
		}
		return c.Workspaces.ActionRequests().List(ctx, pos[1], params)
	case "create":
		const usage = "workspace action-request create <workspaceId> <app_delete|database_delete|snapshot_create|snapshot_restore> <resourceId> [--snapshot <snapshotId>]"
		if err := exact(pos, 4, usage); err != nil {
			return nil, err
		}
		body := vertracloud.WorkspaceActionRequestCreateBody{Action: vertracloud.WorkspaceActionRequestAction(pos[2]), ResourceID: pos[3]}
		snapshotID := opts.values["snapshot"]
		switch body.Action {
		case vertracloud.WorkspaceActionRequestActionAppDelete, vertracloud.WorkspaceActionRequestActionDatabaseDelete, vertracloud.WorkspaceActionRequestActionSnapshotCreate:
			if snapshotID != "" {
				return nil, fmt.Errorf("--snapshot is only valid for %s", vertracloud.WorkspaceActionRequestActionSnapshotRestore)
			}
		case vertracloud.WorkspaceActionRequestActionSnapshotRestore:
			if snapshotID == "" {
				return nil, errors.New("snapshot_restore requires --snapshot <snapshotId>")
			}
			body.Params = vertracloud.WorkspaceActionRequestSnapshotRestoreParams{SnapshotID: snapshotID}
		default:
			return nil, fmt.Errorf("invalid action %q; usage: %s", pos[2], usage)
		}
		return c.Workspaces.ActionRequests().Create(ctx, pos[1], body)
	default:
		return nil, fmt.Errorf("unknown workspace action-request command %q", pos[0])
	}
}

func resolveWorkspace(ctx context.Context, c *vertracloud.Client, pos []string) (string, error) {
	if len(pos) > 1 {
		return "", errors.New("workspace accepts at most one id")
	}
	if len(pos) == 1 {
		return pos[0], nil
	}
	items, err := c.Workspaces.List(ctx)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", errors.New("no workspaces available")
	}
	if len(items) > 1 {
		if !ui.IsInteractive() {
			return "", errors.New("workspace id is required when more than one workspace exists")
		}
		choices := make([]ui.Choice, 0, len(items))
		for _, item := range items {
			choices = append(choices, ui.Choice{ID: item.ID, Label: item.Name, Hint: item.ID})
		}
		return ui.PickID("Select workspace", choices)
	}
	return items[0].ID, nil
}

func workspaceRole(ctx context.Context, c *vertracloud.Client, pos []string, opts options) (any, error) {
	if len(pos) == 0 {
		return nil, errors.New("missing workspace role command")
	}
	switch pos[0] {
	case "list":
		if err := exact(pos, 2, "workspace role list <workspaceId>"); err != nil {
			return nil, err
		}
		return c.Workspaces.Roles().List(ctx, pos[1])
	case "create":
		if err := exact(pos, 3, "workspace role create <workspaceId> <name> --permissions <a,b>"); err != nil {
			return nil, err
		}
		permissions, ok := opts.values["permissions"]
		if !ok {
			return nil, errors.New("workspace role create requires --permissions")
		}
		return c.Workspaces.Roles().Create(ctx, pos[1], vertracloud.WorkspaceRoleBody{Name: pos[2], Permissions: splitCSV(permissions)})
	case "update":
		if err := exact(pos, 3, "workspace role update <workspaceId> <roleId>"); err != nil {
			return nil, err
		}
		if _, ok := opts.values["name"]; !ok {
			if _, ok := opts.values["permissions"]; !ok {
				return nil, errors.New("workspace role update requires --name or --permissions")
			}
		}
		roles, err := c.Workspaces.Roles().List(ctx, pos[1])
		if err != nil {
			return nil, err
		}
		var current *vertracloud.WorkspaceRole
		for i := range roles {
			if roles[i].ID == pos[2] {
				current = &roles[i]
				break
			}
		}
		if current == nil {
			return nil, fmt.Errorf("workspace role %q not found", pos[2])
		}
		name := current.Name
		if v, ok := opts.values["name"]; ok {
			name = v
		}
		permissions := current.Permissions
		if v, ok := opts.values["permissions"]; ok {
			permissions = workspacePermissions(v)
		}
		return c.Workspaces.Roles().Update(ctx, pos[1], pos[2], vertracloud.WorkspaceRoleBody{Name: name, Permissions: permissions})
	case "delete":
		if err := exact(pos, 3, "workspace role delete <workspaceId> <roleId> --yes"); err != nil {
			return nil, err
		}
		if err := requireYes(opts); err != nil {
			return nil, err
		}
		if err := c.Workspaces.Roles().Delete(ctx, pos[1], pos[2]); err != nil {
			return nil, err
		}
		return map[string]string{"id": pos[2]}, nil
	default:
		return nil, fmt.Errorf("unknown workspace role command %q", pos[0])
	}
}

func workspaceMember(ctx context.Context, c *vertracloud.Client, pos []string, opts options) (any, error) {
	if len(pos) == 0 {
		return nil, errors.New("missing workspace member command")
	}
	switch pos[0] {
	case "list":
		if err := exact(pos, 2, "workspace member list <workspaceId>"); err != nil {
			return nil, err
		}
		return c.Workspaces.Members().List(ctx, pos[1])
	case "update":
		if err := exact(pos, 4, "workspace member update <workspaceId> <userId> <roleId>"); err != nil {
			return nil, err
		}
		return c.Workspaces.Members().Update(ctx, pos[1], pos[2], vertracloud.WorkspaceMemberUpdateBody{RoleID: pos[3]})
	case "remove":
		if err := exact(pos, 3, "workspace member remove <workspaceId> <memberId> --yes"); err != nil {
			return nil, err
		}
		if err := requireYes(opts); err != nil {
			return nil, err
		}
		if err := c.Workspaces.Members().Remove(ctx, pos[1], pos[2]); err != nil {
			return nil, err
		}
		return map[string]string{"id": pos[2]}, nil
	default:
		return nil, fmt.Errorf("unknown workspace member command %q", pos[0])
	}
}

func workspaceApp(ctx context.Context, c *vertracloud.Client, pos []string, opts options) (any, error) {
	if err := exact(pos, 3, "workspace app <add|remove> <workspaceId> <appId>"); err != nil {
		return nil, err
	}
	switch pos[0] {
	case "add":
		if err := c.Workspaces.Apps().Add(ctx, pos[1], pos[2]); err != nil {
			return nil, err
		}
		return map[string]string{"id": pos[2]}, nil
	case "remove":
		if err := c.Workspaces.Apps().Remove(ctx, pos[1], pos[2]); err != nil {
			return nil, err
		}
		return map[string]string{"id": pos[2]}, nil
	default:
		return nil, fmt.Errorf("unknown workspace app command %q", pos[0])
	}
}

func workspaceDB(ctx context.Context, c *vertracloud.Client, pos []string, opts options) (any, error) {
	if err := exact(pos, 3, "workspace db <add|remove> <workspaceId> <dbId>"); err != nil {
		return nil, err
	}
	switch pos[0] {
	case "add":
		if err := c.Workspaces.Databases().Add(ctx, pos[1], pos[2]); err != nil {
			return nil, err
		}
		return map[string]string{"id": pos[2]}, nil
	case "remove":
		if err := c.Workspaces.Databases().Remove(ctx, pos[1], pos[2]); err != nil {
			return nil, err
		}
		return map[string]string{"id": pos[2]}, nil
	default:
		return nil, fmt.Errorf("unknown workspace db command %q", pos[0])
	}
}

func splitCSV(value string) []vertracloud.WorkspacePermission {
	parts := strings.Split(value, ",")
	result := make([]vertracloud.WorkspacePermission, 0, len(parts))
	for _, part := range parts {
		if p := strings.TrimSpace(part); p != "" {
			result = append(result, vertracloud.WorkspacePermission(p))
		}
	}
	return result
}

func workspacePermissions(value string) []vertracloud.WorkspacePermission { return splitCSV(value) }

func snapshot(ctx context.Context, c *vertracloud.Client, args []string) (any, error) {
	pos, opts, err := parse(args)
	if err != nil {
		return nil, err
	}
	if len(pos) == 0 {
		return nil, errors.New("missing snapshot command")
	}
	scope := vertracloud.SnapshotScopeApplications
	if _, ok := opts.values["db"]; ok {
		scope = vertracloud.SnapshotScopeDatabases
	}
	switch pos[0] {
	case "list":
		if len(pos) > 2 {
			return nil, errors.New("usage: snapshot list [resourceId] [--db]")
		}
		if len(pos) == 1 {
			return c.Snapshots.ListAll(ctx, scope)
		}
		return c.Snapshots.List(ctx, pos[1], scope)
	case "create":
		if len(pos) > 2 {
			return nil, errors.New("usage: snapshot create [resourceId] [--db]")
		}
		id := ""
		if len(pos) == 2 {
			id = pos[1]
		} else {
			id, err = resolveSnapshotResource(ctx, c, scope)
			if err != nil {
				return nil, err
			}
		}
		snap, err := c.Snapshots.Create(ctx, id, scope)
		if err != nil {
			return nil, err
		}
		if _, download := opts.values["download"]; !download {
			return snap, nil
		}
		data, err := c.Snapshots.Download(ctx, id, snap.ID, scope)
		if err != nil {
			return nil, err
		}
		file := snap.ID + ".zip"
		if _, err := local.SaveStream(file, data); err != nil {
			return nil, err
		}
		return mapWith(snap, "downloaded_file", file)
	case "download":
		if err := exact(pos, 3, "snapshot download <resourceId> <snapshotId>"); err != nil {
			return nil, err
		}
		file := opts.values["output"]
		if file == "" {
			file = pos[2] + ".zip"
		}
		data, err := c.Snapshots.Download(ctx, pos[1], pos[2], scope)
		if err != nil {
			return nil, err
		}
		if _, err := local.SaveStream(file, data); err != nil {
			return nil, err
		}
		return map[string]string{"file": file}, nil
	case "restore":
		if err := exact(pos, 3, "snapshot restore <resourceId> <snapshotId> --yes"); err != nil {
			return nil, err
		}
		if err := requireYes(opts); err != nil {
			return nil, err
		}
		if target := opts.values["to"]; target != "" {
			return c.Snapshots.RestoreTo(ctx, pos[1], pos[2], scope, target)
		}
		return c.Snapshots.Restore(ctx, pos[1], pos[2], scope)
	default:
		return nil, fmt.Errorf("unknown snapshot command %q", pos[0])
	}
}

func resolveSnapshotResource(ctx context.Context, c *vertracloud.Client, scope vertracloud.SnapshotScope) (string, error) {
	account, err := c.Account.Get(ctx)
	if err != nil {
		return "", err
	}
	if scope == vertracloud.SnapshotScopeDatabases {
		if len(account.Databases) == 1 {
			return account.Databases[0].ID, nil
		}
		if len(account.Databases) == 0 || !ui.IsInteractive() {
			return "", errors.New("resource id is required unless exactly one database exists")
		}
		choices := make([]ui.Choice, 0, len(account.Databases))
		for _, db := range account.Databases {
			choices = append(choices, ui.Choice{ID: db.ID, Label: db.Name, Hint: db.ID})
		}
		return ui.PickID("Select database", choices)
	}
	if len(account.Applications) == 1 {
		return account.Applications[0].ID, nil
	}
	if len(account.Applications) == 0 || !ui.IsInteractive() {
		return "", errors.New("resource id is required unless exactly one application exists")
	}
	choices := make([]ui.Choice, 0, len(account.Applications))
	for _, app := range account.Applications {
		choices = append(choices, ui.Choice{ID: app.ID, Label: app.Name, Hint: app.ID})
	}
	return ui.PickID("Select application", choices)
}

func mapWith(value any, key, text string) (map[string]any, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, err
	}
	result[key] = text
	return result, nil
}

func organization(ctx context.Context, c *vertracloud.Client, workspaceID string) (vertracloud.WorkspaceResourceOrganization, error) {
	if workspaceID == "" {
		account, err := c.Account.Get(ctx)
		if err != nil {
			return vertracloud.WorkspaceResourceOrganization{}, err
		}
		return account.ResourceOrganization, nil
	}
	workspace, err := c.Workspaces.Get(ctx, workspaceID)
	if err != nil {
		return vertracloud.WorkspaceResourceOrganization{}, err
	}
	return workspace.ResourceOrganization, nil
}

func folder(ctx context.Context, c *vertracloud.Client, args []string) (any, error) {
	pos, opts, err := parse(args)
	if err != nil {
		return nil, err
	}
	if len(pos) == 0 {
		return nil, errors.New("missing folder command")
	}
	workspaceID := opts.values["workspace"]
	switch pos[0] {
	case "list":
		if err := exact(pos, 1, "folder list [--workspace <id>]"); err != nil {
			return nil, err
		}
		org, err := organization(ctx, c, workspaceID)
		if err != nil {
			return nil, err
		}
		return org.Folders, nil
	case "create":
		if err := exact(pos, 2, "folder create <name>"); err != nil {
			return nil, err
		}
		body := vertracloud.ResourceFolderCreateBody{Name: pos[1]}
		if raw := opts.values["color"]; raw != "" {
			body.Color, err = parseColor(raw)
			if err != nil {
				return nil, err
			}
		}
		if workspaceID != "" {
			return c.Workspaces.Folders().Create(ctx, workspaceID, body)
		}
		return c.Account.Folders().Create(ctx, body)
	case "update":
		if err := exact(pos, 2, "folder update <folderId>"); err != nil {
			return nil, err
		}
		name, hasName := opts.values["name"]
		rawColor, hasColor := opts.values["color"]
		if !hasName && !hasColor {
			return nil, errors.New("folder update requires --name or --color")
		}
		body := vertracloud.ResourceFolderUpdateBody{}
		if hasName {
			body.Name = &name
		}
		if hasColor {
			body.Color, err = parseColor(rawColor)
			if err != nil {
				return nil, err
			}
		}
		if workspaceID != "" {
			return c.Workspaces.Folders().Update(ctx, workspaceID, pos[1], body)
		}
		return c.Account.Folders().Update(ctx, pos[1], body)
	case "delete":
		if err := exact(pos, 2, "folder delete <folderId> --yes"); err != nil {
			return nil, err
		}
		if err := requireYes(opts); err != nil {
			return nil, err
		}
		if workspaceID != "" {
			err = c.Workspaces.Folders().Delete(ctx, workspaceID, pos[1])
		} else {
			err = c.Account.Folders().Delete(ctx, pos[1])
		}
		if err != nil {
			return nil, err
		}
		return map[string]string{"folder_id": pos[1]}, nil
	case "add", "remove":
		if err := exact(pos, 4, "folder <add|remove> <folderId> <application|database> <resourceId>"); err != nil {
			return nil, err
		}
		typ, err := parseType(pos[2])
		if err != nil {
			return nil, err
		}
		if workspaceID != "" {
			if pos[0] == "add" {
				return c.Workspaces.Folders().AddResource(ctx, workspaceID, pos[1], typ, pos[3], nil)
			}
			return c.Workspaces.Folders().RemoveResource(ctx, workspaceID, pos[1], typ, pos[3])
		}
		if pos[0] == "add" {
			return c.Account.Folders().AddResource(ctx, pos[1], typ, pos[3], nil)
		}
		return c.Account.Folders().RemoveResource(ctx, pos[1], typ, pos[3])
	default:
		return nil, fmt.Errorf("unknown folder command %q", pos[0])
	}
}

func favorite(ctx context.Context, c *vertracloud.Client, args []string) (any, error) {
	pos, opts, err := parse(args)
	if err != nil {
		return nil, err
	}
	if len(pos) == 0 {
		return nil, errors.New("missing favorite command")
	}
	workspaceID := opts.values["workspace"]
	switch pos[0] {
	case "list":
		if err := exact(pos, 1, "favorite list [--workspace <id>]"); err != nil {
			return nil, err
		}
		org, err := organization(ctx, c, workspaceID)
		if err != nil {
			return nil, err
		}
		return org.Favorites, nil
	case "add", "remove":
		if err := exact(pos, 3, "favorite <add|remove> <application|database> <resourceId>"); err != nil {
			return nil, err
		}
		typ, err := parseType(pos[1])
		if err != nil {
			return nil, err
		}
		if workspaceID != "" {
			if pos[0] == "add" {
				return c.Workspaces.Favorites().Add(ctx, workspaceID, typ, pos[2], nil)
			}
			return c.Workspaces.Favorites().Remove(ctx, workspaceID, typ, pos[2])
		}
		if pos[0] == "add" {
			return c.Account.Favorites().Add(ctx, typ, pos[2], nil)
		}
		return c.Account.Favorites().Remove(ctx, typ, pos[2])
	default:
		return nil, fmt.Errorf("unknown favorite command %q", pos[0])
	}
}
