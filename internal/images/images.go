// Package images implements `vertra image`: custom Docker images built from a
// context folder or zip, and apps created from them. Every API call goes
// through the Go SDK (client.Images and client.Apps.CreateFromImage).
package images

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/vertracloud/cli/internal/apps"
	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// ErrCancelled is returned when the person does not confirm a deletion; the
// dispatcher reports it as a cancelled operation, not as a failure.
var ErrCancelled = apps.ErrCancelled

// Overridable by tests.
var (
	stdin       io.Reader = os.Stdin
	interactive           = ui.IsInteractive
	// logIdle ends `logs` without --follow: the API repeats the history and
	// then follows the build live, so without --follow the CLI stops when no
	// new line arrives for this long.
	logIdle = 3 * time.Second
)

// Run executes one `image` command and returns the data to render.
func Run(ctx context.Context, client *vertracloud.Client, command string, args []string, jsonMode bool) (any, error) {
	if client == nil || client.Images == nil {
		return nil, errors.New("images: SDK client is unavailable")
	}
	var (
		data any
		err  error
	)
	switch strings.ToLower(command) {
	case "build", "create":
		data, err = build(ctx, client, args, jsonMode)
	case "list", "ls":
		data, err = list(ctx, client, args)
	case "get", "info":
		data, err = get(ctx, client, args)
	case "update":
		data, err = update(ctx, client, args)
	case "rename":
		data, err = rename(ctx, client, args)
	case "rebuild":
		data, err = rebuild(ctx, client, args, jsonMode)
	case "delete", "remove", "rm":
		data, err = remove(ctx, client, args, jsonMode)
	case "logs":
		var lines []string
		lines, err = collectLogs(ctx, client, args)
		data = map[string]any{"lines": lines}
	case "deploy":
		data, err = deploy(ctx, client, args)
	default:
		return nil, fmt.Errorf("unknown image command %q", command)
	}
	return data, friendly(err)
}

// StreamLogs writes the build log to out as lines arrive (human mode).
func StreamLogs(ctx context.Context, client *vertracloud.Client, args []string, out io.Writer) error {
	if client == nil || client.Images == nil {
		return errors.New("images: SDK client is unavailable")
	}
	return friendly(streamLogs(ctx, client, args, func(line string) {
		_, _ = fmt.Fprintln(out, ui.LogText(line))
	}))
}

// ---------------------------------------------------------------------------
// flags

func newFlags(name string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	return f
}

func canonicalFlag(arg string) string {
	prefix, value := arg, ""
	if n := strings.IndexByte(arg, '='); n >= 0 {
		prefix, value = arg[:n], arg[n:]
	}
	switch prefix {
	case "-f":
		prefix = "--follow"
	case "-y":
		prefix = "--yes"
	case "-e":
		prefix = "--env"
	}
	return prefix + value
}

// parse accepts flags before or after the positional arguments, like the app commands.
func parse(f *flag.FlagSet, args []string, values, bools map[string]bool) ([]string, error) {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := canonicalFlag(args[i])
		key := a
		if n := strings.IndexByte(a, '='); n >= 0 {
			key = a[:n]
		}
		if key == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		switch {
		case values[key]:
			flags = append(flags, a)
			if !strings.Contains(a, "=") {
				if i+1 >= len(args) {
					return nil, fmt.Errorf("flag %s needs a value", key)
				}
				flags = append(flags, args[i+1])
				i++
			}
		case bools[key]:
			flags = append(flags, a)
		case strings.HasPrefix(key, "-") && len(key) > 1:
			return nil, fmt.Errorf("unknown flag %q", args[i])
		default:
			positional = append(positional, a)
		}
	}
	if err := f.Parse(flags); err != nil {
		return nil, err
	}
	return positional, nil
}

type repeatFlag []string

func (r *repeatFlag) String() string     { return strings.Join(*r, ",") }
func (r *repeatFlag) Set(v string) error { *r = append(*r, v); return nil }

func oneID(pos []string, command string) (string, error) {
	if len(pos) != 1 || strings.TrimSpace(pos[0]) == "" {
		return "", fmt.Errorf("%s needs exactly one image id", command)
	}
	return pos[0], nil
}

func workspaceOpts(workspace string) []rest.RequestOpt {
	if workspace == "" {
		return nil
	}
	return []rest.RequestOpt{rest.WithWorkspaceID(workspace)}
}

// ---------------------------------------------------------------------------
// commands

func build(ctx context.Context, client *vertracloud.Client, args []string, jsonMode bool) (any, error) {
	f := newFlags("image build")
	name := f.String("name", "", "image name")
	description := f.String("description", "", "image description (up to 128 characters)")
	contextPath := f.String("context", ".", "folder or .zip with the Dockerfile")
	workspace := f.String("workspace", "", "workspace id")
	pos, err := parse(f, args, map[string]bool{"--name": true, "--description": true, "--context": true, "--workspace": true}, nil)
	if err != nil {
		return nil, err
	}
	if len(pos) != 0 {
		return nil, fmt.Errorf("unexpected arguments: %s", strings.Join(pos, " "))
	}
	if strings.TrimSpace(*name) == "" {
		return nil, errors.New("image build requires --name")
	}
	stop := spin(jsonMode, "pack")
	src, err := OpenContext(*contextPath)
	stop()
	if err != nil {
		return nil, err
	}
	defer src.Close()
	stop = spin(jsonMode, "upload")
	image, err := client.Images.Create(ctx, vertracloud.ImageCreateParams{File: src.Reader, FileName: src.Name, Name: *name, Description: *description, WorkspaceID: *workspace})
	stop()
	if err != nil {
		return nil, err
	}
	followHint(jsonMode, image.ID)
	return image, nil
}

func list(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	f := newFlags("image list")
	workspace := f.String("workspace", "", "workspace id")
	pos, err := parse(f, args, map[string]bool{"--workspace": true}, nil)
	if err != nil {
		return nil, err
	}
	if len(pos) != 0 {
		return nil, errors.New("image list takes no arguments")
	}
	images, err := client.Images.List(ctx, workspaceOpts(*workspace)...)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(images))
	for _, im := range images {
		row := map[string]any{"id": im.ID, "name": im.Name, "status": im.Status, "apps": len(im.Apps), "updated_at": im.UpdatedAt}
		if im.UncompressedBytes != nil {
			row["size"] = humanBytes(*im.UncompressedBytes)
		}
		if im.SuggestedPort != nil {
			row["suggested_port"] = *im.SuggestedPort
		}
		if im.Description != nil {
			row["description"] = *im.Description
		}
		if im.GitHub != nil {
			row["github"] = im.GitHub.Repository + "@" + im.GitHub.Branch
		}
		rows = append(rows, row)
	}
	return ui.Presentation{JSON: images, Human: rows}, nil
}

func get(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	f := newFlags("image get")
	workspace := f.String("workspace", "", "workspace id")
	pos, err := parse(f, args, map[string]bool{"--workspace": true}, nil)
	if err != nil {
		return nil, err
	}
	id, err := oneID(pos, "image get")
	if err != nil {
		return nil, err
	}
	return client.Images.Get(ctx, id, workspaceOpts(*workspace)...)
}

// update renames the image and/or changes its description. Nothing is built.
// An empty --description removes it (sent as JSON null).
func update(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	f := newFlags("image update")
	name := f.String("name", "", "new image name")
	description := f.String("description", "", "new description (up to 128 characters); empty removes it")
	workspace := f.String("workspace", "", "workspace id")
	pos, err := parse(f, args, map[string]bool{"--name": true, "--description": true, "--workspace": true}, nil)
	if err != nil {
		return nil, err
	}
	id, err := oneID(pos, "image update")
	if err != nil {
		return nil, err
	}
	var body vertracloud.ImageUpdateBody
	set := map[string]bool{}
	f.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	if set["name"] {
		if strings.TrimSpace(*name) == "" {
			return nil, errors.New("--name cannot be empty")
		}
		body.Name = name
	}
	if set["description"] {
		if strings.TrimSpace(*description) == "" {
			body.Description = vertracloud.NullableNull[string]()
		} else {
			body.Description = vertracloud.NullableValue(*description)
		}
	}
	if body.Name == nil && body.Description == nil {
		return nil, errors.New("image update needs --name and/or --description")
	}
	return client.Images.Update(ctx, id, body, workspaceOpts(*workspace)...)
}

// rename is `update --name`: vertra image rename <id> <new-name>.
func rename(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	f := newFlags("image rename")
	workspace := f.String("workspace", "", "workspace id")
	pos, err := parse(f, args, map[string]bool{"--workspace": true}, nil)
	if err != nil {
		return nil, err
	}
	if len(pos) != 2 || strings.TrimSpace(pos[0]) == "" || strings.TrimSpace(pos[1]) == "" {
		return nil, errors.New("image rename needs an image id and the new name")
	}
	name := pos[1]
	return client.Images.Update(ctx, pos[0], vertracloud.ImageUpdateBody{Name: &name}, workspaceOpts(*workspace)...)
}

func rebuild(ctx context.Context, client *vertracloud.Client, args []string, jsonMode bool) (any, error) {
	f := newFlags("image rebuild")
	contextPath := f.String("context", ".", "folder or .zip with the Dockerfile")
	workspace := f.String("workspace", "", "workspace id")
	pos, err := parse(f, args, map[string]bool{"--context": true, "--workspace": true}, nil)
	if err != nil {
		return nil, err
	}
	id, err := oneID(pos, "image rebuild")
	if err != nil {
		return nil, err
	}
	stop := spin(jsonMode, "pack")
	src, err := OpenContext(*contextPath)
	stop()
	if err != nil {
		return nil, err
	}
	defer src.Close()
	stop = spin(jsonMode, "upload")
	image, err := client.Images.Rebuild(ctx, id, vertracloud.ImageRebuildParams{File: src.Reader, FileName: src.Name, WorkspaceID: *workspace})
	stop()
	if err != nil {
		return nil, err
	}
	followHint(jsonMode, image.ID)
	return image, nil
}

func remove(ctx context.Context, client *vertracloud.Client, args []string, jsonMode bool) (any, error) {
	f := newFlags("image delete")
	yes := f.Bool("yes", false, "skip the confirmation")
	workspace := f.String("workspace", "", "workspace id")
	pos, err := parse(f, args, map[string]bool{"--workspace": true}, map[string]bool{"--yes": true})
	if err != nil {
		return nil, err
	}
	id, err := oneID(pos, "image delete")
	if err != nil {
		return nil, err
	}
	opts := workspaceOpts(*workspace)
	image, err := client.Images.Get(ctx, id, opts...)
	if err != nil {
		return nil, err
	}
	if !*yes {
		if jsonMode || !interactive() {
			return nil, &rest.APIError{Code: "CONFIRMATION_REQUIRED", Message: fmt.Sprintf(text("confirm_required"), image.Name)}
		}
		fmt.Fprintf(os.Stderr, text("confirm_warn")+"\n", image.Name, len(image.Apps))
		fmt.Fprintf(os.Stderr, text("confirm_prompt"), image.Name)
		typed, _ := bufio.NewReader(stdin).ReadString('\n')
		if strings.TrimSpace(typed) != image.Name {
			return nil, ErrCancelled
		}
	}
	result, err := client.Images.Delete(ctx, id, vertracloud.ImageDeleteBody{ConfirmName: image.Name}, opts...)
	if err != nil {
		return nil, err
	}
	deleted := result.DeletedApps
	if deleted == nil {
		deleted = []string{}
	}
	return map[string]any{"id": id, "deleted_apps": deleted}, nil
}

func deploy(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	if client.Apps == nil {
		return nil, errors.New("images: SDK client is unavailable")
	}
	f := newFlags("image deploy")
	name := f.String("name", "", "app name")
	ram := f.Int("ram", 0, "RAM in MB")
	web := f.Bool("web", false, "publish the app on the web")
	port := f.Int("port", 0, "port the app listens on (with --web)")
	subdomain := f.String("subdomain", "", "subdomain, or random (with --web)")
	description := f.String("description", "", "app description")
	workspace := f.String("workspace", "", "workspace id")
	var envs repeatFlag
	f.Var(&envs, "env", "environment variable KEY=VALUE (repeatable)")
	pos, err := parse(f, args,
		map[string]bool{"--name": true, "--ram": true, "--port": true, "--subdomain": true, "--description": true, "--workspace": true, "--env": true},
		map[string]bool{"--web": true})
	if err != nil {
		return nil, err
	}
	id, err := oneID(pos, "image deploy")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(*name) == "" || *ram <= 0 {
		return nil, errors.New("image deploy requires --name and --ram (MB)")
	}
	if !*web && (*port != 0 || *subdomain != "") {
		return nil, errors.New("--port and --subdomain need --web")
	}
	if *port < 0 || *port > 65535 {
		return nil, errors.New("--port must be between 1 and 65535")
	}
	inputs := make([]vertracloud.ApplicationEnvironmentInput, 0, len(envs))
	for _, pair := range envs {
		k, v, ok := strings.Cut(pair, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid --env %q: use KEY=VALUE", pair)
		}
		inputs = append(inputs, vertracloud.ApplicationEnvironmentInput{Key: k, Value: v})
	}
	params := vertracloud.ApplicationCreateFromImageParams{
		ImageID: id, Name: *name, Memory: *ram, Description: *description, Web: *web,
		Subdomain: *subdomain, Envs: inputs, WorkspaceID: *workspace,
	}
	if *port != 0 {
		p := *port
		params.Port = &p
	} else if *web {
		// Without --port, a published app listens on the image's suggested port (first EXPOSE, or 80).
		image, err := client.Images.Get(ctx, id, workspaceOpts(*workspace)...)
		if err != nil {
			return nil, err
		}
		if image.SuggestedPort != nil {
			p := *image.SuggestedPort
			params.Port = &p
		}
	}
	return client.Apps.CreateFromImage(ctx, params)
}

// ---------------------------------------------------------------------------
// logs

type logOptions struct {
	id     string
	params *vertracloud.ImageLogsParams
	follow bool
}

func parseLogs(args []string) (logOptions, error) {
	f := newFlags("image logs")
	follow := f.Bool("follow", false, "follow the build live")
	version := f.String("version", "", "image version id (default: the latest build)")
	workspace := f.String("workspace", "", "workspace id")
	pos, err := parse(f, args, map[string]bool{"--version": true, "--workspace": true}, map[string]bool{"--follow": true})
	if err != nil {
		return logOptions{}, err
	}
	id, err := oneID(pos, "image logs")
	if err != nil {
		return logOptions{}, err
	}
	return logOptions{id: id, follow: *follow, params: &vertracloud.ImageLogsParams{VersionID: *version, WorkspaceID: *workspace}}, nil
}

func streamLogs(ctx context.Context, client *vertracloud.Client, args []string, emit func(string)) error {
	o, err := parseLogs(args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := client.Images.Logs(ctx, o.id, o.params)
	if err != nil {
		return err
	}
	defer stream.Close()
	var timer *time.Timer
	if !o.follow {
		timer = time.AfterFunc(logIdle, cancel)
		defer timer.Stop()
	}
	for stream.Next(ctx) {
		if timer != nil {
			timer.Reset(logIdle)
		}
		emit(stream.Line())
	}
	if err := stream.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func collectLogs(ctx context.Context, client *vertracloud.Client, args []string) ([]string, error) {
	lines := []string{}
	err := streamLogs(ctx, client, args, func(l string) { lines = append(lines, ui.LogText(l)) })
	return lines, err
}

// ---------------------------------------------------------------------------
// output helpers

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

func followHint(jsonMode bool, id string) {
	if jsonMode {
		return
	}
	fmt.Fprintln(os.Stderr, ui.Dim(fmt.Sprintf(text("follow_hint"), id)))
}

func spin(jsonMode bool, action string) func() {
	if jsonMode {
		return func() {}
	}
	return ui.Spin(text("spin_" + action))
}
