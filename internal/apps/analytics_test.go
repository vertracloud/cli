package apps

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

type fakeAnalytics struct {
	id      string
	traffic *vertracloud.ApplicationAnalyticsTrafficParams
	errs    *vertracloud.ApplicationAnalyticsErrorsParams
	perf    *vertracloud.ApplicationAnalyticsPerformanceParams
	reqs    *vertracloud.ApplicationAnalyticsRequestsParams
}

func (f *fakeAnalytics) Traffic(_ context.Context, id string, p *vertracloud.ApplicationAnalyticsTrafficParams, _ ...rest.RequestOpt) (vertracloud.ApplicationAnalyticsTraffic, error) {
	f.id, f.traffic = id, p
	return vertracloud.ApplicationAnalyticsTraffic{}, nil
}
func (f *fakeAnalytics) Errors(_ context.Context, id string, p *vertracloud.ApplicationAnalyticsErrorsParams, _ ...rest.RequestOpt) (vertracloud.ApplicationAnalyticsErrors, error) {
	f.id, f.errs = id, p
	return vertracloud.ApplicationAnalyticsErrors{}, nil
}
func (f *fakeAnalytics) Performance(_ context.Context, id string, p *vertracloud.ApplicationAnalyticsPerformanceParams, _ ...rest.RequestOpt) (vertracloud.ApplicationAnalyticsPerformance, error) {
	f.id, f.perf = id, p
	return vertracloud.ApplicationAnalyticsPerformance{}, nil
}
func (f *fakeAnalytics) Requests(_ context.Context, id string, p *vertracloud.ApplicationAnalyticsRequestsParams, _ ...rest.RequestOpt) (vertracloud.ApplicationAnalyticsRequestList, error) {
	f.id, f.reqs = id, p
	return vertracloud.ApplicationAnalyticsRequestList{}, nil
}
func (f *fakeAnalytics) RequestsIter(string, *vertracloud.ApplicationAnalyticsRequestsParams, ...rest.RequestOpt) *vertracloud.ApplicationAnalyticsRequestIterator {
	return nil
}

func TestAnalyticsMapsFlagsToTheSDK(t *testing.T) {
	apps := &fakeApps{}
	client := &vertracloud.Client{Apps: apps}
	_, err := Run(context.Background(), client, "analytics", []string{"app-1", "--start", "2026-11-01T00:00:00Z", "--compare", "--breakdowns", "country,edge-location", "--limit", "5",
		"--path", "/api", "--status", "5xx", "--referer", "(none)", "--cache-status", "hit", "--content-type", "json", "--workspace", "ws-1"})
	if err != nil {
		t.Fatal(err)
	}
	p := apps.analytics.traffic
	if apps.analytics.id != "app-1" || p == nil || !p.Compare || p.Limit != 5 || p.WorkspaceID != "ws-1" ||
		p.Start != time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC) || !p.End.IsZero() ||
		p.Filters.Path != "/api" || p.Filters.Status != "5xx" || p.Filters.Referer != "(none)" || p.Filters.CacheStatus != "hit" || p.Filters.ContentType != "json" ||
		len(p.Breakdowns) != 2 || p.Breakdowns[1] != vertracloud.ApplicationAnalyticsDimensionEdgeLocation {
		t.Fatalf("params = %#v", p)
	}
}

func TestAnalyticsSubcommands(t *testing.T) {
	apps := &fakeApps{}
	client := &vertracloud.Client{Apps: apps}
	ctx := context.Background()
	if _, err := Run(ctx, client, "analytics", []string{"errors", "app-2", "--include-4xx"}); err != nil || apps.analytics.errs == nil || !apps.analytics.errs.Include4xx || apps.analytics.id != "app-2" {
		t.Fatalf("errors: %#v %v", apps.analytics.errs, err)
	}
	if _, err := Run(ctx, client, "analytics", []string{"errors", "app-2", "--compare-start", "2026-10-26T00:00:00Z"}); err != nil || !apps.analytics.errs.CompareStart.Equal(time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("errors compare-start: %#v %v", apps.analytics.errs, err)
	}
	if _, err := Run(ctx, client, "analytics", []string{"app-1", "--compare-start=2026-10-26T00:00:00Z"}); err != nil || apps.analytics.traffic.CompareStart.IsZero() {
		t.Fatalf("traffic compare-start: %#v %v", apps.analytics.traffic, err)
	}
	if _, err := Run(ctx, client, "analytics", []string{"performance", "app-3", "--compare-start", "2026-10-26T00:00:00Z"}); err != nil || apps.analytics.perf.CompareStart.IsZero() {
		t.Fatalf("performance compare-start: %#v %v", apps.analytics.perf, err)
	}
	if _, err := Run(ctx, client, "analytics", []string{"app-1", "--compare-start", "last week"}); err == nil {
		t.Fatal("a malformed --compare-start must fail before calling the API")
	}
	if _, err := Run(ctx, client, "analytics", []string{"requests", "app-4", "--compare-start", "2026-10-26T00:00:00Z"}); err == nil {
		t.Fatal("requests has no --compare-start")
	}
	if _, err := Run(ctx, client, "analytics", []string{"performance", "app-3", "--compare"}); err != nil || apps.analytics.perf == nil || !apps.analytics.perf.Compare {
		t.Fatalf("performance: %#v %v", apps.analytics.perf, err)
	}
	if _, err := Run(ctx, client, "analytics", []string{"requests", "app-4", "--order", "asc", "--cursor", "c1", "--limit", "100", "--country", "BR"}); err != nil ||
		apps.analytics.reqs == nil || apps.analytics.reqs.Order != vertracloud.ApplicationAnalyticsOrderAsc || apps.analytics.reqs.Cursor != "c1" || apps.analytics.reqs.Limit != 100 || apps.analytics.reqs.Filters.Country != "BR" {
		t.Fatalf("requests: %#v %v", apps.analytics.reqs, err)
	}
}

func TestAnalyticsValidatesInput(t *testing.T) {
	client := &vertracloud.Client{Apps: &fakeApps{}}
	for args, want := range map[string]string{
		"app-1 --start nope": "--start",
		"app-1 --end nope":   "--end",
		"app-1 --start 2026-11-02T00:00:00Z --end 2026-11-01T00:00:00Z": "after --start",
		"app-1 --limit 30":           "--limit",
		"app-1 --breakdowns x":       "unknown breakdown",
		"app-1 --unknown":            "unknown flag",
		"requests app-1 --order up":  "--order",
		"requests app-1 --limit 101": "--limit",
		"errors app-1 extra":         "one app id",
	} {
		if _, err := Run(context.Background(), client, "analytics", strings.Fields(args)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", args, err, want)
		}
	}
}
