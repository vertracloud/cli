package templates

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// KV prints aligned "label  value" lines, the way the other detail views do.
type KV struct{ Label, Value string }

// PrintKV writes the rows with the labels dimmed and aligned. Values are
// passed through ui.Plain: they come from the server.
func PrintKV(w io.Writer, indent string, rows []KV) {
	width := 0
	for _, r := range rows {
		width = max(width, utf8.RuneCountInString(r.Label))
	}
	for _, r := range rows {
		fmt.Fprintf(w, "%s%s%s  %s\n", indent, ui.Dim(r.Label), strings.Repeat(" ", width-utf8.RuneCountInString(r.Label)), ui.Plain(r.Value))
	}
}

func serviceLine(s vertracloud.TemplateServiceSummary) string {
	kind := s.Kind
	switch {
	case s.Database != nil:
		kind = string(*s.Database)
	case s.Kind == "app" && s.SourceType != nil:
		kind = "app/" + string(*s.SourceType)
	case s.Kind == "blob" && s.BlobVisibility != nil:
		kind = "blob/" + string(*s.BlobVisibility)
	}
	out := fmt.Sprintf("%s (%s", s.ID, kind)
	if s.RAM.Min > 0 {
		out += fmt.Sprintf(", %d MB", s.RAM.Min)
	}
	out += ")"
	if s.Conditional {
		out += " *"
	}
	return out
}

// ---------------------------------------------------------------------------
// info

type infoView struct{ d vertracloud.TemplateDetail }

func (v infoView) RenderHuman(w io.Writer, _ string) {
	d := v.d
	fmt.Fprintf(w, "%s\n", ui.Bold(ui.Plain(I18n(d.Name))))
	if desc := I18n(d.Description); desc != "" {
		fmt.Fprintf(w, "%s\n", ui.Plain(desc))
	}
	fmt.Fprintln(w)
	rows := []KV{
		{text("label_slug"), d.Slug},
		{text("label_origin"), string(d.Origin)},
		{text("label_category"), string(d.Category)},
		{text("label_version"), d.CurrentVersion.Version},
		{text("label_plan"), string(d.RequiredPlan)},
		{text("label_ram"), fmt.Sprintf("%d MB", d.RAMMinTotal)},
		{text("label_stack"), yesNo(d.CreatesStack)},
	}
	if d.AuthorDisplay != nil {
		rows = append(rows, KV{text("label_author"), *d.AuthorDisplay})
	}
	if d.Origin == vertracloud.TemplateOriginPrivate {
		rows = append(rows, KV{"", text("label_unverified")})
	}
	if d.Account != nil {
		fit := yesNo(d.Account.Fits)
		if !d.Account.Fits {
			var reasons []string
			for _, r := range d.Account.Reasons {
				reasons = append(reasons, fitReason(r.Reason))
			}
			fit += " (" + strings.Join(reasons, ", ") + ")"
		}
		rows = append(rows, KV{text("label_fits"), fit})
	}
	PrintKV(w, "", rows)
	if len(d.Services) > 0 {
		fmt.Fprintf(w, "\n%s\n", ui.Bold(text("label_services")))
		for _, s := range d.Services {
			fmt.Fprintf(w, "  %s\n", ui.Plain(serviceLine(s)))
		}
	}
	if len(d.Inputs) > 0 {
		fmt.Fprintf(w, "\n%s\n", ui.Bold(text("label_inputs")))
		for _, in := range d.Inputs {
			line := fmt.Sprintf("%s  %s  %s", in.ID, ui.Dim(string(in.Type)), ui.Plain(I18n(in.Label)))
			if in.Required && in.Default == nil {
				line += " " + ui.Dim("("+text("label_required")+")")
			}
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
}

func fitReason(r vertracloud.TemplateFitReason) string {
	if t := text("fit_" + string(r)); t != "" {
		return t
	}
	return string(r)
}

// ---------------------------------------------------------------------------
// validate

type issueView struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
	// Line is the 1-based line in the file; 0 when unknown.
	Line int    `json:"line,omitempty"`
	File string `json:"-"`
}

func (i issueView) location() string {
	switch {
	case i.File != "" && i.Line > 0:
		return fmt.Sprintf("%s:%d", i.File, i.Line)
	case i.File != "":
		return i.File
	}
	return ""
}

type validationView struct {
	File         string
	Valid        bool
	Issues       []issueView
	RequiredPlan string
	RAMMinTotal  int
	Services     []vertracloud.TemplateServiceSummary
}

func newValidationView(m *Manifest, v vertracloud.TemplateValidation) validationView {
	out := validationView{File: m.File, Valid: v.Valid && len(v.Issues) == 0, Services: v.Services}
	for _, i := range v.Issues {
		out.Issues = append(out.Issues, issueView{Path: i.Path, Code: string(i.Code), Message: i.Message, Line: m.Line(i.Path), File: m.File})
	}
	if v.RequiredPlan != nil {
		out.RequiredPlan = string(*v.RequiredPlan)
	}
	if v.RAMMinTotal != nil {
		out.RAMMinTotal = *v.RAMMinTotal
	}
	return out
}

// JSON is the machine output: what the API answered, with the line of each issue.
func (v validationView) JSON() any {
	issues := v.Issues
	if issues == nil {
		issues = []issueView{}
	}
	out := map[string]any{"valid": v.Valid, "file": v.File, "issues": issues}
	if v.Valid {
		out["required_plan"] = v.RequiredPlan
		out["ram_min_total"] = v.RAMMinTotal
		out["services"] = v.Services
	}
	return out
}

func (v validationView) RenderHuman(w io.Writer, _ string) {
	if !v.Valid {
		ui.Failure(w, fmt.Sprintf(text("manifest_bad"), len(v.Issues)), "")
		for _, i := range v.Issues {
			where := i.location()
			if where == "" {
				where = v.File
			}
			line := fmt.Sprintf("  %s", ui.Cyan(ui.Plain(where)))
			if i.Path != "" {
				line += "  " + ui.Plain(i.Path)
			}
			line += "  " + ui.Dim(ui.Plain(i.Code)) + "  " + ui.Plain(i.Message)
			fmt.Fprintln(w, line)
		}
		return
	}
	ui.Success(w, text("manifest_ok"))
	rows := []KV{{text("label_plan"), v.RequiredPlan}, {text("label_ram"), fmt.Sprintf("%d MB", v.RAMMinTotal)}}
	PrintKV(w, "  ", rows)
	if len(v.Services) > 0 {
		fmt.Fprintf(w, "  %s\n", ui.Dim(text("label_services")))
		for _, s := range v.Services {
			fmt.Fprintf(w, "    %s\n", ui.Plain(serviceLine(s)))
		}
	}
}

// ---------------------------------------------------------------------------
// create

type createdView struct {
	Detail  *vertracloud.TemplateDetail
	Version *vertracloud.TemplateVersion
	// ID is the template of a new version.
	ID string
}

func (v createdView) RenderHuman(w io.Writer, _ string) {
	if v.Version != nil {
		ui.Success(w, fmt.Sprintf(text("newver_hint"), v.Version.Version))
		PrintKV(w, "  ", []KV{{text("label_id"), v.ID}, {text("label_version"), v.Version.Version}})
		return
	}
	d := v.Detail
	ui.Success(w, text("created"))
	PrintKV(w, "  ", []KV{
		{text("label_name"), I18n(d.Name)}, {text("label_id"), d.ID}, {text("label_slug"), d.Slug},
		{text("label_version"), d.CurrentVersion.Version}, {text("label_plan"), string(d.RequiredPlan)},
	})
	fmt.Fprintf(w, "\n  %s\n", ui.Dim(fmt.Sprintf(text("created_hint"), d.Slug)))
}

// ---------------------------------------------------------------------------
// share

type shareView struct {
	l vertracloud.TemplateShareLinkCreated
}

func (v shareView) RenderHuman(w io.Writer, _ string) {
	l := v.l
	maxUses, expires := text("label_unlimited"), text("label_never")
	if l.MaxUses != nil {
		maxUses = fmt.Sprint(*l.MaxUses)
	}
	if l.ExpiresAt != nil {
		expires = l.ExpiresAt.Local().Format("2006-01-02 15:04")
	}
	ui.Success(w, text("created"))
	PrintKV(w, "  ", []KV{
		{text("label_id"), l.ID}, {text("label_mode"), string(l.Mode)}, {text("label_uses"), maxUses}, {text("label_expires"), expires},
		{text("label_token"), l.Token},
	})
	fmt.Fprintf(w, "\n  %s\n", ui.Yellow(text("share_once")))
}

// ---------------------------------------------------------------------------
// one-line results

type messageView struct{ Success string }

func (v messageView) RenderHuman(w io.Writer, _ string) { ui.Success(w, v.Success) }

// ---------------------------------------------------------------------------
// deploy

// DeployView prints a finished deploy: what was created and the outputs.
// Outputs marked secret or once appear here only; the API does not show them again.
type DeployView struct{ Job vertracloud.TemplateDeploy }

func (v DeployView) RenderHuman(w io.Writer, _ string) {
	j := v.Job
	ui.Success(w, text("deploy_done"))
	rows := []KV{{text("label_id"), j.ID}, {text("label_version"), j.Version}}
	if j.StackID != nil {
		rows = append(rows, KV{"Stack", *j.StackID})
	}
	if j.ApplicationID != nil {
		rows = append(rows, KV{"App", *j.ApplicationID})
	}
	PrintKV(w, "  ", rows)
	if len(j.Resources) > 0 {
		fmt.Fprintf(w, "\n  %s\n", ui.Bold(text("resources")))
		for _, r := range j.Resources {
			fmt.Fprintf(w, "    %s  %s  %s\n", ui.Plain(string(r.ResourceType)), ui.Plain(r.Name), ui.Dim(ui.Plain(r.ResourceID)))
		}
	}
	if len(j.Outputs) == 0 {
		return
	}
	fmt.Fprintf(w, "\n  %s\n", ui.Bold(text("outputs")))
	secret := false
	for _, o := range j.Outputs {
		value := outputText(o)
		label := ui.Plain(I18n(o.Label))
		switch {
		case value == "" && o.Once:
			fmt.Fprintf(w, "    %s  %s\n", ui.Dim(label), ui.Dim(text("output_read")))
		default:
			fmt.Fprintf(w, "    %s  %s\n", ui.Dim(label), ui.Plain(value))
		}
		secret = secret || o.Secret || o.Once
	}
	if secret {
		fmt.Fprintf(w, "\n  %s\n", ui.Yellow(text("secret_once")))
	}
}

// outputText returns the value of an output: a plain string, or the text of
// an i18n object in the current language; "" when there is none (an output
// shown once that was already read).
func outputText(o vertracloud.TemplateDeployOutput) string {
	if s, ok := o.Text(); ok {
		return s
	}
	if len(o.Value) > 0 && o.Value[0] == '{' {
		var t vertracloud.TemplateI18nText
		if json.Unmarshal(o.Value, &t) == nil {
			return I18n(t)
		}
	}
	return ""
}
