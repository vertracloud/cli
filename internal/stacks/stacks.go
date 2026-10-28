// Package stacks implements `vertra stack`: list, inspect, rename, update to a
// newer template version and delete stacks. Every API call goes through the
// Go SDK (client.Stacks and client.Templates.Updates()).
package stacks

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/vertracloud/cli/internal/apps"
	"github.com/vertracloud/cli/internal/flagset"
	"github.com/vertracloud/cli/internal/templates"
	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// ErrCancelled is returned when the person does not confirm; the dispatcher
// reports it as a cancelled operation, not as a failure.
var ErrCancelled = apps.ErrCancelled

// Run executes one `stack` command and returns the data to render.
func Run(ctx context.Context, client *vertracloud.Client, command string, args []string, jsonMode bool, errOut io.Writer) (any, error) {
	if client == nil || client.Stacks == nil {
		return nil, errors.New("stacks: SDK client is unavailable")
	}
	var (
		data any
		err  error
	)
	switch strings.ToLower(command) {
	case "list", "ls":
		data, err = list(ctx, client, args)
	case "get", "info":
		data, err = get(ctx, client, args)
	case "rename":
		data, err = rename(ctx, client, args)
	case "update":
		data, err = update(ctx, client, args, jsonMode, errOut)
	case "delete", "remove", "rm":
		data, err = remove(ctx, client, args, jsonMode, errOut)
	default:
		return nil, fmt.Errorf("unknown stack command %q", command)
	}
	return data, templates.Friendly(err)
}

func workspaceOpts(workspace string) []rest.RequestOpt {
	if workspace == "" {
		return nil
	}
	return []rest.RequestOpt{rest.WithWorkspaceID(workspace)}
}

func oneID(pos []string, command string) (string, error) {
	if len(pos) != 1 || strings.TrimSpace(pos[0]) == "" {
		return "", fmt.Errorf(templates.Text("stack_needs_one"), command)
	}
	return pos[0], nil
}

// statusWord sums the members up as one word the table colors.
func statusWord(s vertracloud.StackStatus) string {
	switch {
	case s.Building > 0:
		return "building"
	case s.Total == 0:
		return "-"
	case s.Up == s.Total:
		return "up"
	case s.Up == 0:
		return "down"
	}
	return "partial"
}

func templateLabel(t *vertracloud.TemplateOriginInfo) string {
	if t == nil {
		return ""
	}
	label := templates.I18n(t.Name) + "@" + t.Version
	if t.UpdateAvailable {
		label += " ↑"
	}
	return label
}

func list(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	f := flagset.New("stack list")
	workspace := f.String("workspace", "", "workspace id")
	pos, err := flagset.Parse(f, args)
	if err != nil {
		return nil, err
	}
	if len(pos) != 0 {
		return nil, errors.New("stack list takes no arguments")
	}
	stacks, err := client.Stacks.List(ctx, workspaceOpts(*workspace)...)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(stacks))
	for _, s := range stacks {
		row := map[string]any{
			"name": s.Name, "id": s.ID, "status": statusWord(s.Status), "members": float64(len(s.Members)),
			"ram": float64(s.RAMTotal), "updated_at": s.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
		if label := templateLabel(s.Template); label != "" {
			row["template"] = label
		}
		rows = append(rows, row)
	}
	return ui.Presentation{JSON: stacks, Human: rows}, nil
}

func get(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	f := flagset.New("stack get")
	workspace := f.String("workspace", "", "workspace id")
	pos, err := flagset.Parse(f, args)
	if err != nil {
		return nil, err
	}
	id, err := oneID(pos, "stack get")
	if err != nil {
		return nil, err
	}
	stack, err := client.Stacks.Get(ctx, id, workspaceOpts(*workspace)...)
	if err != nil {
		return nil, err
	}
	return ui.Presentation{JSON: stack, Human: stackView{stack}}, nil
}

// rename changes the name and/or the description. An empty --description removes it.
func rename(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	f := flagset.New("stack rename")
	description := f.String("description", "", "new description; empty removes it")
	workspace := f.String("workspace", "", "workspace id")
	pos, err := flagset.Parse(f, args)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	f.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	if len(pos) < 1 || len(pos) > 2 || (len(pos) == 1 && !set["description"]) {
		return nil, errors.New(templates.Text("stack_rename_args"))
	}
	var body vertracloud.StackUpdateBody
	if len(pos) == 2 {
		if strings.TrimSpace(pos[1]) == "" {
			return nil, errors.New("the new name cannot be empty")
		}
		name := pos[1]
		body.Name = &name
	}
	if set["description"] {
		if strings.TrimSpace(*description) == "" {
			body.Description = vertracloud.NullableNull[string]()
		} else {
			body.Description = vertracloud.NullableValue(*description)
		}
	}
	stack, err := client.Stacks.Update(ctx, pos[0], body, workspaceOpts(*workspace)...)
	if err != nil {
		return nil, err
	}
	return ui.Presentation{JSON: stack, Human: map[string]any{"id": stack.ID, "name": stack.Name}}, nil
}

// update moves the stack to the latest version of its template. --check only
// shows what would change.
func update(ctx context.Context, client *vertracloud.Client, args []string, jsonMode bool, errOut io.Writer) (any, error) {
	if client.Templates == nil {
		return nil, errors.New("stacks: SDK client is unavailable")
	}
	f := flagset.New("stack update")
	check := f.Bool("check", false, "only show what the update would change")
	yes := f.Bool("yes", false, "do not ask for confirmation")
	noWait := f.Bool("no-wait", false, "return as soon as the job starts")
	var sets, rams flagset.Repeat
	f.Var(&sets, "set", "answer to a new question: id=value (repeatable)")
	f.Var(&rams, "ram", "memory in MB for a service whose minimum rose: <service>=<MB> (repeatable)")
	pos, err := flagset.Parse(f, args)
	if err != nil {
		return nil, err
	}
	id, err := oneID(pos, "stack update")
	if err != nil {
		return nil, err
	}
	updates := client.Templates.Updates()
	info, err := updates.StackInfo(ctx, id)
	if err != nil {
		return nil, err
	}
	if *check {
		return ui.Presentation{JSON: info, Human: updateView{info}}, nil
	}
	if !info.UpdateAvailable {
		return ui.Presentation{JSON: info, Human: updateView{info}}, nil
	}
	if info.Blocked != nil {
		return nil, &rest.APIError{Code: vertracloud.ErrCodeTemplateUpdateBlocked, Message: fmt.Sprintf(templates.Text("stack_update_blocked"), templates.BlockReason(info.Blocked.Reason))}
	}
	prompt := templates.IsInteractive() && !*yes && !jsonMode
	if prompt {
		updateView{info}.RenderHuman(errOut, ui.Locale)
		fmt.Fprintln(errOut)
	}
	setValues, err := templates.ParseSets(sets)
	if err != nil {
		return nil, err
	}
	answers, err := templates.CollectAnswers(info.NewInputs, setValues, prompt, errOut)
	if err != nil {
		return nil, err
	}
	ram, err := parseRAM(rams, info.RAMChanges)
	if err != nil {
		return nil, err
	}
	if prompt {
		if !templates.Confirm(errOut, templates.Text("stack_update_confirm")) {
			return nil, ErrCancelled
		}
	} else if !*yes && !jsonMode {
		return nil, &rest.APIError{Code: "CONFIRMATION_REQUIRED", Message: "run again with --yes to update, or --check to see what would change"}
	}
	job, err := updates.StackUpdate(ctx, id, vertracloud.TemplateUpdateBody{Inputs: answers, RAM: ram})
	if err != nil {
		return nil, err
	}
	if *noWait {
		return ui.Presentation{JSON: job, Human: map[string]any{"id": job.ID, "status": string(job.Status)}}, nil
	}
	job, err = templates.Follow(ctx, client, job.ID, jsonMode, errOut)
	if err != nil {
		return nil, err
	}
	if job.Status == vertracloud.TemplateDeployStatusFailed {
		return nil, templates.DeployFailure(job)
	}
	return ui.Presentation{JSON: job, Human: templates.DeployView{Job: job}}, nil
}

// parseRAM reads --ram <service>=<MB>; a service whose minimum rose and has no
// value gets that minimum.
func parseRAM(pairs []string, changes []vertracloud.TemplateRAMChange) (map[string]int, error) {
	out := map[string]int{}
	known := map[string]bool{}
	ids := make([]string, 0, len(changes))
	for _, c := range changes {
		known[c.ServiceID] = true
		ids = append(ids, c.ServiceID)
		out[c.ServiceID] = c.RequiredMin
	}
	sort.Strings(ids)
	for _, pair := range pairs {
		service, value, ok := strings.Cut(pair, "=")
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if !ok || err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid --ram %q: use <service>=<MB>", pair)
		}
		if !known[service] {
			return nil, fmt.Errorf("--ram %s: only services whose minimum memory rose take it (%s)", service, strings.Join(ids, ", "))
		}
		out[service] = n
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func remove(ctx context.Context, client *vertracloud.Client, args []string, jsonMode bool, errOut io.Writer) (any, error) {
	f := flagset.New("stack delete")
	deleteAll := f.Bool("delete-all", false, "also delete every member (apps, databases)")
	blob := f.String("blob", "", "keep or delete the Blob files (only with --delete-all; default keep)")
	yes := f.Bool("yes", false, "skip the confirmation")
	workspace := f.String("workspace", "", "workspace id")
	pos, err := flagset.Parse(f, args)
	if err != nil {
		return nil, err
	}
	id, err := oneID(pos, "stack delete")
	if err != nil {
		return nil, err
	}
	disposition := vertracloud.StackBlobDisposition("")
	switch *blob {
	case "":
	case "keep", "delete":
		if !*deleteAll {
			return nil, errors.New(templates.Text("stack_blob_needs_all"))
		}
		disposition = vertracloud.StackBlobDisposition(*blob)
	default:
		return nil, errors.New(templates.Text("stack_blob_bad"))
	}
	opts := workspaceOpts(*workspace)
	stack, err := client.Stacks.Get(ctx, id, opts...)
	if err != nil {
		return nil, err
	}
	mode := vertracloud.StackDeleteModeDissolve
	warn := "stack_confirm_warn_dissolve"
	if *deleteAll {
		mode, warn = vertracloud.StackDeleteModeDeleteAll, "stack_confirm_warn_all"
	}
	if !*yes {
		if jsonMode || !templates.IsInteractive() {
			return nil, &rest.APIError{Code: "CONFIRMATION_REQUIRED", Message: fmt.Sprintf(templates.Text("stack_confirm_required"), stack.Name)}
		}
		fmt.Fprintf(errOut, templates.Text(warn)+"\n", ui.Plain(stack.Name), len(stack.Members))
		fmt.Fprintf(errOut, templates.Text("stack_confirm_prompt"), ui.Plain(stack.Name))
		typed, _ := templates.ReadLine()
		if strings.TrimSpace(typed) != stack.Name {
			return nil, ErrCancelled
		}
	}
	result, err := client.Stacks.Delete(ctx, id, vertracloud.StackDeleteBody{Mode: mode, ConfirmName: stack.Name, Blob: disposition}, opts...)
	if err != nil {
		return nil, err
	}
	human := map[string]any{"id": id}
	if len(result.DeletedApps) > 0 {
		human["deleted_apps"] = result.DeletedApps
	}
	if len(result.DeletedDatabases) > 0 {
		human["deleted_databases"] = result.DeletedDatabases
	}
	if result.DeletedBlob {
		human["deleted_blob"] = true
	}
	return ui.Presentation{JSON: result, Human: human}, nil
}
