package stacks

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/vertracloud/cli/internal/templates"
	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// stackView prints a stack with its members and connections.
type stackView struct{ s vertracloud.Stack }

func (v stackView) RenderHuman(w io.Writer, _ string) {
	s := v.s
	fmt.Fprintf(w, "%s\n", ui.Bold(ui.Plain(s.Name)))
	if s.Description != nil && *s.Description != "" {
		fmt.Fprintf(w, "%s\n", ui.Plain(*s.Description))
	}
	fmt.Fprintln(w)
	rows := []templates.KV{
		kv("label_id", s.ID),
		kv("label_status", fmt.Sprintf("%s (%d/%d)", statusWord(s.Status), s.Status.Up, s.Status.Total)),
		kv("label_ram", fmt.Sprintf("%d MB", s.RAMTotal)),
		kv("label_network", string(s.NetworkMode)),
	}
	if label := templateLabel(s.Template); label != "" {
		rows = append(rows, kv("label_template", label))
	}
	if s.UpdateAvailable {
		rows = append(rows, kv("label_update", "vertra stack update "+s.ID))
	}
	templates.PrintKV(w, "", rows)

	names := map[string]string{}
	fmt.Fprintf(w, "\n%s\n", ui.Bold(templates.Text("stack_members")))
	if len(s.Members) == 0 {
		fmt.Fprintf(w, "  %s\n", ui.Dim(templates.Text("stack_none")))
	}
	for _, m := range s.Members {
		names[m.ResourceID] = m.Name
		line := fmt.Sprintf("  %-11s %s", ui.Plain(string(m.ResourceType)), ui.Bold(ui.Plain(m.Name)))
		if m.DatabaseType != nil {
			line += " " + ui.Dim(databaseName(*m.DatabaseType))
		}
		if m.Status != nil {
			line += "  " + ui.Plain(*m.Status)
		}
		if m.RAM > 0 {
			line += "  " + ui.Dim(fmt.Sprintf("%d MB", m.RAM))
		}
		if m.ServiceKey != nil {
			line += "  " + ui.Dim("["+ui.Plain(*m.ServiceKey)+"]")
		}
		fmt.Fprintln(w, line)
	}

	fmt.Fprintf(w, "\n%s\n", ui.Bold(templates.Text("stack_bindings")))
	if len(s.Bindings) == 0 {
		fmt.Fprintf(w, "  %s\n", ui.Dim(templates.Text("stack_none")))
	}
	name := func(id string) string {
		if n := names[id]; n != "" {
			return ui.Plain(n)
		}
		return ui.Plain(id)
	}
	for _, b := range s.Bindings {
		var to string
		switch {
		case b.DatabaseID != nil:
			to = name(*b.DatabaseID)
		case b.BlobID != nil:
			to = name(*b.BlobID)
		case b.TargetAppID != nil:
			to = name(*b.TargetAppID)
		}
		keys := make([]string, 0, len(b.EnvMap))
		for k := range b.EnvMap {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(w, "  %s → %s  %s\n", name(b.AppID), to, ui.Dim(ui.Plain(strings.Join(keys, ", "))))
	}
}

func kv(label, value string) templates.KV {
	return templates.KV{Label: templates.Text(label), Value: value}
}

// updateView prints what updating a stack to the latest template version would do.
type updateView struct {
	i vertracloud.TemplateUpdateInfo
}

func (v updateView) RenderHuman(w io.Writer, _ string) {
	i := v.i
	if !i.UpdateAvailable {
		ui.Success(w, fmt.Sprintf(templates.Text("stack_update_none"), ui.Plain(i.CurrentVersion)))
		return
	}
	latest := ""
	if i.LatestVersion != nil {
		latest = *i.LatestVersion
	}
	fmt.Fprintf(w, "%s\n", ui.Bold(fmt.Sprintf(templates.Text("stack_update_available"), ui.Plain(i.CurrentVersion), ui.Plain(latest))))
	if i.Security {
		fmt.Fprintf(w, "  %s\n", ui.Yellow(templates.Text("stack_update_security")))
	}
	if i.Changelog != nil {
		if log := templates.I18n(*i.Changelog); log != "" {
			fmt.Fprintf(w, "  %s\n", ui.Plain(log))
		}
	}
	if i.OpensPermissions {
		fmt.Fprintf(w, "  %s\n", ui.Yellow(templates.Text("stack_update_permissions")))
	}
	for _, c := range i.RAMChanges {
		fmt.Fprintf(w, "  %s\n", fmt.Sprintf(templates.Text("stack_update_ram"), ui.Plain(c.ServiceID), c.Current, c.RequiredMin))
	}
	if len(i.AddedServices) > 0 {
		ids := make([]string, 0, len(i.AddedServices))
		for _, s := range i.AddedServices {
			ids = append(ids, ui.Plain(s.ID))
		}
		fmt.Fprintf(w, "  %s\n", fmt.Sprintf(templates.Text("stack_update_added"), strings.Join(ids, ", ")))
	}
	if len(i.RemovedServices) > 0 {
		fmt.Fprintf(w, "  %s\n", fmt.Sprintf(templates.Text("stack_update_removed"), ui.Plain(strings.Join(i.RemovedServices, ", "))))
	}
	for _, in := range i.NewInputs {
		fmt.Fprintf(w, "  + %s %s\n", ui.Plain(in.ID), ui.Dim(ui.Plain(templates.I18n(in.Label))))
	}
	if i.Blocked != nil {
		fmt.Fprintf(w, "  %s\n", ui.Red(fmt.Sprintf(templates.Text("stack_update_blocked"), templates.BlockReason(i.Blocked.Reason))))
	}
}

func databaseName(t vertracloud.DatabaseType) string {
	switch t {
	case vertracloud.DatabaseTypePostgreSQL:
		return "PostgreSQL"
	case vertracloud.DatabaseTypeMongoDB:
		return "MongoDB"
	case vertracloud.DatabaseTypeRedis:
		return "Redis"
	case vertracloud.DatabaseTypeMySQL:
		return "MySQL"
	}
	return fmt.Sprint(int(t))
}
