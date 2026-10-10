// Package totalnotes implements WeRead's all-time note-count Metric.
package totalnotes

import (
	"context"
	"fmt"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/protocol/v2"
	"github.com/ccusage-viz/ccuv-dataset-weread/internal/weread"
)

const ID = "total_notes"

type Source interface {
	NotebookCounts(context.Context) ([]weread.NoteCount, error)
}

type Handler struct{ source Source }

func New(source Source) Handler { return Handler{source} }

func Descriptor() v2.MetricDescriptor {
	text := func(en, zh string) v2.LocalizedText { return v2.LocalizedText{EN: en, ZhCN: zh} }
	return v2.MetricDescriptor{
		ID: ID, Title: text("Total notes", "总笔记数"), Description: text("Current all-time notes: highlights, thoughts, and bookmarks.", "当前全量笔记数：划线、想法和书签。"),
		Unit: v2.Unit{ID: "note", Label: text("notes", "笔记"), Placement: "suffix", DecimalPlaces: 0}, Aggregation: "sum", Summary: "total",
		Capabilities: v2.MetricCapabilities{GroupBy: []v2.Dimension{{ID: "book", Label: text("Book", "书籍")}, {ID: "note_type", Label: text("Note type", "笔记类型")}}, Charts: map[string]v2.ChartCapability{"ranking": {Granularities: []string{"day"}, GroupBy: []string{"book", "note_type"}, Periods: []string{"all"}}}},

		Defaults: map[string]v2.ChartDefaults{"ranking": {Period: "all", GroupBy: "book", Top: 10}},
	}
}

func (h Handler) Chart(ctx context.Context, request v2.Request) v2.Response {
	chart := *request.Chart
	if response := ValidateChart(request, chart); response != nil {
		return *response
	}
	counts, err := h.source.NotebookCounts(ctx)
	if err != nil {
		return sourceUnavailable(request, err)
	}
	series, ok := rankingSeries(counts, *chart.Query.GroupBy, chart.Range.EndDate)
	if !ok {
		return v2.ErrorFor(request, "internal_error", "The source returned invalid note data.", "数据源返回了无效的笔记数据。", nil)
	}
	return v2.OK(request, map[string]any{"data": map[string]any{"series": series}})
}

func (h Handler) Summary(ctx context.Context, request v2.Request) v2.Response {
	if response := ValidateSummary(request); response != nil {
		return *response
	}
	counts, err := h.source.NotebookCounts(ctx)
	if err != nil {
		return sourceUnavailable(request, err)
	}
	var total uint64
	for _, count := range counts {
		value, ok := count.Total()
		if !ok || ^uint64(0)-total < value {
			return v2.ErrorFor(request, "internal_error", "The source returned invalid note data.", "数据源返回了无效的笔记数据。", nil)
		}
		total += value
	}
	value := fmt.Sprintf("%d", total)
	return v2.OK(request, map[string]any{"summary": map[string]any{"metric": map[string]any{"id": ID, "label": v2.LocalizedText{EN: "Total notes", ZhCN: "总笔记数"}, "current": map[string]any{"value": value, "display": v2.LocalizedText{EN: value + " notes", ZhCN: value + " 条笔记"}}, "sequential": map[string]any{"status": "unavailable"}, "year_over_year": map[string]any{"status": "unavailable"}}}})
}

// ValidateChart rejects source-owned selections before a Gateway is created.
func ValidateChart(request v2.Request, chart v2.ChartRequest) *v2.Response {
	if len(chart.Query.Filters) != 0 || chart.Query.Granularity != "day" {
		response := v2.ErrorFor(request, "invalid_request", "The request is invalid.", "请求无效。", nil)
		return &response
	}
	if chart.Range.Period == nil || *chart.Range.Period != "all" {
		value := "null"
		if chart.Range.Period != nil {
			value = *chart.Range.Period
		}
		response := v2.ErrorFor(request, "unsupported_query", "WeRead total notes supports only all-time ranking.", "微信读书总笔记数仅支持全量排行。", &v2.Unsupported{Field: "range.period", Value: value})
		return &response
	}
	if chart.Query.GroupBy == nil || (*chart.Query.GroupBy != "book" && *chart.Query.GroupBy != "note_type") {
		value := "total"
		if chart.Query.GroupBy != nil {
			value = *chart.Query.GroupBy
		}
		response := v2.ErrorFor(request, "unsupported_query", "WeRead total notes supports only Book and Note type groupings.", "微信读书总笔记数仅支持按书籍和笔记类型分组。", &v2.Unsupported{Field: "query.group_by", Value: value})
		return &response
	}
	return nil
}

// ValidateSummary rejects source-owned selections before a Gateway is created.
func ValidateSummary(request v2.Request) *v2.Response {
	summary := request.Summary
	if len(summary.Query.Filters) != 0 || summary.Summary.Scope != "total" {
		response := v2.ErrorFor(request, "unsupported_query", "WeRead total notes supports only the all-time total.", "微信读书总笔记数仅支持全量总计。", &v2.Unsupported{Field: "summary.scope", Value: summary.Summary.Scope})
		return &response
	}
	return nil
}

func rankingSeries(counts []weread.NoteCount, groupBy, day string) ([]v2.Series, bool) {
	if groupBy == "book" {
		series := make([]v2.Series, 0, len(counts))
		for _, count := range counts {
			total, ok := count.Total()
			if !ok || count.BookID == "" || count.Label == "" {
				return nil, false
			}
			series = append(series, v2.Series{ID: "book:" + count.BookID, Label: v2.LocalizedText{EN: count.Label, ZhCN: count.Label}, Points: []v2.Point{{Date: day, Value: fmt.Sprintf("%d", total)}}})
		}
		return series, true
	}
	var highlights, thoughtsReviews, bookmarks uint64
	for _, count := range counts {
		if ^uint64(0)-highlights < count.Highlights || ^uint64(0)-thoughtsReviews < count.ThoughtsReviews || ^uint64(0)-bookmarks < count.Bookmarks {
			return nil, false
		}
		highlights += count.Highlights
		thoughtsReviews += count.ThoughtsReviews
		bookmarks += count.Bookmarks
	}
	return []v2.Series{
		{ID: "highlight", Label: v2.LocalizedText{EN: "Highlights", ZhCN: "划线"}, Points: []v2.Point{{Date: day, Value: fmt.Sprintf("%d", highlights)}}},
		{ID: "thought-review", Label: v2.LocalizedText{EN: "Thoughts & reviews", ZhCN: "想法与点评"}, Points: []v2.Point{{Date: day, Value: fmt.Sprintf("%d", thoughtsReviews)}}},
		{ID: "bookmark", Label: v2.LocalizedText{EN: "Bookmarks", ZhCN: "书签"}, Points: []v2.Point{{Date: day, Value: fmt.Sprintf("%d", bookmarks)}}},
	}, true
}

func sourceUnavailable(request v2.Request, err error) v2.Response {
	en, zh := "WeRead data is unavailable.", "微信读书数据暂不可用。"
	if err != nil && err.Error() == "WEREAD_API_KEY is not configured" {
		en, zh = "WEREAD_API_KEY is not configured. Export it in the shell that starts ccuv.", "未配置 WEREAD_API_KEY。请在启动 ccuv 的 Shell 中导出该环境变量。"
	}
	return v2.ErrorFor(request, "source_unavailable", en, zh, nil)
}
