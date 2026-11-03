package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const analyticsMeta = `"start":"2026-11-01T00:00:00Z","end":"2026-11-02T00:00:00Z","hosts":["demo.vertraweb.app"],"web_published":true,"sampled":false,"stale":false,"generated_at":"2026-11-02T00:01:00Z","history_days":7`

// analyticsServer answers the four analytics routes and records the last query.
func analyticsServer(t *testing.T, status int, body map[string]string) (*url.Values, func()) {
	t.Helper()
	last := &url.Values{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*last = r.URL.Query()
		payload, ok := body[r.URL.Path]
		if !ok {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(payload))
	}))
	old := apiBaseURL
	apiBaseURL = server.URL
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VERTRA_API_KEY", "local-key")
	return last, func() { apiBaseURL = old; server.Close() }
}

func runCLI(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(context.Background(), args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestAnalyticsTrafficTableAndFilters(t *testing.T) {
	traffic := `{"response":{` + analyticsMeta + `,"interval":"1h","summary":{"requests":1200,"visits":300,"bytes":2048000,"errors_4xx":12,"errors_5xx":3,"error_rate_4xx":0.01,"error_rate_5xx":0.0025,"edge_time_p95_ms":182.4},"series":[{"time":"2026-11-01T00:00:00Z","requests":10,"visits":1,"bytes":1,"errors_4xx":0,"errors_5xx":0},{"time":"2026-11-01T01:00:00Z","requests":20,"visits":1,"bytes":1,"errors_4xx":0,"errors_5xx":0}],` +
		`"previous":{"start":"2026-10-31T00:00:00Z","end":"2026-11-01T00:00:00Z","summary":{"requests":600,"visits":300,"bytes":1,"errors_4xx":0,"errors_5xx":0,"error_rate_4xx":0,"error_rate_5xx":0,"edge_time_p95_ms":200},"series":[]},` +
		`"breakdowns":{"country":[{"value":"BR","label":null,"requests":900,"visits":200,"bytes":10},{"value":"US","label":null,"requests":300,"visits":100,"bytes":5}],"path":[{"value":"/evil\u001b[2J","label":null,"requests":5,"visits":1,"bytes":1}],"asn":[{"value":"28573","label":"Claro","requests":4,"visits":1,"bytes":1}]}}}`
	query, stop := analyticsServer(t, 200, map[string]string{"/v1/apps/app-1/analytics": traffic})
	defer stop()
	code, out, errOut := runCLI("--lang", "en", "app", "analytics", "app-1", "--start", "2026-11-01T00:00:00Z", "--end=2026-11-02T00:00:00Z", "--compare", "--country", "BR", "--status", "5xx", "--limit", "5")
	if code != 0 {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
	if query.Get("compare") != "true" || query.Get("country") != "BR" || query.Get("status") != "5xx" || query.Get("limit") != "5" || query.Get("start") == "" || query.Get("end") == "" {
		t.Fatalf("query = %v", *query)
	}
	for _, want := range []string{"1,200", "+100.0%", "BR", "US", "Claro", "Countries", "Networks (ASN)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[2J") {
		t.Errorf("escape from visitor data reached the terminal: %q", out)
	}
}

func TestAnalyticsJSONIsTheSDKAnswerAndDefaultsToNoWindow(t *testing.T) {
	query, stop := analyticsServer(t, 200, map[string]string{"/v1/apps/app-1/analytics/errors": `{"response":{` + analyticsMeta + `,"interval":"1h","include_4xx":true,"summary":null,"series":[],"previous":null,"paths":[],"statuses":[]}}`})
	defer stop()
	code, out, errOut := runCLI("app", "analytics", "errors", "app-1", "--include-4xx", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if query.Get("include_4xx") != "true" || query.Get("start") != "" || query.Get("end") != "" {
		t.Fatalf("query = %v", *query)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(out), &data); err != nil || data["include_4xx"] != true || data["web_published"] != true {
		t.Fatalf("json = %q err=%v", out, err)
	}
}

func TestAnalyticsPerformanceShowsEdgeAndContainer(t *testing.T) {
	perf := `{"response":{` + analyticsMeta + `,"interval":"1h","summary":{"requests":50,"edge":{"avg_ms":40,"p50_ms":30,"p75_ms":50,"p90_ms":80,"p95_ms":120,"p99_ms":300,"p999_ms":900},"container":{"avg_ms":20,"p50_ms":15,"p75_ms":25,"p90_ms":40,"p95_ms":60,"p99_ms":150,"p999_ms":400}},"series":[],"previous":null,"countries":[],"edge_locations":[{"value":"GRU","label":null,"requests":50,"edge":{"avg_ms":40,"p50_ms":30,"p95_ms":120,"p99_ms":300},"container":null}],"slow_paths":[],"path_issues":[{"path":"/api","requests":50,"errors":1,"error_rate":0.02,"avg_ms":40,"p99_ms":300}]}}`
	_, stop := analyticsServer(t, 200, map[string]string{"/v1/apps/app-1/analytics/performance": perf})
	defer stop()
	code, out, errOut := runCLI("--lang", "en", "app", "analytics", "performance", "app-1")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"p99.9", "900 ms", "400 ms", "GRU", "/api", "Problems by path"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestAnalyticsRequestsShowsNextPageWithWindow(t *testing.T) {
	list := `{"response":{` + analyticsMeta + `,"order":"desc","requests":[{"time":"2026-11-01T10:00:00Z","request_id":"req-1","host":"demo.vertraweb.app","path":"/a\u001b[31m","query":"x=1","method":"GET","protocol":"HTTP/2","status":502,"container_status":null,"edge_time_ms":12.5,"container_time_ms":null,"ip":"1.2.3.4","asn":1,"asn_name":"n","country":"BR","edge_location":"GRU","referer":"(none)","referer_url":null,"device":"desktop","os":"x","browser":"y","bot":"(none)","content_type":"html","cache_status":"miss"}],"cursor":"abc123"}}`
	query, stop := analyticsServer(t, 200, map[string]string{"/v1/apps/app-1/analytics/requests": list})
	defer stop()
	code, out, errOut := runCLI("--lang", "en", "app", "analytics", "requests", "app-1", "--order", "desc", "--limit", "20", "--cursor", "prev")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if query.Get("order") != "desc" || query.Get("limit") != "20" || query.Get("cursor") != "prev" {
		t.Fatalf("query = %v", *query)
	}
	for _, want := range []string{"req-1", "502", "--cursor abc123", "--start 2026-11-01T00:00:00Z", "/a?x=1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[31m") {
		t.Errorf("escape from visitor data reached the terminal: %q", out)
	}
}

func TestAnalyticsNotPublishedIsAnExplicitState(t *testing.T) {
	_, stop := analyticsServer(t, 200, map[string]string{"/v1/apps/app-1/analytics": `{"response":{"start":"2026-11-01T00:00:00Z","end":"2026-11-02T00:00:00Z","hosts":[],"web_published":false,"sampled":false,"stale":false,"generated_at":"2026-11-02T00:01:00Z","history_days":7,"interval":"1h","summary":null,"series":[],"previous":null,"breakdowns":{}}}`})
	defer stop()
	code, out, _ := runCLI("--lang", "en", "app", "analytics", "app-1")
	if code != 0 || !strings.Contains(out, "no site published") {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestAnalyticsRejectsBadFlagsBeforeNetwork(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VERTRA_API_KEY", "local-key")
	old := apiBaseURL
	apiBaseURL = "http://127.0.0.1:1"
	defer func() { apiBaseURL = old }()
	for _, args := range [][]string{
		{"app", "analytics", "app-1", "--start", "yesterday"},
		{"app", "analytics", "app-1", "--start", "2026-11-02T00:00:00Z", "--end", "2026-11-01T00:00:00Z"},
		{"app", "analytics", "app-1", "--limit", "26"},
		{"app", "analytics", "requests", "app-1", "--order", "sideways"},
		{"app", "analytics", "app-1", "--breakdowns", "nope"},
		{"app", "analytics", "requests", "app-1", "--compare"},
		{"app", "analytics", "app-1", "--compare-start", "last week"},
	} {
		if code, _, _ := runCLI(append(args, "--json")...); code != 1 {
			t.Fatalf("%v: exit %d", args, code)
		}
	}
}

func TestAnalyticsErrorMessages(t *testing.T) {
	for _, tc := range []struct {
		status       int
		body, want   string
		wantContains []string
	}{
		{403, `{"code":"PLAN_DOES_NOT_SUPPORT_WEB_ANALYTICS_PERFORMANCE","message":"x"}`, "performance", []string{"does not include performance", "larger plans"}},
		{403, `{"code":"PLAN_DOES_NOT_SUPPORT_WEB_ANALYTICS_REQUESTS","message":"x"}`, "requests", []string{"does not include the request log"}},
		{400, `{"code":"ANALYTICS_RANGE_EXCEEDS_PLAN","message":"x","details":{"history_days":7,"earliest":"2026-10-27T00:00:00.000Z"}}`, "traffic", []string{"starts before the history", "7 days back", "2026-10-27T00:00:00Z"}},
		{503, `{"code":"ANALYTICS_BUSY","message":"x","details":{"retry_after":60}}`, "traffic", []string{"busy", "Try again in 60 s"}},
		{400, `{"code":"INVALID_RANGE","message":"x"}`, "traffic", []string{"Invalid window", "at most 30 days"}},
	} {
		path := "/v1/apps/app-1/analytics"
		args := []string{"--lang", "en", "app", "analytics", "app-1"}
		if tc.want != "traffic" {
			path += "/" + tc.want
			args = []string{"--lang", "en", "app", "analytics", tc.want, "app-1"}
		}
		_, stop := analyticsServer(t, tc.status, map[string]string{path: tc.body})
		var out, errOut bytes.Buffer
		code := run(context.Background(), args, &out, &errOut)
		stop()
		combined := out.String() + errOut.String()
		if code != 1 {
			t.Fatalf("%s: exit %d", tc.want, code)
		}
		for _, w := range tc.wantContains {
			if !strings.Contains(combined, w) {
				t.Errorf("%s: output missing %q: %s", tc.body, w, combined)
			}
		}
	}
}
