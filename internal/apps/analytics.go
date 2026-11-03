package apps

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// analyticsDimensions is the order the traffic breakdowns are shown in, and the
// set of values --breakdowns accepts.
var analyticsDimensions = []vertracloud.ApplicationAnalyticsDimension{
	vertracloud.ApplicationAnalyticsDimensionPath,
	vertracloud.ApplicationAnalyticsDimensionCountry,
	vertracloud.ApplicationAnalyticsDimensionReferer,
	vertracloud.ApplicationAnalyticsDimensionStatus,
	vertracloud.ApplicationAnalyticsDimensionMethod,
	vertracloud.ApplicationAnalyticsDimensionDevice,
	vertracloud.ApplicationAnalyticsDimensionOS,
	vertracloud.ApplicationAnalyticsDimensionBrowser,
	vertracloud.ApplicationAnalyticsDimensionHost,
	vertracloud.ApplicationAnalyticsDimensionEdgeLocation,
	vertracloud.ApplicationAnalyticsDimensionProtocol,
	vertracloud.ApplicationAnalyticsDimensionASN,
	vertracloud.ApplicationAnalyticsDimensionIP,
	vertracloud.ApplicationAnalyticsDimensionContentType,
	vertracloud.ApplicationAnalyticsDimensionCacheStatus,
	vertracloud.ApplicationAnalyticsDimensionBot,
}

// analyticsTopN is how many entries per dimension the human output shows.
const analyticsTopN = 10

type analyticsFlags struct {
	start, end, workspace string
	compareStart          string
	compare, include4xx   bool
	limit                 int
	cursor, order         string
	breakdowns            string
	filters               vertracloud.ApplicationAnalyticsFilters
}

// analytics runs "app analytics [traffic|errors|performance|requests] [<id>]".
func analytics(ctx context.Context, client *vertracloud.Client, args []string) (any, error) {
	sub := "traffic"
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "traffic", "errors", "performance", "requests":
			sub, args = strings.ToLower(args[0]), args[1:]
		}
	}
	name := "app analytics"
	if sub != "traffic" {
		name += " " + sub
	}
	f := fs(name)
	var a analyticsFlags
	values := map[string]bool{"--start": true, "--end": true, "--workspace": true, "--limit": true,
		"--country": true, "--edge-location": true, "--host": true, "--path": true, "--method": true, "--status": true,
		"--protocol": true, "--device": true, "--os": true, "--browser": true, "--referer": true, "--asn": true,
		"--ip": true, "--content-type": true, "--cache-status": true, "--bot": true}
	bools := map[string]bool{"--compare": true}
	f.StringVar(&a.start, "start", "", "start of the window (RFC 3339)")
	f.StringVar(&a.end, "end", "", "end of the window (RFC 3339)")
	f.StringVar(&a.workspace, "workspace", "", "workspace id")
	f.IntVar(&a.limit, "limit", 0, "entries per list")
	f.StringVar(&a.filters.Country, "country", "", "filter: country (ISO 3166-1 alpha-2)")
	f.StringVar(&a.filters.EdgeLocation, "edge-location", "", "filter: edge location code")
	f.StringVar(&a.filters.Host, "host", "", "filter: host")
	f.StringVar(&a.filters.Path, "path", "", "filter: path prefix")
	f.StringVar(&a.filters.Method, "method", "", "filter: HTTP method")
	f.StringVar(&a.filters.Status, "status", "", "filter: status or class (5xx)")
	f.StringVar(&a.filters.Protocol, "protocol", "", "filter: HTTP version")
	f.StringVar(&a.filters.Device, "device", "", "filter: device")
	f.StringVar(&a.filters.OS, "os", "", "filter: operating system")
	f.StringVar(&a.filters.Browser, "browser", "", "filter: browser")
	f.StringVar(&a.filters.Referer, "referer", "", "filter: referer host")
	f.StringVar(&a.filters.ASN, "asn", "", "filter: ASN")
	f.StringVar(&a.filters.IP, "ip", "", "filter: visitor IP")
	f.StringVar(&a.filters.ContentType, "content-type", "", "filter: kind of content")
	f.StringVar(&a.filters.CacheStatus, "cache-status", "", "filter: cache status")
	f.StringVar(&a.filters.Bot, "bot", "", "filter: bot category")
	if sub != "requests" {
		f.BoolVar(&a.compare, "compare", false, "compare with the previous period")
		f.StringVar(&a.compareStart, "compare-start", "", "start of the compared period, same length as the window (RFC 3339); implies --compare")
		values["--compare-start"] = true
	} else {
		delete(bools, "--compare")
	}
	switch sub {
	case "traffic":
		f.StringVar(&a.breakdowns, "breakdowns", "", "comma-separated dimensions")
		values["--breakdowns"] = true
	case "errors":
		f.BoolVar(&a.include4xx, "include-4xx", false, "count 4xx as errors")
		bools["--include-4xx"] = true
	case "requests":
		f.StringVar(&a.cursor, "cursor", "", "cursor of the previous page")
		f.StringVar(&a.order, "order", "", "asc or desc")
		values["--cursor"], values["--order"] = true, true
	}
	pos, err := parse(f, args, values, bools)
	if err != nil {
		return nil, err
	}
	rng, err := a.window()
	if err != nil {
		return nil, err
	}
	compareStart, err := a.compareStartTime()
	if err != nil {
		return nil, err
	}
	max := 25
	if sub == "requests" {
		max = 100
	}
	if a.limit < 0 || a.limit > max {
		return nil, fmt.Errorf("--limit must be between 1 and %d", max)
	}
	id, extra, err := requireID(ctx, client, pos, name)
	if err != nil {
		return nil, err
	}
	if len(extra) != 0 {
		return nil, errors.New(name + " accepts one app id")
	}
	svc := client.Apps.Analytics()
	switch sub {
	case "errors":
		data, err := svc.Errors(ctx, id, &vertracloud.ApplicationAnalyticsErrorsParams{ApplicationAnalyticsRange: rng, Compare: a.compare, CompareStart: compareStart, Include4xx: a.include4xx, Limit: a.limit})
		if err != nil {
			return nil, err
		}
		return ui.Presentation{JSON: data, Human: errorsView{data}}, nil
	case "performance":
		data, err := svc.Performance(ctx, id, &vertracloud.ApplicationAnalyticsPerformanceParams{ApplicationAnalyticsRange: rng, Compare: a.compare, CompareStart: compareStart, Limit: a.limit})
		if err != nil {
			return nil, err
		}
		return ui.Presentation{JSON: data, Human: performanceView{data}}, nil
	case "requests":
		order := vertracloud.ApplicationAnalyticsOrder(a.order)
		if order != "" && order != vertracloud.ApplicationAnalyticsOrderAsc && order != vertracloud.ApplicationAnalyticsOrderDesc {
			return nil, errors.New("--order must be asc or desc")
		}
		data, err := svc.Requests(ctx, id, &vertracloud.ApplicationAnalyticsRequestsParams{ApplicationAnalyticsRange: rng, Cursor: a.cursor, Order: order, Limit: a.limit})
		if err != nil {
			return nil, err
		}
		return ui.Presentation{JSON: data, Human: requestsView{data}}, nil
	}
	dims, err := parseDimensions(a.breakdowns)
	if err != nil {
		return nil, err
	}
	data, err := svc.Traffic(ctx, id, &vertracloud.ApplicationAnalyticsTrafficParams{ApplicationAnalyticsRange: rng, Compare: a.compare, CompareStart: compareStart, Breakdowns: dims, Limit: a.limit})
	if err != nil {
		return nil, err
	}
	return ui.Presentation{JSON: data, Human: trafficView{data}}, nil
}

// window validates --start/--end (RFC 3339) and builds the shared range. An
// omitted bound is left to the API: the last 24 hours ending now.
func (a analyticsFlags) window() (vertracloud.ApplicationAnalyticsRange, error) {
	rng := vertracloud.ApplicationAnalyticsRange{Filters: a.filters, WorkspaceID: a.workspace}
	var err error
	if a.start != "" {
		if rng.Start, err = time.Parse(time.RFC3339, a.start); err != nil {
			return rng, errors.New("--start must be an RFC 3339 date, e.g. 2026-11-01T00:00:00Z")
		}
	}
	if a.end != "" {
		if rng.End, err = time.Parse(time.RFC3339, a.end); err != nil {
			return rng, errors.New("--end must be an RFC 3339 date, e.g. 2026-11-02T00:00:00Z")
		}
	}
	if !rng.Start.IsZero() && !rng.End.IsZero() && !rng.End.After(rng.Start) {
		return rng, errors.New("--end must be after --start")
	}
	return rng, nil
}

// compareStartTime validates --compare-start (RFC 3339); it implies --compare
// on the API, so combining it with an explicit --compare is fine.
func (a analyticsFlags) compareStartTime() (time.Time, error) {
	if a.compareStart == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, a.compareStart)
	if err != nil {
		return time.Time{}, errors.New("--compare-start must be an RFC 3339 date, e.g. 2026-10-25T00:00:00Z")
	}
	return t, nil
}

func parseDimensions(raw string) ([]vertracloud.ApplicationAnalyticsDimension, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var out []vertracloud.ApplicationAnalyticsDimension
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(strings.ReplaceAll(name, "-", "_"))
		found := false
		for _, d := range analyticsDimensions {
			if string(d) == name {
				out, found = append(out, d), true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown breakdown %q", name)
		}
	}
	return out, nil
}
