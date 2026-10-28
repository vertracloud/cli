package templates

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vertracloud/cli/internal/flagset"
	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// Overridable by tests.
var (
	pollEvery   = 2 * time.Second
	followLimit = 30 * time.Minute
)

// target is what a deploy needs from the template (or share link) it uses.
type target struct {
	slug     string
	inputs   []vertracloud.TemplateInput
	services []vertracloud.TemplateServiceSummary
	deploy   func(context.Context, deployRequest) (vertracloud.TemplateDeploy, error)
}

type deployRequest struct {
	name, description, version, workspace, folder string
	inputs                                        map[string]vertracloud.TemplateInputValue
	services                                      map[string]vertracloud.TemplateDeployServiceOptions
	idempotencyKey                                string
}

func newKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// shareToken accepts the token or the link that carries it.
func shareToken(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.IndexAny(v, "?#"); i >= 0 {
		v = v[:i]
	}
	v = strings.TrimRight(v, "/")
	if i := strings.LastIndexByte(v, '/'); i >= 0 {
		v = v[i+1:]
	}
	return v
}

func deploy(ctx context.Context, client *vertracloud.Client, args []string, jsonMode bool, errOut io.Writer) (any, error) {
	f := flagset.New("template deploy")
	name := f.String("name", "", "name of the app, or of the stack")
	description := f.String("description", "", "description")
	version := f.String("version", "", "template version (default: the latest)")
	share := f.String("share", "", "share link (or its token) instead of a catalog slug")
	workspace := f.String("workspace", "", "workspace that will own what is created")
	folder := f.String("folder", "", "folder id")
	yes := f.Bool("yes", false, "do not ask anything; use --set for the answers")
	noWait := f.Bool("no-wait", false, "return as soon as the job starts")
	var sets, svcs flagset.Repeat
	f.Var(&sets, "set", "answer to a question: id=value (repeatable)")
	f.Var(&svcs, "service", "choice for a service: <service>.<name|ram|subdomain>=<value> (repeatable)")
	pos, err := flagset.Parse(f, args)
	if err != nil {
		return nil, err
	}
	var tgt target
	switch {
	case *share != "":
		if len(pos) != 0 {
			return nil, errors.New("template deploy takes either a slug or --share")
		}
		if *version != "" {
			return nil, errors.New("--version does not apply to a share link")
		}
		tgt, err = shareTarget(ctx, client, shareToken(*share))
	default:
		if len(pos) != 1 || strings.TrimSpace(pos[0]) == "" {
			return nil, errors.New(text("need_template"))
		}
		tgt, err = catalogTarget(ctx, client, pos[0], *workspace)
	}
	if err != nil {
		return nil, err
	}

	prompt := interactive() && !*yes && !jsonMode
	setValues, err := ParseSets(sets)
	if err != nil {
		return nil, err
	}
	options, err := parseServices(svcs, tgt.services)
	if err != nil {
		return nil, err
	}
	appName := strings.TrimSpace(*name)
	if appName == "" {
		if !prompt {
			return nil, errors.New(text("name_required"))
		}
		fmt.Fprintf(errOut, "%s %s %s: ", ui.Yellow("?"), ui.Bold(text("ask_name")), ui.Dim("["+tgt.slug+"]"))
		line, _ := readLine()
		if appName = strings.TrimSpace(line); appName == "" {
			appName = tgt.slug
		}
	}
	answers, err := CollectAnswers(tgt.inputs, setValues, prompt, errOut)
	if err != nil {
		return nil, err
	}
	key, err := newKey()
	if err != nil {
		return nil, err
	}
	if !jsonMode {
		fmt.Fprintln(errOut, ui.Dim(fmt.Sprintf(text("deploy_started"), appName)))
	}
	job, err := tgt.deploy(ctx, deployRequest{
		name: appName, description: *description, version: *version, workspace: *workspace, folder: *folder,
		inputs: answers, services: options, idempotencyKey: key,
	})
	if err != nil {
		return nil, err
	}
	if *noWait {
		return ui.Presentation{JSON: job, Human: messageView{Success: fmt.Sprintf(text("deploy_still"), job.ID)}}, nil
	}
	job, err = Follow(ctx, client, job.ID, jsonMode, errOut)
	if err != nil {
		return nil, err
	}
	if job.Status == vertracloud.TemplateDeployStatusFailed {
		return nil, DeployFailure(job)
	}
	if !job.Status.Finished() {
		return ui.Presentation{JSON: job, Human: messageView{Success: fmt.Sprintf(text("deploy_still"), job.ID)}}, nil
	}
	return ui.Presentation{JSON: job, Human: DeployView{job}}, nil
}

func catalogTarget(ctx context.Context, client *vertracloud.Client, slug, workspace string) (target, error) {
	detail, err := client.Templates.Get(ctx, slug, workspaceOpts(workspace)...)
	if err != nil {
		return target{}, err
	}
	return target{
		slug: detail.Slug, inputs: detail.Inputs, services: detail.Services,
		deploy: func(ctx context.Context, r deployRequest) (vertracloud.TemplateDeploy, error) {
			return client.Templates.Deploy(ctx, slug, vertracloud.TemplateDeployBody{
				Name: r.name, Description: r.description, Version: r.version, Inputs: r.inputs, Services: r.services,
				WorkspaceID: r.workspace, FolderID: r.folder, IdempotencyKey: r.idempotencyKey,
			})
		},
	}, nil
}

func shareTarget(ctx context.Context, client *vertracloud.Client, token string) (target, error) {
	if token == "" {
		return target{}, errors.New(text("need_template"))
	}
	preview, err := client.Templates.Share().Preview(ctx, token)
	if err != nil {
		return target{}, err
	}
	if preview.Mode == vertracloud.TemplateShareLinkModeFullCopy {
		return target{}, errors.New("this link gives a copy of the template, not a deploy; copy it from the dashboard")
	}
	return target{
		slug: "app", inputs: preview.Inputs, services: preview.Services,
		deploy: func(ctx context.Context, r deployRequest) (vertracloud.TemplateDeploy, error) {
			return client.Templates.Share().Deploy(ctx, token, vertracloud.TemplateShareDeployBody{
				Name: r.name, Description: r.description, Inputs: r.inputs, Services: r.services,
				WorkspaceID: r.workspace, FolderID: r.folder, IdempotencyKey: r.idempotencyKey,
			})
		},
	}, nil
}

// parseServices reads --service <service>.<name|ram|subdomain>=<value>.
func parseServices(pairs []string, services []vertracloud.TemplateServiceSummary) (map[string]vertracloud.TemplateDeployServiceOptions, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	known := map[string]bool{}
	ids := make([]string, 0, len(services))
	for _, s := range services {
		known[s.ID] = true
		ids = append(ids, s.ID)
	}
	sort.Strings(ids)
	out := map[string]vertracloud.TemplateDeployServiceOptions{}
	for _, pair := range pairs {
		left, value, ok := strings.Cut(pair, "=")
		id, field, ok2 := strings.Cut(left, ".")
		if !ok || !ok2 || id == "" || field == "" {
			return nil, fmt.Errorf(text("bad_service"), pair)
		}
		if !known[id] {
			return nil, fmt.Errorf(text("unknown_service"), id, orNone(ids))
		}
		o := out[id]
		switch field {
		case "name":
			o.Name = value
		case "subdomain":
			o.Subdomain = value
		case "ram":
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("--service %s: %s", left, text("v_not_number"))
			}
			o.RAM = n
		default:
			return nil, fmt.Errorf(text("bad_service"), pair)
		}
		out[id] = o
	}
	return out, nil
}

// stepLabel is the name of a deploy step in the current language.
func stepLabel(s vertracloud.TemplateDeployStep) string {
	if t := text("step_" + string(s)); t != "" {
		return t
	}
	return string(s)
}

// Follow shows the progress of a deploy or update job (human mode) until it
// finishes and returns the final job, with its outputs. It reads the progress
// stream and, if that fails or ends early, polls the job every few seconds.
func Follow(ctx context.Context, client *vertracloud.Client, id string, jsonMode bool, errOut io.Writer) (vertracloud.TemplateDeploy, error) {
	ctx, cancel := context.WithTimeout(ctx, followLimit)
	defer cancel()
	seen := map[vertracloud.TemplateDeployStep]vertracloud.TemplateDeployStepStatus{}
	report := func(steps []vertracloud.TemplateDeployStepState) {
		if jsonMode || errOut == nil {
			return
		}
		for _, s := range steps {
			if seen[s.Step] == s.Status {
				continue
			}
			seen[s.Step] = s.Status
			switch s.Status {
			case vertracloud.TemplateDeployStepStatusRunning:
				fmt.Fprintf(errOut, "%s %s…\n", ui.Cyan("→"), stepLabel(s.Step))
			case vertracloud.TemplateDeployStepStatusDone:
				fmt.Fprintf(errOut, "%s %s\n", ui.Green("✓"), stepLabel(s.Step))
			case vertracloud.TemplateDeployStepStatusFailed:
				fmt.Fprintf(errOut, "%s %s\n", ui.Red("✗"), stepLabel(s.Step))
			}
		}
	}
	if stream, err := client.Templates.Deploys().Events(ctx, id); err == nil {
		for stream.Next(ctx) {
			report(stream.Event().Steps)
		}
		_ = stream.Close()
	}
	for {
		job, err := client.Templates.Deploys().Get(ctx, id)
		if err != nil {
			return job, err
		}
		report(job.Steps)
		if job.Status.Finished() {
			return job, nil
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return job, nil // still running: the caller says so
			}
			return job, ctx.Err()
		case <-time.After(pollEvery):
		}
	}
}

// DeployFailure turns a failed job into an error with the code and a
// message in the current language.
func DeployFailure(job vertracloud.TemplateDeploy) error {
	apiErr := &rest.APIError{Code: vertracloud.ErrCodeTemplateDeployFailed, Message: text("deploy_failed")}
	if job.Error != nil {
		apiErr.Code, apiErr.Message, apiErr.Details = job.Error.Code, job.Error.Message, job.Error.Details
		if apiErr.Code == "" {
			apiErr.Code = vertracloud.ErrCodeTemplateDeployFailed
		}
	}
	err := Friendly(apiErr)
	if job.Error != nil && job.Error.Step != nil {
		if c, ok := rest.AsAPIError(err); ok {
			c.Message += " (" + stepLabel(*job.Error.Step) + ")"
		}
	}
	return err
}
