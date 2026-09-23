package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vertracloud/sdk-api-go/vertracloud"
)

func Render(w io.Writer, value any, command, locale string) {
	RenderPaged(w, value, command, locale, 0, true)
}

// RenderPaged prints value for humans. command is the raw argument list joined
// by spaces; it selects columns, labels and success messages.
func RenderPaged(w io.Writer, value any, command, locale string, pageSize int, all bool) {
	if presentation, ok := value.(Presentation); ok {
		value = presentation.Human
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		fmt.Fprintln(w, value)
		return
	}
	var data any
	if json.Unmarshal(encoded, &data) != nil {
		fmt.Fprintln(w, value)
		return
	}
	cmd := Normalize(command)
	data = localized(data, locale, strings.HasPrefix(command, "db credentials reset password"))
	kind := "app"
	if strings.HasPrefix(cmd, "db") {
		kind = "db"
	}
	switch cmd {
	case "auth login":
		if account, ok := data.(map[string]any); ok {
			Success(w, fmt.Sprintf(text(locale, "signed_in"), Bold(fmt.Sprint(account["name"]))))
			return
		}
	case "auth whoami":
		if account, ok := data.(map[string]any); ok {
			whoami(w, account, locale)
			return
		}
	case "doctor":
		if result, ok := data.(map[string]any); ok {
			doctor(w, result, locale)
			return
		}
	case "status":
		if result, ok := data.(map[string]any); ok {
			label, color := statusWord(result, locale)
			if message, _ := result["message"].(string); message != "" {
				label = "● " + message
			}
			fmt.Fprintln(w, color(label))
			return
		}
	case "link show":
		if data == nil {
			fmt.Fprintln(w, Dim(text(locale, "not_linked")))
			return
		}
	}
	if message := actionMessage(cmd, locale); message != "" || (trivial(data) && mutation(cmd)) {
		if message == "" {
			message = text(locale, "ok")
		}
		Success(w, message)
		if trivial(data) {
			return
		}
		fmt.Fprintln(w)
		if m, ok := data.(map[string]any); ok {
			detail(w, m, kind, locale, "  ")
			if id, _ := m["id"].(string); id != "" && (cmd == "app upload" || cmd == "db create") {
				section := map[string]string{"app": "apps", "db": "databases"}[kind]
				fmt.Fprintf(w, "\n  %s %s\n", Dim("→"), Cyan("https://vertracloud.app/dashboard/"+section+"/"+id))
			}
			return
		}
	}
	switch v := data.(type) {
	case nil:
		fmt.Fprintln(w, Dim(text(locale, "empty")))
	case string:
		fmt.Fprintln(w, v)
	case []any:
		if len(v) == 0 {
			fmt.Fprintln(w, Dim(text(locale, "empty")))
			return
		}
		if pageSize > 0 && !all && stdoutTTY() && len(v) > pageSize {
			renderPages(w, v, cmd, kind, locale, pageSize)
		} else {
			table(w, v, cmd, kind, locale)
			listFooter(w, v, cmd, locale)
		}
	case map[string]any:
		detail(w, v, kind, locale, "")
	default:
		fmt.Fprintln(w, v)
	}
}

// Resource is the name and RAM limit shown next to live status numbers.
type Resource struct {
	Name   string
	RAM    int
	Shield *vertracloud.ApplicationShieldCooldown // nil when not paused
}

// WithResources keeps the status payload as the JSON output and, for humans,
// adds each resource's name and RAM limit (the status routes return neither).
func WithResources(status any, resources map[string]Resource) Presentation {
	encoded, _ := json.Marshal(status)
	var human any
	_ = json.Unmarshal(encoded, &human)
	decorate := func(value any) {
		if row, ok := value.(map[string]any); ok {
			if resource, ok := resources[fmt.Sprint(row["id"])]; ok {
				row["name"], row["ram_limit"] = resource.Name, float64(resource.RAM)
				if resource.Shield != nil {
					encoded, _ := json.Marshal(resource.Shield)
					var shield any
					_ = json.Unmarshal(encoded, &shield)
					row["shield_cooldown"] = shield
				}
			}
		}
	}
	if list, ok := human.([]any); ok {
		for _, item := range list {
			decorate(item)
		}
	} else {
		decorate(human)
	}
	return Presentation{JSON: status, Human: human}
}

// Normalize folds aliases into a canonical "group action [sub]" key, e.g.
// "apps ls --all" → "app list" and "deploy abc" → "app commit".
func Normalize(command string) string {
	var words []string
	show := false
	for _, word := range strings.Fields(command) {
		if word == "--show" {
			show = true
		}
		if !strings.HasPrefix(word, "-") {
			words = append(words, word)
		}
	}
	if len(words) == 0 {
		return ""
	}
	group := map[string]string{"projects": "projects", "project": "projects", "proj": "projects", "apps": "app", "database": "db", "ws": "workspace", "snapshots": "snapshot", "fav": "favorite", "language": "lang", "locale": "lang"}[words[0]]
	if group == "" {
		group = words[0]
	}
	switch group {
	case "profile", "me":
		return "auth whoami"
	case "projects":
		return "projects"
	case "upload", "commit", "push", "deploy", "logs":
		words = append([]string{"app"}, words...)
		group = "app"
	case "link":
		if show {
			return "link show"
		}
		return "link"
	case "app", "db", "workspace", "snapshot", "folder", "favorite", "auth":
	default:
		return group
	}
	if len(words) < 2 {
		return group
	}
	action := words[1]
	if canonical := map[string]string{"ls": "list", "get": "info", "show": "info", "remove": "delete", "rm": "delete", "push": "commit", "deploy": "commit", "create": "create"}[action]; canonical != "" {
		action = canonical
	}
	if group == "app" && action == "create" {
		action = "upload"
	}
	switch action {
	case "env", "file", "network", "credentials", "member", "role", "invite":
		if len(words) > 2 {
			sub := words[2]
			if canonical := map[string]string{"ls": "list", "remove": "delete", "rm": "delete", "unset": "delete", "mv": "move", "rename": "move"}[sub]; canonical != "" {
				sub = canonical
			}
			return group + " " + action + " " + sub
		}
	}
	return group + " " + action
}

var listColumns = map[string][]string{
	"projects":       {"name", "id", "kind", "status", "cpu", "ram", "runtime", "folder"},
	"app list":       {"name", "id", "status", "cpu", "ram", "language", "domain", "folder"},
	"db list":        {"name", "id", "type", "status", "cpu", "ram", "folder"},
	"app status":     {"name", "id", "status", "cpu", "ram", "uptime"},
	"db status":      {"name", "id", "status", "cpu", "ram", "storage", "uptime"},
	"workspace list": {"name", "id", "members_count", "created_at"},
	"snapshot list":  {"id", "resource_name", "size", "date"},
	"folder list":    {"name", "id", "color", "resources"},
	"favorite list":  {"resource_type", "resource_id", "created_at"},
	"app env list":   {"key", "value", "note"},
	"app file list":  {"name", "type", "last_modified"},
}

// hidden fields are internal bookkeeping that only add noise for humans.
var hidden = map[string]bool{
	"owner_id": true, "owner_plan_id": true,
	"deleted_at": true, "resource_organization": true, "position": true, "installing": true, "running": true,
	"favorite": true, "user_id": true, "workspace_id": true, "cluster": true, "author_id": true, "is_owner": true, "folder_color": true, "ram_used": true, "ram_limit": true,
}

func table(w io.Writer, values []any, cmd, kind, locale string) {
	first, ok := values[0].(map[string]any)
	if !ok {
		for _, value := range values {
			fmt.Fprintln(w, value)
		}
		return
	}
	var keys []string
	for _, key := range listColumns[cmd] {
		if _, ok := first[key]; ok || key == "domain" || (key == "status" && first["running"] != nil) {
			keys = append(keys, key)
		}
	}
	if len(keys) < 2 {
		keys = orderedKeys(first, true)
	}
	type styled struct {
		text  string
		color func(string) string
	}
	rows := make([][]styled, 0, len(values))
	for _, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		row := make([]styled, len(keys))
		for i, key := range keys {
			t, c := cell(item, key, kind, locale)
			row[i] = styled{t, c}
		}
		rows = append(rows, row)
	}
	// Drop columns that carry nothing in any row (e.g. no app is in a folder).
	var visible []int
	for i, key := range keys {
		for _, row := range rows {
			if row[i].text != "-" || key == "id" || key == "name" {
				visible = append(visible, i)
				break
			}
		}
	}
	widths := make(map[int]int)
	for _, i := range visible {
		widths[i] = utf8.RuneCountInString(header(locale, keys[i]))
		for _, row := range rows {
			widths[i] = max(widths[i], utf8.RuneCountInString(row[i].text))
		}
	}
	pad := func(s string, width int) string {
		return s + strings.Repeat(" ", max(0, width-utf8.RuneCountInString(s)))
	}
	right := map[string]bool{"ram": true, "cpu": true, "size": true, "storage": true, "members_count": true, "resources": true}
	align := func(i int, s string) string {
		if right[keys[i]] {
			return strings.Repeat(" ", max(0, widths[i]-utf8.RuneCountInString(s))) + s
		}
		return pad(s, widths[i])
	}
	line := make([]string, 0, len(visible))
	for _, i := range visible {
		line = append(line, paint("1;2", align(i, header(locale, keys[i]))))
	}
	fmt.Fprintln(w, strings.TrimRight(strings.Join(line, "  "), " "))
	for _, row := range rows {
		line = line[:0]
		for n, i := range visible {
			s := row[i].text
			if n < len(visible)-1 || right[keys[i]] {
				s = align(i, s)
			}
			switch {
			case row[i].text == "-":
				s = Dim(s)
			case row[i].color != nil:
				s = row[i].color(s)
			}
			line = append(line, s)
		}
		fmt.Fprintln(w, strings.Join(line, "  "))
	}
}

func header(locale, key string) string { return strings.ToUpper(text(locale, key)) }

func renderPages(w io.Writer, rows []any, cmd, kind, locale string, pageSize int) {
	page := 0
	pages := (len(rows) + pageSize - 1) / pageSize
	for {
		start := page * pageSize
		end := min(start+pageSize, len(rows))
		table(w, rows[start:end], cmd, kind, locale)
		if page == pages-1 {
			return
		}
		fmt.Fprintf(w, "\n%s\n", Dim(fmt.Sprintf("%d/%d · %s", page+1, pages, text(locale, "pagination_next"))))
		key, err := ReadPageKey()
		if err != nil || key == "quit" {
			return
		}
		if key == "next" && page < pages-1 {
			page++
		}
		if key == "prev" && page > 0 {
			page--
		}
	}
}

func detail(w io.Writer, m map[string]any, kind, locale, indent string) {
	if _, ok := m["running"]; ok {
		if s, _ := m["status"].(string); s == "" {
			m["status"] = map[bool]string{true: "up", false: "down"}[m["running"] == true]
		}
	}
	type row struct{ label, value string }
	var rows []row
	for _, key := range orderedKeys(m, false) {
		value, color := cell(m, key, kind, locale)
		if value == "-" && key != "subdomain" && key != "custom_domain" {
			continue
		}
		if color != nil {
			value = color(value)
		}
		rows = append(rows, row{text(locale, key), value})
	}
	width := 0
	for _, r := range rows {
		width = max(width, utf8.RuneCountInString(r.label))
	}
	for _, r := range rows {
		fmt.Fprintf(w, "%s%s%s  %s\n", indent, Dim(r.label), strings.Repeat(" ", width-utf8.RuneCountInString(r.label)), r.value)
	}
	if shieldActive(m) {
		shieldNotice(w, m, locale)
	}
}

var priority = []string{"name", "display_name", "email", "id", "status", "shield_cooldown", "plan", "type", "language", "version", "ram", "cpu", "storage", "network", "uptime", "subdomain", "custom_domain", "host", "port"}

// orderedKeys puts identity and state first, the rest alphabetically and
// timestamps last. Tables also skip nested values that do not fit a cell.
func orderedKeys(m map[string]any, forTable bool) []string {
	rank := func(key string) int {
		for i, p := range priority {
			if p == key {
				return i
			}
		}
		if strings.HasSuffix(key, "_at") || key == "date" || key == "offline_since" || key == "last_snapshot" {
			return 1000
		}
		return 500
	}
	keys := make([]string, 0, len(m))
	for key, value := range m {
		if hidden[key] || (forTable && (strings.HasSuffix(key, "_id") || key == "updated_at")) {
			continue
		}
		if forTable {
			switch value.(type) {
			case map[string]any, []any:
				continue
			}
		}
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if rank(keys[i]) != rank(keys[j]) {
			return rank(keys[i]) < rank(keys[j])
		}
		return keys[i] < keys[j]
	})
	return keys
}

// cell formats one field for humans and optionally returns a color for it.
func cell(item map[string]any, key, kind, locale string) (string, func(string) string) {
	value := item[key]
	switch key {
	case "status":
		return statusWord(item, locale)
	case "name":
		name := fmt.Sprint(value)
		if value == nil {
			name = "-"
		}
		if item["favorite"] == true {
			return "★ " + name, func(s string) string { return Yellow("★") + " " + Bold(strings.TrimPrefix(s, "★ ")) }
		}
		return name, Bold
	case "id":
		if s, ok := value.(string); ok {
			return s, Dim
		}
	case "language":
		if s, ok := value.(string); ok && s != "" {
			return s, languageColor(s)
		}
	case "folder":
		if s, ok := value.(string); ok && s != "" {
			color := folderColor(fmt.Sprint(item["folder_color"]))
			return "■ " + s, func(v string) string { return color("■") + strings.TrimPrefix(v, "■") }
		}
	case "domain":
		for _, field := range []string{"custom_domain", "subdomain"} {
			if s, ok := item[field].(string); ok && s != "" {
				return s, Cyan
			}
		}
		return "-", nil
	case "subdomain", "custom_domain":
		if s, ok := value.(string); ok && s != "" {
			return s, Cyan
		}
		return text(locale, "not_configured"), Dim
	case "ram":
		if n, ok := value.(float64); ok {
			if used, _ := item["ram_used"].(string); used != "" && isUp(item) {
				return strings.TrimSuffix(used, " MB") + " / " + fmt.Sprintf("%.0f MB", n), memoryColor(used, n)
			}
			return fmt.Sprintf("%.0f MB", n), nil
		}
		if used, ok := value.(string); ok {
			if limit, ok := item["ram_limit"].(float64); ok {
				if !isUp(item) {
					return fmt.Sprintf("%.0f MB", limit), nil
				}
				return strings.TrimSuffix(used, " MB") + fmt.Sprintf(" / %.0f MB", limit), memoryColor(used, limit)
			}
		}
	case "shield_cooldown":
		if !shieldActive(item) {
			return "-", nil
		}
		shield := value.(map[string]any)
		out := fmt.Sprintf(text(locale, "shield_until"), shieldUntil(shield, locale))
		if strikes, ok := shield["strikes"].(float64); ok && strikes > 0 {
			out += " · " + fmt.Sprintf(text(locale, "shield_strike"), strikes)
		}
		return out, Red
	case "network":
		if m, ok := value.(map[string]any); ok {
			now, _ := m["now"].(string)
			total, _ := m["total"].(string)
			if now != "" || total != "" {
				return fmt.Sprintf("%s %s · %s %s", text(locale, "net_now"), plain(now, locale), text(locale, "net_total"), plain(total, locale)), nil
			}
		}
	case "cpu":
		if !isUp(item) {
			return "-", nil
		}
	case "uptime":
		if n, ok := value.(float64); ok && n > 0 && (item["status"] == "up" || item["running"] == true) {
			return duration(time.Duration(n) * time.Second), nil
		}
		return "-", nil
	case "type":
		if s, ok := value.(string); ok {
			if s == "directory" {
				return text(locale, "directory"), Cyan
			}
			return text(locale, s), nil
		}
		n, _ := value.(float64)
		if kind == "db" {
			if name := map[float64]string{1: "PostgreSQL", 2: "MongoDB", 3: "Redis", 4: "MySQL"}[n]; name != "" {
				return name, nil
			}
		} else if n == 1 || n == 2 {
			return text(locale, map[float64]string{1: "bot", 2: "website"}[n]), nil
		}
	case "kind":
		if value == "database" {
			return text(locale, "kind_db"), Cyan
		}
		return text(locale, "kind_app"), paint2("35")
	case "runtime":
		if item["kind"] == "database" {
			n, _ := item["type"].(float64)
			return map[float64]string{1: "PostgreSQL", 2: "MongoDB", 3: "Redis", 4: "MySQL"}[n], nil
		}
		if s, ok := value.(string); ok && s != "" {
			return s, languageColor(s)
		}
	case "resource_type":
		switch value {
		case float64(1), "application":
			return text(locale, "application"), nil
		case float64(2), "database":
			return text(locale, "database"), nil
		}
	case "auto_restart":
		return text(locale, map[bool]string{true: "enabled", false: "disabled"}[value == true]), nil
	case "resources":
		if list, ok := value.([]any); ok {
			return fmt.Sprint(len(list)), nil
		}
	case "color":
		if s, ok := value.(string); ok {
			return s, nil
		}
	case "value":
		if s, ok := value.(string); ok && s != "" {
			return s, nil
		}
	}
	return plain(value, locale), nil
}

func plain(value any, locale string) string {
	switch v := value.(type) {
	case nil:
		return "-"
	case string:
		if strings.TrimSpace(v) == "" {
			return "-"
		}
		return v
	case bool:
		return text(locale, map[bool]string{true: "yes", false: "no"}[v])
	case float64:
		return fmt.Sprint(v)
	case []any:
		if len(v) == 0 {
			return "-"
		}
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, plain(item, locale))
		}
		limit := 4
		if len(parts) > limit {
			return strings.Join(parts[:limit], ", ") + fmt.Sprintf(" +%d", len(parts)-limit)
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		for _, key := range []string{"display_name", "name", "key", "resource_id"} {
			if s, ok := v[key].(string); ok && s != "" {
				return s
			}
		}
		var parts []string
		for _, key := range orderedKeys(v, false) {
			if s := plain(v[key], locale); s != "-" {
				parts = append(parts, s)
			}
		}
		if len(parts) == 0 {
			return "-"
		}
		return strings.Join(parts, " / ")
	}
	return fmt.Sprint(value)
}

// memoryColor warns in yellow from 80% of the limit and in red from 95%.
func memoryColor(used string, limit float64) func(string) string {
	n, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(used, "MB")), 64)
	if err != nil || limit <= 0 {
		return nil
	}
	switch ratio := n / limit; {
	case ratio >= 0.95:
		return Red
	case ratio >= 0.8:
		return Yellow
	}
	return nil
}

func shieldActive(item map[string]any) bool {
	shield, ok := item["shield_cooldown"].(map[string]any)
	if !ok {
		return false
	}
	until, err := time.Parse(time.RFC3339Nano, fmt.Sprint(shield["until"]))
	return err == nil && time.Now().Before(until)
}

func shieldUntil(shield map[string]any, locale string) string {
	until, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(shield["until"]))
	return fmt.Sprintf("%s (%s)", until.Local().Format("15:04"), duration(time.Until(until).Round(time.Second)))
}

// shieldNotice explains a Shield pause the way the dashboard banner does.
func shieldNotice(w io.Writer, item map[string]any, locale string) {
	shield := item["shield_cooldown"].(map[string]any)
	reason := text(locale, fmt.Sprintf("shield_reason_%v_%v", shield["reason"], shield["direction"]))
	if strings.HasPrefix(reason, "Shield reason") {
		reason = text(locale, "shield_title")
	}
	fmt.Fprintf(w, "\n%s %s\n  %s\n", Red("⛨"), Bold(text(locale, "shield_title")), reason)
	fmt.Fprintf(w, "  %s\n", Dim(text(locale, "shield_ram_hint")))
}

// Text returns a CLI string in the current locale.
func Text(key string) string { return text(Locale, key) }

func isUp(item map[string]any) bool {
	return item["running"] == true || item["status"] == "up"
}

func statusWord(item map[string]any, locale string) (string, func(string) string) {
	if shieldActive(item) {
		return "⛨ shield", Red
	}
	if item["installing"] == true {
		return "● " + text(locale, "installing"), Yellow
	}
	s, _ := item["status"].(string)
	if s == "" {
		if running, ok := item["running"].(bool); ok {
			s = map[bool]string{true: "up", false: "down"}[running]
		}
	}
	switch s {
	case "up", "running", "healthy", "online":
		return "● " + text(locale, "online"), Green
	case "down", "stopped", "offline", "exited":
		return "○ " + text(locale, "offline"), Red
	case "":
		return "-", nil
	}
	return "● " + s, Yellow
}

// languageColor tints each runtime with its usual brand color (256-color ANSI).
func languageColor(language string) func(string) string {
	code := map[string]string{
		"javascript": "38;5;220", "typescript": "38;5;33", "python": "38;5;75", "java": "38;5;208",
		"go": "38;5;45", "golang": "38;5;45", "rust": "38;5;173", "php": "38;5;104", "ruby": "38;5;160",
		"elixir": "38;5;97", "csharp": "38;5;71", "dotnet": "38;5;99", "static": "38;5;250", "bun": "38;5;223", "deno": "38;5;252",
	}[strings.ToLower(language)]
	if code == "" {
		return nil
	}
	return func(s string) string { return paint(code, s) }
}

func paint2(code string) func(string) string { return func(s string) string { return paint(code, s) } }

func folderColor(color string) func(string) string {
	code := map[string]string{"red": "31", "orange": "38;5;208", "yellow": "33", "green": "32", "blue": "34", "purple": "35"}[color]
	if code == "" {
		return Dim
	}
	return func(s string) string { return paint(code, s) }
}

// listFooter summarizes resource lists, e.g. "8 applications · 2 online".
func listFooter(w io.Writer, rows []any, cmd, locale string) {
	defer func() {
		paused := 0
		for _, row := range rows {
			if item, ok := row.(map[string]any); ok && shieldActive(item) {
				paused++
			}
		}
		if paused > 0 {
			fmt.Fprintf(w, "%s %s\n", Red("⛨"), fmt.Sprintf(text(locale, "shield_list"), paused))
		}
	}()
	if cmd == "projects" {
		counts := map[any]int{}
		online := 0
		for _, row := range rows {
			if item, ok := row.(map[string]any); ok {
				counts[item["kind"]]++
				if item["status"] == "up" {
					online++
				}
			}
		}
		noun := func(n int, one, many string) string {
			if n == 1 {
				return fmt.Sprintf("%d %s", n, strings.ToLower(text(locale, one)))
			}
			return fmt.Sprintf("%d %s", n, strings.ToLower(text(locale, many)))
		}
		fmt.Fprintf(w, "\n%s\n", Dim(noun(counts["application"], "application", "applications")+" · "+noun(counts["database"], "database", "databases")+fmt.Sprintf(" · %d online", online)))
		return
	}
	if cmd != "app list" && cmd != "db list" {
		return
	}
	online := 0
	for _, row := range rows {
		if item, ok := row.(map[string]any); ok && item["status"] == "up" {
			online++
		}
	}
	noun := map[string]string{"app list": "applications", "db list": "databases"}[cmd]
	if len(rows) == 1 {
		noun = map[string]string{"app list": "application", "db list": "database"}[cmd]
	}
	fmt.Fprintf(w, "\n%s\n", Dim(fmt.Sprintf("%d %s · %d online", len(rows), strings.ToLower(text(locale, noun)), online)))
}

func duration(d time.Duration) string {
	days, hours, minutes := int(d.Hours())/24, int(d.Hours())%24, int(d.Minutes())%60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

func whoami(w io.Writer, account map[string]any, locale string) {
	summary := map[string]any{"name": account["name"], "email": account["email"]}
	if plan, ok := account["plan"].(map[string]any); ok {
		summary["plan"] = plan["name"]
	}
	for _, key := range []string{"applications", "databases"} {
		if list, ok := account[key].([]any); ok {
			summary[key] = float64(len(list))
		}
	}
	detail(w, summary, "app", locale, "")
}

func doctor(w io.Writer, result map[string]any, locale string) {
	checks, _ := result["checks"].([]any)
	for _, raw := range checks {
		check, _ := raw.(map[string]any)
		message := fmt.Sprint(check["message"])
		switch check["level"] {
		case "pass":
			fmt.Fprintf(w, "%s %s\n", Green("✓"), message)
		case "warn":
			fmt.Fprintf(w, "%s %s\n", Yellow("!"), message)
		default:
			fmt.Fprintf(w, "%s %s\n", Red("✗"), message)
		}
	}
	if summary, ok := result["summary"].(map[string]any); ok {
		fmt.Fprintf(w, "\n%s\n", Dim(fmt.Sprintf(text(locale, "doctor_summary"), summary["passed"], summary["warnings"], summary["errors"])))
	}
}

// trivial reports payloads that only confirm an action happened.
func trivial(data any) bool {
	switch v := data.(type) {
	case nil, string, bool:
		return true
	case map[string]any:
		for key := range v {
			if key != "status" && key != "ok" && key != "id" && key != "logged_out" {
				return false
			}
		}
		return true
	}
	return false
}

func mutation(cmd string) bool {
	parts := strings.Fields(cmd)
	switch parts[len(parts)-1] {
	case "create", "update", "delete", "add", "start", "stop", "restart", "set", "write", "move", "publish",
		"unpublish", "purge-cache", "domain", "subdomain", "reset", "restore", "upload", "commit", "config", "revoke", "accept":
		return true
	}
	return false
}

var actionMessages = map[string][3]string{ // pt, en, es
	"app start":        {"Aplicação iniciada.", "Application started.", "Aplicación iniciada."},
	"app stop":         {"Aplicação parada.", "Application stopped.", "Aplicación detenida."},
	"app restart":      {"Aplicação reiniciada.", "Application restarted.", "Aplicación reiniciada."},
	"app delete":       {"Aplicação excluída.", "Application deleted.", "Aplicación eliminada."},
	"app upload":       {"Aplicação enviada.", "Application uploaded.", "Aplicación subida."},
	"app commit":       {"Arquivos enviados.", "Files uploaded.", "Archivos subidos."},
	"app config":       {"Configuração atualizada.", "Configuration updated.", "Configuración actualizada."},
	"app download":     {"Download concluído.", "Download complete.", "Descarga completada."},
	"app env set":      {"Variáveis salvas.", "Variables saved.", "Variables guardadas."},
	"app env delete":   {"Variáveis removidas.", "Variables removed.", "Variables eliminadas."},
	"db create":        {"Banco de dados criado.", "Database created.", "Base de datos creada."},
	"db start":         {"Banco de dados iniciado.", "Database started.", "Base de datos iniciada."},
	"db stop":          {"Banco de dados parado.", "Database stopped.", "Base de datos detenida."},
	"db delete":        {"Banco de dados excluído.", "Database deleted.", "Base de datos eliminada."},
	"db reset":         {"Banco de dados redefinido.", "Database reset.", "Base de datos restablecida."},
	"snapshot create":  {"Snapshot criado.", "Snapshot created.", "Snapshot creado."},
	"snapshot restore": {"Snapshot restaurado.", "Snapshot restored.", "Snapshot restaurado."},
	"link":             {"Pasta vinculada à aplicação.", "Directory linked to the application.", "Carpeta vinculada a la aplicación."},
	"unlink":           {"Vínculo removido.", "Link removed.", "Vínculo eliminado."},
	"auth logout":      {"Sessão encerrada.", "Logged out.", "Sesión cerrada."},
	"zip":              {"Arquivo compactado.", "Archive created.", "Archivo comprimido."},
}

func actionMessage(cmd, locale string) string {
	messages, ok := actionMessages[cmd]
	if !ok {
		return ""
	}
	return messages[map[string]int{"pt": 0, "en": 1, "es": 2}[locale]]
}

func localized(value any, locale string, showResetPassword bool) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if sensitive(key) && !(showResetPassword && key == "password") {
				v[key] = "[redacted]"
				continue
			}
			if strings.HasSuffix(key, "_at") || key == "offline_since" || key == "last_snapshot" || key == "date" || key == "last_modified" {
				if raw, ok := item.(string); ok {
					v[key] = formatDate(raw, locale)
					continue
				}
			}
			v[key] = localized(item, locale, showResetPassword)
		}
	case []any:
		for i := range v {
			v[i] = localized(v[i], locale, showResetPassword)
		}
	}
	return value
}

func sensitive(key string) bool {
	key = strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
	switch key {
	case "password", "token", "apikey", "secret", "privatekey", "certificate", "cert":
		return true
	}
	return false
}

func formatDate(value, locale string) string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	layout := "Jan 2, 2006 3:04 PM"
	if locale == "pt" || locale == "es" {
		layout = "02/01/2006 15:04"
	}
	return parsed.Local().Format(layout)
}

var translations = map[string]map[string]string{
	"pt": {
		"empty": "Nenhum resultado.", "status": "Estado", "id": "ID", "name": "Nome", "created_at": "Criado em", "updated_at": "Atualizado em",
		"last_snapshot": "Último snapshot", "offline_since": "Offline desde", "cpu": "CPU", "ram": "Memória", "storage": "Armazenamento",
		"language": "Linguagem", "type": "Tipo", "description": "Descrição", "domain": "Domínio", "subdomain": "Subdomínio",
		"custom_domain": "Domínio personalizado", "auto_restart": "Reinício automático", "applications": "Aplicações", "databases": "Bancos de dados",
		"email": "E-mail", "locale": "Idioma", "current": "Atual", "available": "Disponíveis", "ok": "Concluído.", "file": "Arquivo",
		"folder": "Pasta", "not_configured": "Não configurado", "enabled": "Ativado", "disabled": "Desativado", "website": "Site", "bot": "Bot",
		"yes": "Sim", "no": "Não", "online": "online", "offline": "offline", "installing": "instalando", "uptime": "Tempo ativo",
		"network": "Rede", "plan": "Plano", "members_count": "Membros", "resource_name": "Recurso", "resource_type": "Tipo",
		"resource_id": "ID do recurso", "size": "Tamanho", "date": "Data", "color": "Cor", "resources": "Recursos", "key": "Chave",
		"value": "Valor", "note": "Nota", "last_modified": "Modificado em", "path": "Caminho", "host": "Host", "port": "Porta",
		"version": "Versão", "main_file": "Arquivo principal", "start_command": "Comando de início", "build_command": "Comando de build",
		"public_url": "URL pública", "bytes": "Bytes", "removed": "Removidas", "application": "Aplicação", "database": "Banco de dados",
		"directory": "Pasta", "message": "Mensagem", "permissions": "Permissões", "members": "Membros", "roles": "Funções", "owner": "Dono",
		"frozen": "Congelado", "user": "Usuário", "username": "Usuário", "password": "Senha", "commit_id": "Commit",
		"net_now": "agora", "net_total": "total", "kind_app": "App", "kind_db": "Banco", "runtime": "Runtime", "kind": "Tipo",
		"shield_cooldown":              "Vertra Shield",
		"shield_title":                 "Pausado pelo Vertra Shield",
		"shield_until":                 "Pausado até %s",
		"shield_strike":                "Ocorrência nº %v",
		"shield_reason_burst_in":       "O Vertra Shield detectou um pico de tráfego de entrada.",
		"shield_reason_burst_out":      "O Vertra Shield detectou um pico de tráfego de saída.",
		"shield_reason_rate_limit_in":  "O Vertra Shield detectou excesso de requisições recebidas.",
		"shield_reason_rate_limit_out": "O Vertra Shield detectou excesso de requisições enviadas.",
		"shield_ram_hint":              "Os limites de requisições e de banda crescem com a memória alocada: se o tráfego é legítimo, aumentar a RAM do projeto aumenta esses limites.",
		"shield_list":                  "%d pausado(s) pelo Vertra Shield. Veja o motivo com: vertra app info <id>",
		"shield_blocked":               "Bloqueado pelo Vertra Shield até a contenção liberar.",
		"signed_in":                    "Login realizado como %s.", "not_linked": "Esta pasta não está vinculada a nenhuma aplicação.",
		"doctor_summary": "%v ok · %v avisos · %v erros", "picker_hint": "↑/↓ navegar · enter selecionar · q sair",
		"pagination_next": "enter/n próxima · p anterior · q sair",
	},
	"es": {
		"empty": "No hay resultados.", "status": "Estado", "id": "ID", "name": "Nombre", "created_at": "Creado", "updated_at": "Actualizado",
		"last_snapshot": "Última instantánea", "offline_since": "Sin conexión desde", "cpu": "CPU", "ram": "Memoria", "storage": "Almacenamiento",
		"language": "Lenguaje", "type": "Tipo", "description": "Descripción", "domain": "Dominio", "subdomain": "Subdominio",
		"custom_domain": "Dominio personalizado", "auto_restart": "Reinicio automático", "applications": "Aplicaciones", "databases": "Bases de datos",
		"email": "Correo", "locale": "Idioma", "current": "Actual", "available": "Disponibles", "ok": "Completado.", "file": "Archivo",
		"folder": "Carpeta", "not_configured": "No configurado", "enabled": "Activado", "disabled": "Desactivado", "website": "Sitio web", "bot": "Bot",
		"yes": "Sí", "no": "No", "online": "activa", "offline": "apagada", "installing": "instalando", "uptime": "Tiempo activo",
		"network": "Red", "plan": "Plan", "members_count": "Miembros", "resource_name": "Recurso", "resource_type": "Tipo",
		"resource_id": "ID del recurso", "size": "Tamaño", "date": "Fecha", "color": "Color", "resources": "Recursos", "key": "Clave",
		"value": "Valor", "note": "Nota", "last_modified": "Modificado", "path": "Ruta", "host": "Host", "port": "Puerto",
		"version": "Versión", "main_file": "Archivo principal", "start_command": "Comando de inicio", "build_command": "Comando de build",
		"public_url": "URL pública", "bytes": "Bytes", "removed": "Eliminadas", "application": "Aplicación", "database": "Base de datos",
		"directory": "Carpeta", "message": "Mensaje", "permissions": "Permisos", "members": "Miembros", "roles": "Roles", "owner": "Dueño",
		"frozen": "Congelado", "user": "Usuario", "username": "Usuario", "password": "Contraseña", "commit_id": "Commit",
		"net_now": "ahora", "net_total": "total", "kind_app": "App", "kind_db": "Base", "runtime": "Runtime", "kind": "Tipo",
		"shield_cooldown":              "Vertra Shield",
		"shield_title":                 "Pausado por Vertra Shield",
		"shield_until":                 "Pausado hasta %s",
		"shield_strike":                "Ocurrencia n.º %v",
		"shield_reason_burst_in":       "Vertra Shield detectó un pico de tráfico de entrada.",
		"shield_reason_burst_out":      "Vertra Shield detectó un pico de tráfico de salida.",
		"shield_reason_rate_limit_in":  "Vertra Shield detectó exceso de solicitudes recibidas.",
		"shield_reason_rate_limit_out": "Vertra Shield detectó exceso de solicitudes enviadas.",
		"shield_ram_hint":              "Los límites de solicitudes y de ancho de banda crecen con la memoria asignada: si el tráfico es legítimo, aumentar la RAM del proyecto eleva esos límites.",
		"shield_list":                  "%d pausado(s) por Vertra Shield. Mira el motivo con: vertra app info <id>",
		"shield_blocked":               "Bloqueado por Vertra Shield hasta que la contención se libere.",
		"signed_in":                    "Sesión iniciada como %s.", "not_linked": "Esta carpeta no está vinculada a ninguna aplicación.",
		"doctor_summary": "%v ok · %v avisos · %v errores", "picker_hint": "↑/↓ navegar · enter seleccionar · q salir",
		"pagination_next": "enter/n siguiente · p anterior · q salir",
	},
	"en": {
		"empty": "No results.", "status": "Status", "id": "ID", "name": "Name", "created_at": "Created", "updated_at": "Updated",
		"last_snapshot": "Last snapshot", "offline_since": "Offline since", "cpu": "CPU", "ram": "Memory", "storage": "Storage",
		"language": "Language", "type": "Type", "description": "Description", "domain": "Domain", "subdomain": "Subdomain",
		"custom_domain": "Custom domain", "auto_restart": "Auto restart", "applications": "Applications", "databases": "Databases",
		"email": "Email", "locale": "Language", "current": "Current", "available": "Available", "ok": "Done.", "file": "File",
		"folder": "Folder", "not_configured": "Not configured", "enabled": "On", "disabled": "Off", "website": "Website", "bot": "Bot",
		"yes": "Yes", "no": "No", "online": "online", "offline": "offline", "installing": "installing", "uptime": "Uptime",
		"network": "Network", "plan": "Plan", "members_count": "Members", "resource_name": "Resource", "resource_type": "Type",
		"resource_id": "Resource ID", "size": "Size", "date": "Date", "color": "Color", "resources": "Resources", "key": "Key",
		"value": "Value", "note": "Note", "last_modified": "Modified", "path": "Path", "host": "Host", "port": "Port",
		"version": "Version", "main_file": "Main file", "start_command": "Start command", "build_command": "Build command",
		"public_url": "Public URL", "bytes": "Bytes", "removed": "Removed", "application": "Application", "database": "Database",
		"directory": "Folder", "message": "Message", "permissions": "Permissions", "members": "Members", "roles": "Roles", "owner": "Owner",
		"frozen": "Frozen", "user": "User", "username": "Username", "password": "Password", "commit_id": "Commit",
		"net_now": "now", "net_total": "total", "kind_app": "App", "kind_db": "Database", "runtime": "Runtime", "kind": "Kind",
		"shield_cooldown":              "Vertra Shield",
		"shield_title":                 "Paused by Vertra Shield",
		"shield_until":                 "Paused until %s",
		"shield_strike":                "Strike #%v",
		"shield_reason_burst_in":       "Vertra Shield detected an inbound traffic spike.",
		"shield_reason_burst_out":      "Vertra Shield detected an outbound traffic spike.",
		"shield_reason_rate_limit_in":  "Vertra Shield detected too many incoming requests.",
		"shield_reason_rate_limit_out": "Vertra Shield detected too many outgoing requests.",
		"shield_ram_hint":              "Request and bandwidth limits scale with the allocated memory: if the traffic is legitimate, increasing the project's RAM raises those limits.",
		"shield_list":                  "%d paused by Vertra Shield. See why with: vertra app info <id>",
		"shield_blocked":               "Blocked by Vertra Shield until the containment clears.",
		"signed_in":                    "Signed in as %s.", "not_linked": "This directory is not linked to an application.",
		"doctor_summary": "%v passed · %v warnings · %v errors", "picker_hint": "↑/↓ move · enter select · q quit",
		"pagination_next": "enter/n next · p previous · q quit",
	},
}

func text(locale, key string) string {
	if translated := translations[locale][key]; translated != "" {
		return translated
	}
	if translated := translations["en"][key]; translated != "" {
		return translated
	}
	words := strings.Fields(strings.ReplaceAll(key, "_", " "))
	for i, word := range words {
		if word == "id" || word == "url" {
			words[i] = strings.ToUpper(word)
		}
	}
	if len(words) == 0 {
		return key
	}
	words[0] = strings.ToUpper(words[0][:1]) + words[0][1:]
	return strings.Join(words, " ")
}
