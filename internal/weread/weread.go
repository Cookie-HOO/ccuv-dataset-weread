// Package weread adapts WeRead daily reading-time data to ccuv's custom protocol.
package weread

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/protocol"
)

// Gateway supplies normalized records for the requested intervals. Implementations
// own authentication and transport; records outside the intervals are ignored.
type Gateway interface {
	DailyRecords(context.Context, []protocol.Range) ([]DailyRecord, error)
	PeriodRanking(context.Context, protocol.Range, string) ([]RankedRecord, error)
}

// RankedRecord is one official natural-period reading summary.
type RankedRecord struct {
	ID      string
	Label   string
	Seconds string
}

// DailyRecord is one date whose total reading seconds are known exactly.
type DailyRecord struct {
	Date    string
	Seconds string
}

// FakeGateway is a deterministic dependency-injected gateway for tests and fixtures.
type FakeGateway struct {
	Records        []DailyRecord
	RankingRecords []RankedRecord
	Err            error
}

func (g FakeGateway) DailyRecords(_ context.Context, _ []protocol.Range) ([]DailyRecord, error) {
	if g.Err != nil {
		return nil, g.Err
	}
	return append([]DailyRecord(nil), g.Records...), nil
}

func (g FakeGateway) PeriodRanking(_ context.Context, _ protocol.Range, _ string) ([]RankedRecord, error) {
	if g.Err != nil {
		return nil, g.Err
	}
	return append([]RankedRecord(nil), g.RankingRecords...), nil
}

// Handler processes one validated ccuv request against a gateway.
type Handler struct{ gateway Gateway }

func NewHandler(gateway Gateway) Handler { return Handler{gateway: gateway} }

func (h Handler) Handle(ctx context.Context, request protocol.Request) any {
	if err := request.Validate(); err != nil {
		return protocol.ErrorFor(request, "invalid_request", "The request is invalid.", "请求无效。")
	}
	if response := SelectionError(request); response != nil {
		return *response
	}
	if request.RequestKind == "chart" && request.Query.GroupBy != nil {
		return h.groupedRanking(ctx, request)
	}
	intervals := requestIntervals(request)
	records, err := h.gateway.DailyRecords(ctx, intervals)
	if err != nil {
		if errors.Is(err, errRangeNotAvailable) {
			return unavailable(request)
		}
		return protocol.ErrorFor(request, "source_unavailable", "WeRead data is unavailable.", "微信读书数据暂不可用。")
	}
	known, err := recordsByDate(records)
	if err != nil {
		return protocol.ErrorFor(request, "internal_error", "The source returned invalid daily records.", "数据源返回了无效的每日记录。")
	}
	if request.RequestKind == "chart" {
		return h.chart(request, known)
	}
	return h.summary(request, known)
}

func requestIntervals(request protocol.Request) []protocol.Range {
	if request.Range != nil {
		return []protocol.Range{*request.Range}
	}
	intervals := make([]protocol.Range, 0, len(request.Summary.Intervals))
	for _, interval := range request.Summary.Intervals {
		intervals = append(intervals, interval)
	}
	return intervals
}

func recordsByDate(records []DailyRecord) (map[string]string, error) {
	known := make(map[string]string, len(records))
	for _, record := range records {
		if _, err := parseDate(record.Date); err != nil {
			return nil, fmt.Errorf("invalid record date")
		}
		if _, err := strconv.ParseUint(record.Seconds, 10, 64); err != nil {
			return nil, fmt.Errorf("invalid record seconds")
		}
		if _, exists := known[record.Date]; exists {
			return nil, fmt.Errorf("duplicate record date")
		}
		known[record.Date] = record.Seconds
	}
	return known, nil
}

// SelectionError validates source-owned query combinations without needing a gateway.
func SelectionError(request protocol.Request) *protocol.ErrorResponse {
	if request.RequestKind != "chart" || request.Query.GroupBy == nil {
		return nil
	}
	if !supportedRankingGroupBy(*request.Query.GroupBy) {
		response := protocol.UnsupportedQueryFor(
			request,
			"query.group_by",
			*request.Query.GroupBy,
			"WeRead does not support the selected grouping. Supported groupings are Total, Book, and Category.",
			"微信读书不支持所选分组。支持的分组为总计、书籍和分类。",
		)
		return &response
	}
	if request.Range != nil && request.Range.Period != nil && supportedGroupPeriod(*request.Range.Period) {
		return nil
	}
	period := "null"
	if request.Range != nil && request.Range.Period != nil {
		period = *request.Range.Period
	}
	response := protocol.UnsupportedQueryFor(
		request,
		"range.period",
		period,
		"WeRead grouped summaries support only This week (1w), This month (1mo), and This year (1y). Switch to one of these periods.",
		"微信读书分组汇总仅支持本周（1w）、本月（1mo）和本年（1y）。请切换到这三个周期之一。",
	)
	return &response
}

func (h Handler) groupedRanking(ctx context.Context, request protocol.Request) any {
	records, err := h.gateway.PeriodRanking(ctx, *request.Range, *request.Query.GroupBy)
	if err != nil {
		english, chinese := rankingSourceFailureMessage(sourceFailureKindOf(err))
		return protocol.ErrorFor(request, "source_unavailable", english, chinese)
	}
	series := make([]protocol.Series, 0, len(records))
	for _, record := range records {
		seconds, err := strconv.ParseUint(record.Seconds, 10, 64)
		if err != nil || record.ID == "" || record.Label == "" {
			return protocol.ErrorFor(request, "internal_error", "The source returned invalid ranking data.", "数据源返回了无效的排行数据。")
		}
		series = append(series, protocol.Series{
			ID:     record.ID,
			Label:  protocol.LocalizedText{EN: record.Label, ZhCN: record.Label},
			Points: []protocol.Point{{Date: request.Range.EndDate, Value: formatMinutes(seconds)}},
		})
	}
	return protocol.NewGroupedRankingResponse(request, series)
}

func rankingSourceFailureMessage(kind sourceFailureKind) (string, string) {
	switch kind {
	case sourceFailurePeriod:
		return "WeRead could not match the selected period to a source period.", "微信读书无法将所选周期匹配到数据源周期。"
	case sourceFailureUpgrade:
		return "WeRead requires a source upgrade before this ranking is available.", "微信读书需要完成数据源升级后才能提供此排行。"
	case sourceFailureResponse:
		return "WeRead returned an invalid response for this ranking.", "微信读书为此排行返回了无效响应。"
	case sourceFailureRanking:
		return "WeRead did not provide valid ranking data for the selected grouping.", "微信读书未为所选分组提供有效的排行数据。"
	default:
		return "WeRead could not complete the source request for this ranking.", "微信读书无法完成此排行的数据源请求。"
	}
}

func supportedRankingGroupBy(groupBy string) bool {
	for _, supported := range protocol.RankingGroupBy {
		if groupBy == supported {
			return true
		}
	}
	return false
}

func supportedGroupPeriod(period string) bool {
	return period == "1w" || period == "1mo" || period == "1y"
}

func (h Handler) chart(request protocol.Request, known map[string]string) any {
	dates, err := inclusiveDates(*request.Range)
	if err != nil {
		return protocol.ErrorFor(request, "invalid_request", "The request range is invalid.", "请求范围无效。")
	}
	points := make([]protocol.Point, 0, len(dates))
	for _, date := range dates {
		value, found := known[date]
		if !found {
			return unavailable(request)
		}
		seconds, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return protocol.ErrorFor(request, "internal_error", "The source returned invalid daily records.", "数据源返回了无效的每日记录。")
		}
		points = append(points, protocol.Point{Date: date, Value: formatMinutes(seconds)})
	}
	return protocol.NewChartResponse(request, points)
}

func (h Handler) summary(request protocol.Request, known map[string]string) any {
	values := map[string]string{}
	for name, interval := range request.Summary.Intervals {
		total, ok := sumKnown(interval, known)
		if !ok {
			return unavailable(request)
		}
		values[name] = total
	}
	current := values["current"]
	var sequential, yearOverYear *string
	if value, ok := values["sequential"]; ok {
		sequential = &value
	}
	if value, ok := values["year_over_year"]; ok {
		yearOverYear = &value
	}
	return protocol.NewSummaryResponse(request, current, sequential, yearOverYear)
}

func unavailable(request protocol.Request) protocol.ErrorResponse {
	return protocol.ErrorFor(request, "range_not_available", "The requested date range is not fully available.", "请求的日期范围数据不完整。")
}

func sumKnown(interval protocol.Range, known map[string]string) (string, bool) {
	dates, err := inclusiveDates(interval)
	if err != nil {
		return "", false
	}
	var total uint64
	for _, date := range dates {
		value, ok := known[date]
		if !ok {
			return "", false
		}
		number, err := strconv.ParseUint(value, 10, 64)
		if err != nil || ^uint64(0)-total < number {
			return "", false
		}
		total += number
	}
	return formatMinutes(total), true
}

func formatMinutes(seconds uint64) string {
	whole := seconds / 60
	hundredths := (seconds%60*100 + 30) / 60
	if hundredths == 100 {
		whole++
		hundredths = 0
	}
	return fmt.Sprintf("%d.%02d", whole, hundredths)
}

func inclusiveDates(interval protocol.Range) ([]string, error) {
	start, err := parseDate(interval.StartDate)
	if err != nil {
		return nil, err
	}
	end, err := parseDate(interval.EndDate)
	if err != nil || end.Before(start) {
		return nil, fmt.Errorf("invalid interval")
	}
	var dates []string
	for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
		dates = append(dates, date.Format("2006-01-02"))
	}
	return dates, nil
}

func parseDate(value string) (time.Time, error) { return time.Parse("2006-01-02", value) }
