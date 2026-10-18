// Package readingtime implements WeRead's additive reading-time Metric.
package readingtime

import (
	"context"
	"fmt"
	"time"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/protocol/v2"
	"github.com/ccusage-viz/ccuv-dataset-weread/internal/weread"
)

const ID = "reading_time"

type Source interface {
	DailyRecords(context.Context, []v2.Range) ([]weread.DailyRecord, error)
	PeriodRanking(context.Context, v2.Range, string) ([]weread.ReadingRanking, error)
}

type Handler struct{ source Source }

func New(source Source) Handler { return Handler{source} }

func Descriptor() v2.MetricDescriptor {
	text := func(en, zh string) v2.LocalizedText { return v2.LocalizedText{EN: en, ZhCN: zh} }
	chart := func(groupBy []string, periods []string) v2.ChartCapability {
		return v2.ChartCapability{Granularities: []string{"day"}, GroupBy: groupBy, Periods: periods}
	}
	return v2.MetricDescriptor{
		ID: ID, Title: text("Reading time", "阅读时长"), Description: text("Daily reading time from your WeRead activity.", "基于微信读书活动统计每日阅读时长。"),
		Unit: v2.Unit{ID: "minute", Label: text("minutes", "分钟"), Placement: "suffix", DecimalPlaces: 2}, Aggregation: "sum", Summary: "comparisons",
		Capabilities: v2.MetricCapabilities{GroupBy: []v2.Dimension{{ID: "book", Label: text("Book", "书籍")}, {ID: "category", Label: text("Category", "分类")}}, Charts: map[string]v2.ChartCapability{
			"timeline": chart([]string{}, nil), "calendar": chart([]string{}, nil), "stack": chart([]string{}, nil),
			"ranking": {
				GroupBy:  []string{"book", "category"},
				Periods:  []string{"1d", "7d", "14d", "30d", "90d", "180d", "365d", "1w", "1mo", "1y"},
				MaxItems: ptr(10), Comparison: "none",
			},
		}},
		Defaults: map[string]v2.ChartDefaults{
			"timeline": {Period: "14d", GroupBy: nil, Top: nil}, "calendar": {Period: "14d", GroupBy: nil, Top: nil}, "stack": {Period: "14d", GroupBy: nil, Top: nil}, "ranking": {Period: "1y", GroupBy: "book", Top: 10},
		},
	}
}

func ptr(value int) *int { return &value }

func (h Handler) Chart(ctx context.Context, request v2.Request) v2.Response {
	chart := *request.Chart
	if response := ValidateChart(request, chart); response != nil {
		return *response
	}
	if request.ChartKind == "ranking" && chart.Query.GroupBy != nil {
		records, err := h.source.PeriodRanking(ctx, chart.Range, *chart.Query.GroupBy)
		if err != nil {
			return sourceUnavailable(request, err)
		}
		series := make([]v2.RankingItem, 0, len(records))
		for _, record := range records {
			if record.ID == "" || record.Label == "" {
				return v2.ErrorFor(request, "internal_error", "The source returned invalid ranking data.", "数据源返回了无效的排行数据。", nil)
			}
			item := v2.RankingItem{ID: record.ID, Label: v2.LocalizedText{EN: record.Label, ZhCN: record.Label}, Value: formatMinutes(record.Seconds)}
			display := formatDisplay(record.Seconds)
			item.Display = &display
			if *chart.Query.GroupBy == "book" && record.Author != "" {
				description := v2.LocalizedText{EN: record.Author, ZhCN: record.Author}
				item.Description = &description
			}
			if *chart.Query.GroupBy == "book" {
				item.Link = record.ReaderLink
			}
			series = append(series, item)
		}
		return v2.OK(request, map[string]any{"warnings": warnings(request), "data": map[string]any{"series": series}})
	}
	records, err := h.source.DailyRecords(ctx, []v2.Range{chart.Range})
	if err != nil {
		return sourceUnavailable(request, err)
	}
	known := make(map[string]uint64, len(records))
	for _, record := range records {
		if _, err := time.Parse("2006-01-02", record.Date); err != nil {
			return v2.ErrorFor(request, "internal_error", "The source returned invalid daily records.", "数据源返回了无效的每日记录。", nil)
		}
		if _, duplicate := known[record.Date]; duplicate {
			return v2.ErrorFor(request, "internal_error", "The source returned invalid daily records.", "数据源返回了无效的每日记录。", nil)
		}
		known[record.Date] = record.Seconds
	}
	if request.ChartKind == "ranking" {
		total, ok := sumRange(chart.Range, known)
		if !ok {
			return v2.ErrorFor(request, "range_not_available", "The requested date range is not fully available.", "请求的日期范围数据不完整。", nil)
		}
		data := map[string]any{"series": []v2.RankingItem{{ID: "total", Label: v2.LocalizedText{EN: "Total", ZhCN: "总计"}, Value: formatMinutes(total)}}}
		return v2.OK(request, map[string]any{"warnings": warnings(request), "data": data})
	}
	points, ok := seriesForRange(chart.Range, known)
	if !ok {
		return v2.ErrorFor(request, "range_not_available", "The requested date range is not fully available.", "请求的日期范围数据不完整。", nil)
	}
	data := map[string]any{"series": []v2.Series{{ID: "total", Label: v2.LocalizedText{EN: "Total", ZhCN: "总计"}, Points: points}}}
	return v2.OK(request, map[string]any{"warnings": warnings(request), "data": data})
}

func (h Handler) Summary(ctx context.Context, request v2.Request) v2.Response {
	if response := ValidateSummary(request); response != nil {
		return *response
	}
	intervals := request.Summary.Summary.Intervals
	ranges := make([]v2.Range, 0, len(intervals))
	for _, interval := range intervals {
		ranges = append(ranges, interval)
	}
	records, err := h.source.DailyRecords(ctx, ranges)
	if err != nil {
		return sourceUnavailable(request, err)
	}
	known := make(map[string]uint64, len(records))
	for _, record := range records {
		if _, duplicate := known[record.Date]; duplicate {
			return v2.ErrorFor(request, "internal_error", "The source returned invalid daily records.", "数据源返回了无效的每日记录。", nil)
		}
		known[record.Date] = record.Seconds
	}
	values := map[string]string{}
	for name, interval := range intervals {
		total, ok := sumRange(interval, known)
		if !ok {
			return v2.ErrorFor(request, "range_not_available", "The requested date range is not fully available.", "请求的日期范围数据不完整。", nil)
		}
		values[name] = formatMinutes(total)
	}
	comparison := func(value string, exists bool) map[string]any {
		if !exists {
			return map[string]any{"status": "unavailable"}
		}
		return map[string]any{"status": "ready", "base_value": value}
	}
	current := values["current"]
	_, hasSequential := intervals["sequential"]
	_, hasYearOverYear := intervals["year_over_year"]
	return v2.OK(request, map[string]any{"warnings": warnings(request), "summary": map[string]any{"metric": map[string]any{"id": "reading-time", "label": v2.LocalizedText{EN: "Reading time", ZhCN: "阅读时长"}, "current": map[string]any{"value": current, "display": v2.LocalizedText{EN: current + " minutes", ZhCN: current + " 分钟"}}, "sequential": comparison(values["sequential"], hasSequential), "year_over_year": comparison(values["year_over_year"], hasYearOverYear)}}})
}

// ValidateChart rejects source-owned selections before a Gateway is created.
func ValidateChart(request v2.Request, chart v2.ChartRequest) *v2.Response {
	if len(chart.Query.Filters) != 0 || (request.ChartKind != "ranking" && chart.Query.Granularity != "day") {
		response := v2.ErrorFor(request, "invalid_request", "The request is invalid.", "请求无效。", nil)
		return &response
	}
	if request.ChartKind != "ranking" {
		if request.ChartKind != "timeline" && request.ChartKind != "calendar" && request.ChartKind != "stack" {
			response := v2.ErrorFor(request, "unsupported_chart_kind", "WeRead does not support the selected chart.", "微信读书不支持所选图表。", nil)
			return &response
		}
		if chart.Query.GroupBy != nil {
			response := v2.ErrorFor(request, "invalid_request", "The request is invalid.", "请求无效。", nil)
			return &response
		}
		return nil
	}
	if chart.Query.GroupBy == nil {
		return nil
	}
	if *chart.Query.GroupBy != "book" && *chart.Query.GroupBy != "category" {
		response := v2.ErrorFor(request, "unsupported_query", "WeRead does not support the selected grouping. Supported groupings are Total, Book, and Category.", "微信读书不支持所选分组。支持的分组为总计、书籍和分类。", &v2.Unsupported{Field: "query.group_by", Value: *chart.Query.GroupBy})
		return &response
	}
	if chart.Range.Period == nil || (*chart.Range.Period != "1w" && *chart.Range.Period != "1mo" && *chart.Range.Period != "1y") {
		value := "null"
		if chart.Range.Period != nil {
			value = *chart.Range.Period
		}
		response := v2.ErrorFor(request, "unsupported_query", "WeRead grouped summaries support only This week (1w), This month (1mo), and This year (1y). Switch to one of these periods.", "微信读书分组汇总仅支持本周（1w）、本月（1mo）和本年（1y）。请切换到这三个周期之一。", &v2.Unsupported{Field: "range.period", Value: value})
		return &response
	}
	return nil
}

// ValidateSummary rejects unsupported source selections before a Gateway is created.
func ValidateSummary(request v2.Request) *v2.Response {
	summary := request.Summary
	if len(summary.Query.Filters) != 0 || summary.Query.GroupBy != nil || (summary.Query.Granularity != "" && summary.Query.Granularity != "day") {
		response := v2.ErrorFor(request, "invalid_request", "The request is invalid.", "请求无效。", nil)
		return &response
	}
	return nil
}

func sourceUnavailable(request v2.Request, err error) v2.Response {
	en, zh := weread.SourceFailureMessage(err)
	return v2.ErrorFor(request, "source_unavailable", en, zh, nil)
}
func warnings(request v2.Request) []v2.Warning {
	if request.Timezone == "Asia/Shanghai" {
		return nil
	}
	return []v2.Warning{{Code: "timezone_not_supported", Message: v2.LocalizedText{EN: "WeRead dates use the Asia/Shanghai source timezone.", ZhCN: "微信读书日期使用 Asia/Shanghai 数据源时区。"}}}
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

func formatDisplay(seconds uint64) v2.LocalizedText {
	minutes := seconds / 60
	hours, remainder := minutes/60, minutes%60
	if hours == 0 {
		return v2.LocalizedText{EN: fmt.Sprintf("%dm", remainder), ZhCN: fmt.Sprintf("%d分钟", remainder)}
	}
	return v2.LocalizedText{EN: fmt.Sprintf("%dh %dm", hours, remainder), ZhCN: fmt.Sprintf("%d小时%d分钟", hours, remainder)}
}
func seriesForRange(interval v2.Range, known map[string]uint64) ([]v2.Point, bool) {
	start, err := time.Parse("2006-01-02", interval.StartDate)
	end, endErr := time.Parse("2006-01-02", interval.EndDate)
	if err != nil || endErr != nil {
		return nil, false
	}
	points := []v2.Point{}
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		value, ok := known[day.Format("2006-01-02")]
		if !ok {
			return nil, false
		}
		points = append(points, v2.Point{Date: day.Format("2006-01-02"), Value: formatMinutes(value)})
	}
	return points, true
}
func sumRange(interval v2.Range, known map[string]uint64) (uint64, bool) {
	start, err := time.Parse("2006-01-02", interval.StartDate)
	end, endErr := time.Parse("2006-01-02", interval.EndDate)
	if err != nil || endErr != nil || end.Before(start) {
		return 0, false
	}
	var total uint64
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		value, ok := known[day.Format("2006-01-02")]
		if !ok || ^uint64(0)-total < value {
			return 0, false
		}
		total += value
	}
	return total, true
}
