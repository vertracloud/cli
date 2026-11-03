package apps

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

var analyticsText = map[string]map[string]string{
	"pt": {
		"window": "Janela", "hosts": "Hosts", "not_published": "Este app não tem site publicado: não há dados de tráfego.",
		"sampled": "Tráfego alto: os números são estimados a partir de uma amostra.", "stale": "Os números podem estar desatualizados (última atualização %s).",
		"no_traffic": "Nenhuma requisição na janela.", "requests": "Requisições", "visits": "Visitas", "bytes": "Dados enviados",
		"rate4": "Taxa 4xx", "rate5": "Taxa 5xx", "edge_p95": "p95 na borda", "vs_prev": "vs. período anterior", "errors": "Erros", "error_rate": "Taxa de erro",
		"include4": "Contando 4xx como erro.", "only5": "Contando só 5xx (use --include-4xx para somar os 4xx).", "statuses": "Erros por status", "paths": "Rotas com mais erro",
		"value": "Valor", "rate": "Taxa", "metric": "Métrica", "edge": "Borda", "container": "Container", "avg": "média", "countries": "Por país", "edge_locations": "Por colo da borda",
		"slow_paths": "Rotas mais lentas", "path_issues": "Problemas por rota", "path": "Rota", "avg_ms": "Média", "when": "Quando", "method": "Método", "status": "Status",
		"country": "País", "cache": "Cache", "next": "Próxima página (mesmos filtros e ordem): --start %s --end %s --cursor %s", "no_requests": "Nenhuma requisição encontrada.",
		"third_party": "Os campos de caminho, origem e rede vêm de visitantes: tratados como texto.", "visits_col": "Visitas", "bytes_col": "Dados",
		"dim_path": "Rotas", "dim_country": "Países", "dim_referer": "Origens", "dim_status": "Status", "dim_method": "Métodos", "dim_device": "Dispositivos", "dim_os": "Sistemas",
		"dim_browser": "Navegadores", "dim_host": "Hosts", "dim_edge_location": "Colos da borda", "dim_protocol": "Protocolos", "dim_asn": "Redes (ASN)", "dim_ip": "IPs",
		"dim_content_type": "Tipos de conteúdo", "dim_cache_status": "Cache", "dim_bot": "Bots", "no_container": "sem requisições ao container",
	},
	"en": {
		"window": "Window", "hosts": "Hosts", "not_published": "This app has no site published: there is no traffic data.",
		"sampled": "High traffic: the numbers are estimated from a sample.", "stale": "The numbers may be out of date (last updated %s).",
		"no_traffic": "No requests in the window.", "requests": "Requests", "visits": "Visits", "bytes": "Data sent",
		"rate4": "4xx rate", "rate5": "5xx rate", "edge_p95": "Edge p95", "vs_prev": "vs. previous period", "errors": "Errors", "error_rate": "Error rate",
		"include4": "Counting 4xx as errors.", "only5": "Counting 5xx only (use --include-4xx to add 4xx).", "statuses": "Errors by status", "paths": "Paths with the most errors",
		"value": "Value", "rate": "Rate", "metric": "Metric", "edge": "Edge", "container": "Container", "avg": "average", "countries": "By country", "edge_locations": "By edge location",
		"slow_paths": "Slowest paths", "path_issues": "Problems by path", "path": "Path", "avg_ms": "Average", "when": "When", "method": "Method", "status": "Status",
		"country": "Country", "cache": "Cache", "next": "Next page (same filters and order): --start %s --end %s --cursor %s", "no_requests": "No requests found.",
		"third_party": "Path, referer and network fields come from visitors: treated as text.", "visits_col": "Visits", "bytes_col": "Data",
		"dim_path": "Paths", "dim_country": "Countries", "dim_referer": "Referers", "dim_status": "Status", "dim_method": "Methods", "dim_device": "Devices", "dim_os": "Systems",
		"dim_browser": "Browsers", "dim_host": "Hosts", "dim_edge_location": "Edge locations", "dim_protocol": "Protocols", "dim_asn": "Networks (ASN)", "dim_ip": "IPs",
		"dim_content_type": "Content types", "dim_cache_status": "Cache", "dim_bot": "Bots", "no_container": "no request reached the container",
	},
	"es": {
		"window": "Ventana", "hosts": "Hosts", "not_published": "Esta app no tiene sitio publicado: no hay datos de tráfico.",
		"sampled": "Tráfico alto: los números se estiman a partir de una muestra.", "stale": "Los números pueden estar desactualizados (última actualización %s).",
		"no_traffic": "Ninguna solicitud en la ventana.", "requests": "Solicitudes", "visits": "Visitas", "bytes": "Datos enviados",
		"rate4": "Tasa 4xx", "rate5": "Tasa 5xx", "edge_p95": "p95 en el borde", "vs_prev": "vs. período anterior", "errors": "Errores", "error_rate": "Tasa de error",
		"include4": "Contando 4xx como error.", "only5": "Contando solo 5xx (usa --include-4xx para sumar los 4xx).", "statuses": "Errores por status", "paths": "Rutas con más errores",
		"value": "Valor", "rate": "Tasa", "metric": "Métrica", "edge": "Borde", "container": "Contenedor", "avg": "media", "countries": "Por país", "edge_locations": "Por ubicación del borde",
		"slow_paths": "Rutas más lentas", "path_issues": "Problemas por ruta", "path": "Ruta", "avg_ms": "Media", "when": "Cuándo", "method": "Método", "status": "Status",
		"country": "País", "cache": "Caché", "next": "Página siguiente (mismos filtros y orden): --start %s --end %s --cursor %s", "no_requests": "No se encontraron solicitudes.",
		"third_party": "Los campos de ruta, origen y red vienen de visitantes: se tratan como texto.", "visits_col": "Visitas", "bytes_col": "Datos",
		"dim_path": "Rutas", "dim_country": "Países", "dim_referer": "Orígenes", "dim_status": "Status", "dim_method": "Métodos", "dim_device": "Dispositivos", "dim_os": "Sistemas",
		"dim_browser": "Navegadores", "dim_host": "Hosts", "dim_edge_location": "Ubicaciones del borde", "dim_protocol": "Protocolos", "dim_asn": "Redes (ASN)", "dim_ip": "IPs",
		"dim_content_type": "Tipos de contenido", "dim_cache_status": "Caché", "dim_bot": "Bots", "no_container": "ninguna solicitud llegó al contenedor",
	},
}

func atx(locale, key string) string {
	if m, ok := analyticsText[locale]; ok {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return analyticsText["en"][key]
}

// safe strips control and escape sequences: every string below can come from a visitor.
func safe(s string) string { return ui.Plain(s) }

func fmtInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func fmtBytes(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v, i := float64(n), 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

func fmtMs(v float64) string {
	switch {
	case v >= 10000:
		return fmt.Sprintf("%.1f s", v/1000)
	case v >= 100:
		return fmt.Sprintf("%.0f ms", v)
	default:
		return fmt.Sprintf("%.1f ms", v)
	}
}

func fmtMsPtr(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmtMs(*v)
}

func fmtPct(rate float64) string {
	p := rate * 100
	if p > 0 && p < 0.01 {
		return "<0.01%"
	}
	return fmt.Sprintf("%.2f%%", p)
}

// delta says how much a value changed from the previous period.
func delta(cur, prev float64) string {
	if prev == 0 {
		if cur == 0 {
			return "="
		}
		return "new"
	}
	d := (cur - prev) / prev * 100
	if math.Abs(d) < 0.05 {
		return "="
	}
	return fmt.Sprintf("%+.1f%%", d)
}

func fmtTime(t time.Time) string { return t.Local().Format("2006-01-02 15:04") }

func sparkline(values []int64) string {
	if len(values) == 0 {
		return ""
	}
	var max int64
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	out := make([]rune, 0, len(values))
	for _, v := range values {
		i := 0
		if max > 0 {
			i = int(float64(v) / float64(max) * float64(len(blocks)-1))
		}
		out = append(out, blocks[i])
	}
	return string(out)
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// grid prints rows with padded columns. Cells must already be plain text;
// numeric columns are right-aligned.
func grid(w io.Writer, indent string, head []string, rows [][]string, right map[int]bool) {
	widths := make([]int, len(head))
	for i, h := range head {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, r := range rows {
		for i, c := range r {
			if n := utf8.RuneCountInString(c); n > widths[i] {
				widths[i] = n
			}
		}
	}
	line := func(cells []string, style func(string) string) {
		var b strings.Builder
		for i, c := range cells {
			pad := strings.Repeat(" ", widths[i]-utf8.RuneCountInString(c))
			cell := c
			if right[i] {
				cell = pad + c
			} else if i < len(cells)-1 {
				cell = c + pad
			}
			if style != nil {
				cell = style(cell)
			}
			if i > 0 {
				b.WriteString("  ")
			}
			b.WriteString(cell)
		}
		fmt.Fprintln(w, indent+b.String())
	}
	line(head, ui.Dim)
	for _, r := range rows {
		line(r, nil)
	}
}

func kvLine(w io.Writer, label, value string) {
	fmt.Fprintf(w, "  %-22s %s\n", label+":", value)
}

func metaHeader(w io.Writer, locale, title string, m vertracloud.ApplicationAnalyticsMeta, interval vertracloud.ApplicationAnalyticsInterval) bool {
	fmt.Fprintf(w, "%s\n", ui.Bold(title))
	win := fmtTime(m.Start) + " → " + fmtTime(m.End)
	if interval != "" {
		win += " (" + string(interval) + ")"
	}
	kvLine(w, atx(locale, "window"), win)
	if len(m.Hosts) > 0 {
		hosts := make([]string, len(m.Hosts))
		for i, h := range m.Hosts {
			hosts[i] = safe(h)
		}
		kvLine(w, atx(locale, "hosts"), strings.Join(hosts, ", "))
	}
	if !m.WebPublished {
		fmt.Fprintf(w, "\n%s\n", ui.Yellow(atx(locale, "not_published")))
		return false
	}
	if m.Sampled {
		fmt.Fprintf(w, "  %s\n", ui.Dim(atx(locale, "sampled")))
	}
	if m.Stale {
		fmt.Fprintf(w, "  %s\n", ui.Yellow(fmt.Sprintf(atx(locale, "stale"), fmtTime(m.GeneratedAt))))
	}
	return true
}

func section(w io.Writer, title string) { fmt.Fprintf(w, "\n%s\n", ui.Bold(title)) }

func entryName(value string, label *string) string {
	name := safe(value)
	if label != nil && *label != "" {
		name += " (" + safe(*label) + ")"
	}
	return clip(name, 60)
}

// ---- traffic ----

type trafficView struct {
	d vertracloud.ApplicationAnalyticsTraffic
}

func (v trafficView) RenderHuman(w io.Writer, locale string) {
	d := v.d
	if !metaHeader(w, locale, "Analytics", d.ApplicationAnalyticsMeta, d.Interval) {
		return
	}
	if d.Summary == nil {
		fmt.Fprintf(w, "\n%s\n", ui.Dim(atx(locale, "no_traffic")))
		return
	}
	s := d.Summary
	var p *vertracloud.ApplicationAnalyticsTrafficSummary
	if d.Previous != nil {
		p = d.Previous.Summary
	}
	fmt.Fprintln(w)
	cmp := func(cur, prev float64) string {
		if d.Previous == nil {
			return ""
		}
		if p == nil {
			return "  " + ui.Dim(atx(locale, "vs_prev")+": new")
		}
		return "  " + ui.Dim(atx(locale, "vs_prev")+": "+delta(cur, prev))
	}
	var pr, pv, pb, p4, p5, pe float64
	if p != nil {
		pr, pv, pb, p4, p5, pe = float64(p.Requests), float64(p.Visits), float64(p.Bytes), p.ErrorRate4xx, p.ErrorRate5xx, p.EdgeTimeP95Ms
	}
	kvLine(w, atx(locale, "requests"), fmtInt(s.Requests)+cmp(float64(s.Requests), pr))
	kvLine(w, atx(locale, "visits"), fmtInt(s.Visits)+cmp(float64(s.Visits), pv))
	kvLine(w, atx(locale, "bytes"), fmtBytes(s.Bytes)+cmp(float64(s.Bytes), pb))
	kvLine(w, atx(locale, "rate4"), fmtPct(s.ErrorRate4xx)+" ("+fmtInt(s.Errors4xx)+")"+cmp(s.ErrorRate4xx, p4))
	kvLine(w, atx(locale, "rate5"), fmtPct(s.ErrorRate5xx)+" ("+fmtInt(s.Errors5xx)+")"+cmp(s.ErrorRate5xx, p5))
	kvLine(w, atx(locale, "edge_p95"), fmtMs(s.EdgeTimeP95Ms)+cmp(s.EdgeTimeP95Ms, pe))
	if len(d.Series) > 1 {
		pts := make([]int64, len(d.Series))
		for i, pt := range d.Series {
			pts[i] = pt.Requests
		}
		kvLine(w, atx(locale, "requests"), ui.Cyan(sparkline(pts)))
	}
	for _, dim := range analyticsDimensions {
		entries := d.Breakdowns[dim]
		if len(entries) == 0 {
			continue
		}
		if len(entries) > analyticsTopN {
			entries = entries[:analyticsTopN]
		}
		section(w, atx(locale, "dim_"+string(dim)))
		rows := make([][]string, 0, len(entries))
		for _, e := range entries {
			rows = append(rows, []string{entryName(e.Value, e.Label), fmtInt(e.Requests), fmtInt(e.Visits), fmtBytes(e.Bytes)})
		}
		grid(w, "  ", []string{atx(locale, "value"), atx(locale, "requests"), atx(locale, "visits_col"), atx(locale, "bytes_col")}, rows, map[int]bool{1: true, 2: true, 3: true})
	}
}

// ---- errors ----

type errorsView struct {
	d vertracloud.ApplicationAnalyticsErrors
}

func (v errorsView) RenderHuman(w io.Writer, locale string) {
	d := v.d
	if !metaHeader(w, locale, atx(locale, "errors"), d.ApplicationAnalyticsMeta, d.Interval) {
		return
	}
	if d.Include4xx {
		kvLine(w, "", ui.Dim(atx(locale, "include4")))
	} else {
		kvLine(w, "", ui.Dim(atx(locale, "only5")))
	}
	if d.Summary == nil {
		fmt.Fprintf(w, "\n%s\n", ui.Dim(atx(locale, "no_traffic")))
		return
	}
	s := d.Summary
	var p *vertracloud.ApplicationAnalyticsErrorsSummary
	if d.Previous != nil {
		p = d.Previous.Summary
	}
	fmt.Fprintln(w)
	cmp := func(cur, prev float64) string {
		if d.Previous == nil {
			return ""
		}
		if p == nil {
			return "  " + ui.Dim(atx(locale, "vs_prev")+": new")
		}
		return "  " + ui.Dim(atx(locale, "vs_prev")+": "+delta(cur, prev))
	}
	var pr, pe, prate float64
	if p != nil {
		pr, pe, prate = float64(p.Requests), float64(p.Errors), p.ErrorRate
	}
	kvLine(w, atx(locale, "requests"), fmtInt(s.Requests)+cmp(float64(s.Requests), pr))
	kvLine(w, atx(locale, "errors"), fmtInt(s.Errors)+cmp(float64(s.Errors), pe))
	kvLine(w, atx(locale, "error_rate"), fmtPct(s.ErrorRate)+cmp(s.ErrorRate, prate))
	kvLine(w, "4xx / 5xx", fmtInt(s.Errors4xx)+" / "+fmtInt(s.Errors5xx))
	if len(d.Series) > 1 {
		pts := make([]int64, len(d.Series))
		for i, pt := range d.Series {
			pts[i] = pt.Errors5xx
			if d.Include4xx {
				pts[i] += pt.Errors4xx
			}
		}
		kvLine(w, atx(locale, "errors"), ui.Red(sparkline(pts)))
	}
	if len(d.Statuses) > 0 {
		section(w, atx(locale, "statuses"))
		rows := make([][]string, 0, len(d.Statuses))
		for _, e := range d.Statuses {
			rows = append(rows, []string{safe(e.Value), fmtInt(e.Requests)})
		}
		grid(w, "  ", []string{atx(locale, "status"), atx(locale, "requests")}, rows, map[int]bool{1: true})
	}
	if len(d.Paths) > 0 {
		section(w, atx(locale, "paths"))
		rows := make([][]string, 0, len(d.Paths))
		for _, e := range d.Paths {
			rows = append(rows, []string{clip(safe(e.Value), 60), fmtInt(e.Errors), fmtInt(e.Requests), fmtPct(e.ErrorRate)})
		}
		grid(w, "  ", []string{atx(locale, "path"), atx(locale, "errors"), atx(locale, "requests"), atx(locale, "rate")}, rows, map[int]bool{1: true, 2: true, 3: true})
	}
}

// ---- performance ----

type performanceView struct {
	d vertracloud.ApplicationAnalyticsPerformance
}

func latencyRows(edge vertracloud.ApplicationAnalyticsLatency, container *vertracloud.ApplicationAnalyticsLatency, prev *vertracloud.ApplicationAnalyticsPerformanceSummary, locale string) [][]string {
	type pick struct {
		name string
		get  func(vertracloud.ApplicationAnalyticsLatency) float64
	}
	picks := []pick{
		{atx(locale, "avg"), func(l vertracloud.ApplicationAnalyticsLatency) float64 { return l.AvgMs }},
		{"p50", func(l vertracloud.ApplicationAnalyticsLatency) float64 { return l.P50Ms }},
		{"p75", func(l vertracloud.ApplicationAnalyticsLatency) float64 { return l.P75Ms }},
		{"p90", func(l vertracloud.ApplicationAnalyticsLatency) float64 { return l.P90Ms }},
		{"p95", func(l vertracloud.ApplicationAnalyticsLatency) float64 { return l.P95Ms }},
		{"p99", func(l vertracloud.ApplicationAnalyticsLatency) float64 { return l.P99Ms }},
		{"p99.9", func(l vertracloud.ApplicationAnalyticsLatency) float64 { return l.P999Ms }},
	}
	rows := make([][]string, 0, len(picks))
	for _, p := range picks {
		row := []string{p.name, fmtMs(p.get(edge)), "-"}
		if container != nil {
			row[2] = fmtMs(p.get(*container))
		}
		if prev != nil {
			row = append(row, delta(p.get(edge), p.get(prev.Edge)))
		}
		rows = append(rows, row)
	}
	return rows
}

func latencyEntryRows(entries []vertracloud.ApplicationAnalyticsLatencyEntry) [][]string {
	if len(entries) > analyticsTopN {
		entries = entries[:analyticsTopN]
	}
	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		cp95, cp99 := "-", "-"
		if e.Container != nil {
			cp95, cp99 = fmtMs(e.Container.P95Ms), fmtMs(e.Container.P99Ms)
		}
		rows = append(rows, []string{entryName(e.Value, e.Label), fmtInt(e.Requests), fmtMs(e.Edge.P50Ms), fmtMs(e.Edge.P95Ms), fmtMs(e.Edge.P99Ms), cp95, cp99})
	}
	return rows
}

func (v performanceView) RenderHuman(w io.Writer, locale string) {
	d := v.d
	if !metaHeader(w, locale, "Performance", d.ApplicationAnalyticsMeta, d.Interval) {
		return
	}
	if d.Summary == nil {
		fmt.Fprintf(w, "\n%s\n", ui.Dim(atx(locale, "no_traffic")))
		return
	}
	var prev *vertracloud.ApplicationAnalyticsPerformanceSummary
	if d.Previous != nil {
		prev = d.Previous.Summary
	}
	fmt.Fprintln(w)
	kvLine(w, atx(locale, "requests"), fmtInt(d.Summary.Requests))
	if d.Summary.Container == nil {
		kvLine(w, atx(locale, "container"), ui.Dim(atx(locale, "no_container")))
	}
	fmt.Fprintln(w)
	head := []string{atx(locale, "metric"), atx(locale, "edge"), atx(locale, "container")}
	right := map[int]bool{1: true, 2: true}
	if prev != nil {
		head = append(head, atx(locale, "vs_prev"))
		right[3] = true
	}
	grid(w, "  ", head, latencyRows(d.Summary.Edge, d.Summary.Container, prev, locale), right)
	if len(d.Series) > 1 {
		pts := make([]int64, len(d.Series))
		for i, pt := range d.Series {
			pts[i] = int64(pt.Edge.P95Ms)
		}
		kvLine(w, atx(locale, "edge")+" p95", ui.Cyan(sparkline(pts)))
	}
	latHead := []string{atx(locale, "value"), atx(locale, "requests"), "p50", "p95", "p99", atx(locale, "container") + " p95", atx(locale, "container") + " p99"}
	latRight := map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true, 6: true}
	for _, s := range []struct {
		title   string
		entries []vertracloud.ApplicationAnalyticsLatencyEntry
	}{{"slow_paths", d.SlowPaths}, {"countries", d.Countries}, {"edge_locations", d.EdgeLocations}} {
		if len(s.entries) == 0 {
			continue
		}
		section(w, atx(locale, s.title)+" ("+atx(locale, "edge")+")")
		grid(w, "  ", latHead, latencyEntryRows(s.entries), latRight)
	}
	if len(d.PathIssues) > 0 {
		section(w, atx(locale, "path_issues"))
		issues := d.PathIssues
		if len(issues) > analyticsTopN {
			issues = issues[:analyticsTopN]
		}
		rows := make([][]string, 0, len(issues))
		for _, e := range issues {
			rows = append(rows, []string{clip(safe(e.Path), 60), fmtInt(e.Requests), fmtPct(e.ErrorRate), fmtMs(e.AvgMs), fmtMs(e.P99Ms)})
		}
		grid(w, "  ", []string{atx(locale, "path"), atx(locale, "requests"), atx(locale, "error_rate"), atx(locale, "avg_ms"), "p99"}, rows, map[int]bool{1: true, 2: true, 3: true, 4: true})
	}
}

// ---- requests ----

type requestsView struct {
	d vertracloud.ApplicationAnalyticsRequestList
}

func (v requestsView) RenderHuman(w io.Writer, locale string) {
	d := v.d
	if !metaHeader(w, locale, atx(locale, "requests"), d.ApplicationAnalyticsMeta, "") {
		return
	}
	if len(d.Requests) == 0 {
		fmt.Fprintf(w, "\n%s\n", ui.Dim(atx(locale, "no_requests")))
		return
	}
	fmt.Fprintln(w)
	rows := make([][]string, 0, len(d.Requests))
	for _, r := range d.Requests {
		target := r.Path
		if r.Query != "" {
			target += "?" + r.Query
		}
		status := strconv.Itoa(r.Status)
		if r.ContainerStatus != nil && *r.ContainerStatus != r.Status {
			status += "/" + strconv.Itoa(*r.ContainerStatus)
		}
		rows = append(rows, []string{
			r.Time.Local().Format("01-02 15:04:05"), safe(r.Method), status, clip(safe(target), 50),
			fmtMs(r.EdgeTimeMs), fmtMsPtr(r.ContainerTimeMs), safe(r.Country), safe(r.CacheStatus), safe(r.RequestID),
		})
	}
	grid(w, "  ", []string{atx(locale, "when"), atx(locale, "method"), atx(locale, "status"), atx(locale, "path"), atx(locale, "edge"), atx(locale, "container"), atx(locale, "country"), atx(locale, "cache"), "request_id"}, rows, map[int]bool{4: true, 5: true})
	fmt.Fprintf(w, "\n  %s\n", ui.Dim(atx(locale, "third_party")))
	if d.Cursor != nil && *d.Cursor != "" {
		fmt.Fprintf(w, "  %s\n", ui.Dim(fmt.Sprintf(atx(locale, "next"), d.Start.UTC().Format(time.RFC3339), d.End.UTC().Format(time.RFC3339), safe(*d.Cursor))))
	}
}
