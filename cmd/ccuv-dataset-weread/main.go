// ccuv-dataset-weread implements the one-request ccuv custom command.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/protocol"
	"github.com/ccusage-viz/ccuv-dataset-weread/internal/weread"
)

func main() {
	if len(os.Args) != 2 || os.Args[1] != "ccuv" {
		fmt.Fprintln(os.Stderr, "usage: ccuv-dataset-weread ccuv")
		os.Exit(2)
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read request:", err)
		os.Exit(1)
	}
	response, err := handle(input, func() (weread.Gateway, error) {
		return weread.NewHTTPGatewayFromEnvironment()
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	write(response)
}

func handle(input []byte, gatewayFactory func() (weread.Gateway, error)) (any, error) {
	var envelope struct {
		Protocol    string `json:"protocol"`
		RequestID   string `json:"request_id"`
		RequestKind string `json:"request_kind"`
		MetricID    string `json:"metric_id"`
	}
	if err := json.Unmarshal(input, &envelope); err == nil && envelope.Protocol == "ccuv.custom/v2" {
		return handleV2(input, envelope, gatewayFactory)
	}
	request, err := protocol.DecodeRequestBytes(input)
	if err != nil {
		if response, ok := protocol.InvalidRequestFor(input); ok {
			return response, nil
		}
		return nil, fmt.Errorf("decode request: %w", err)
	}
	if err := request.Validate(); err != nil {
		return protocol.ErrorFor(request, "invalid_request", "The request is invalid.", "请求无效。"), nil
	}
	if request.RequestKind == "probe" {
		return protocol.NewProbeResponse(request), nil
	}
	if response := weread.SelectionError(request); response != nil {
		return *response, nil
	}
	gateway, err := gatewayFactory()
	if err != nil {
		return sourceUnavailableV1(request, err), nil
	}
	return weread.NewHandler(gateway).Handle(context.Background(), request), nil
}

func handleV2(input []byte, envelope struct {
	Protocol    string `json:"protocol"`
	RequestID   string `json:"request_id"`
	RequestKind string `json:"request_kind"`
	MetricID    string `json:"metric_id"`
}, gatewayFactory func() (weread.Gateway, error)) (any, error) {
	if envelope.RequestKind == "probe" {
		return map[string]any{
			"protocol": "ccuv.custom/v2", "request_id": envelope.RequestID, "request_kind": "probe", "dataset_id": protocol.DatasetID, "status": "ok",
			"descriptor": v2Descriptor(),
		}, nil
	}
	if envelope.RequestKind == "chart" && envelope.MetricID == "total_notes" {
		return handleV2TotalNotes(input, envelope, gatewayFactory)
	}
	if envelope.RequestKind == "summary" && envelope.MetricID == "total_notes" {
		return handleV2TotalNotesSummary(envelope, gatewayFactory)
	}
	if (envelope.RequestKind != "chart" && envelope.RequestKind != "summary") || envelope.MetricID != "reading_time" {
		return map[string]any{
			"protocol": "ccuv.custom/v2", "request_id": envelope.RequestID, "request_kind": envelope.RequestKind, "dataset_id": protocol.DatasetID, "status": "error",
			"error": map[string]any{"code": "unsupported_chart_kind", "message": map[string]string{"en": "WeRead supports only the reading_time historical metric.", "zh-CN": "微信读书仅支持阅读时长历史指标。"}},
		}, nil
	}
	var request map[string]any
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, err
	}
	request["protocol"] = protocol.Version
	delete(request, "metric_id")
	legacy, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	result, err := handle(legacy, gatewayFactory)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var response map[string]any
	if err := json.Unmarshal(encoded, &response); err != nil {
		return nil, err
	}
	response["protocol"] = "ccuv.custom/v2"
	response["metric_id"] = "reading_time"
	return response, nil
}

func handleV2TotalNotesSummary(envelope struct {
	Protocol    string `json:"protocol"`
	RequestID   string `json:"request_id"`
	RequestKind string `json:"request_kind"`
	MetricID    string `json:"metric_id"`
}, gatewayFactory func() (weread.Gateway, error)) (any, error) {
	gateway, err := gatewayFactory()
	if err != nil {
		return sourceUnavailableV2(envelope, err), nil
	}
	notebooks, ok := gateway.(weread.NotebookRankingGateway)
	if !ok {
		return sourceUnavailableV2(envelope, nil), nil
	}
	records, err := notebooks.NotebookRanking(context.Background())
	if err != nil {
		return sourceUnavailableV2(envelope, err), nil
	}
	var total uint64
	for _, record := range records {
		value, err := strconv.ParseUint(record.Seconds, 10, 64)
		if err != nil || ^uint64(0)-total < value {
			return sourceUnavailableV2(envelope, err), nil
		}
		total += value
	}
	value := strconv.FormatUint(total, 10)
	return map[string]any{
		"protocol": "ccuv.custom/v2", "request_id": envelope.RequestID, "request_kind": "summary", "dataset_id": protocol.DatasetID, "metric_id": "total_notes", "status": "ok",
		"summary": map[string]any{"metric": map[string]any{
			"id": "total_notes", "label": map[string]string{"en": "Total notes", "zh-CN": "总笔记数"},
			"current":    map[string]any{"value": value, "display": map[string]string{"en": value + " notes", "zh-CN": value + " 条笔记"}},
			"sequential": map[string]any{"status": "unavailable"}, "year_over_year": map[string]any{"status": "unavailable"},
		}},
	}, nil
}

func handleV2TotalNotes(input []byte, envelope struct {
	Protocol    string `json:"protocol"`
	RequestID   string `json:"request_id"`
	RequestKind string `json:"request_kind"`
	MetricID    string `json:"metric_id"`
}, gatewayFactory func() (weread.Gateway, error)) (any, error) {
	var request struct {
		Range struct {
			Period  *string `json:"period"`
			EndDate string  `json:"end_date"`
		} `json:"range"`
		Query struct {
			GroupBy *string `json:"group_by"`
		} `json:"query"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, err
	}
	if request.Range.Period == nil || *request.Range.Period != "all" {
		value := "null"
		if request.Range.Period != nil {
			value = *request.Range.Period
		}
		return unsupportedV2(envelope, "range.period", value, "WeRead total notes supports only all-time ranking.", "微信读书总笔记数仅支持全量排行。"), nil
	}
	if request.Query.GroupBy == nil || *request.Query.GroupBy != "book" {
		value := "total"
		if request.Query.GroupBy != nil {
			value = *request.Query.GroupBy
		}
		return unsupportedV2(envelope, "query.group_by", value, "WeRead total notes supports only Book grouping.", "微信读书总笔记数仅支持按书籍分组。"), nil
	}
	gateway, err := gatewayFactory()
	if err != nil {
		return sourceUnavailableV2(envelope, err), nil
	}
	notebooks, ok := gateway.(weread.NotebookRankingGateway)
	if !ok {
		return sourceUnavailableV2(envelope, nil), nil
	}
	records, err := notebooks.NotebookRanking(context.Background())
	if err != nil {
		return sourceUnavailableV2(envelope, err), nil
	}
	series := make([]map[string]any, 0, len(records))
	for _, record := range records {
		series = append(series, map[string]any{"id": record.ID, "label": map[string]string{"en": record.Label, "zh-CN": record.Label}, "points": []any{map[string]any{"date": request.Range.EndDate, "value": record.Seconds}}})
	}
	return map[string]any{
		"protocol": "ccuv.custom/v2", "request_id": envelope.RequestID, "request_kind": "chart", "dataset_id": protocol.DatasetID, "metric_id": "total_notes", "status": "ok",
		"dataset":      map[string]any{"title": map[string]string{"en": "WeRead", "zh-CN": "微信读书"}, "description": map[string]string{"en": "All-time notes", "zh-CN": "全量笔记"}, "unit": map[string]any{"id": "note", "label": map[string]string{"en": "notes", "zh-CN": "笔记"}, "placement": "suffix", "decimal_places": 0}},
		"capabilities": map[string]any{"group_by": []any{map[string]any{"id": "book", "label": map[string]string{"en": "Book", "zh-CN": "书籍"}}}, "filters": []any{}, "charts": map[string]any{"ranking": map[string]any{"granularities": []string{"day"}, "group_by": []string{"book"}, "filters": []any{}, "periods": []string{"all"}}}},
		"data":         map[string]any{"series": series},
	}, nil
}

func unsupportedV2(envelope struct {
	Protocol    string `json:"protocol"`
	RequestID   string `json:"request_id"`
	RequestKind string `json:"request_kind"`
	MetricID    string `json:"metric_id"`
}, field, value, en, zh string) map[string]any {
	return map[string]any{"protocol": "ccuv.custom/v2", "request_id": envelope.RequestID, "request_kind": envelope.RequestKind, "dataset_id": protocol.DatasetID, "metric_id": envelope.MetricID, "status": "error", "error": map[string]any{"code": "unsupported_query", "message": map[string]string{"en": en, "zh-CN": zh}, "unsupported": map[string]string{"field": field, "value": value}}}
}

func sourceUnavailableV1(request protocol.Request, err error) protocol.ErrorResponse {
	if err != nil && strings.Contains(err.Error(), "WEREAD_API_KEY is not configured") {
		return protocol.ErrorFor(
			request,
			"source_unavailable",
			"WEREAD_API_KEY is not configured. Export it in the shell that starts ccuv.",
			"未配置 WEREAD_API_KEY。请在启动 ccuv 的 Shell 中导出该环境变量。",
		)
	}
	return protocol.ErrorFor(request, "source_unavailable", "WeRead data is unavailable.", "微信读书数据暂不可用。")
}

func sourceUnavailableV2(envelope struct {
	Protocol    string `json:"protocol"`
	RequestID   string `json:"request_id"`
	RequestKind string `json:"request_kind"`
	MetricID    string `json:"metric_id"`
}, err error) map[string]any {
	en, zh := "WeRead data is unavailable.", "微信读书数据暂不可用。"
	if err != nil && strings.Contains(err.Error(), "WEREAD_API_KEY is not configured") {
		en = "WEREAD_API_KEY is not configured. Export it in the shell that starts ccuv."
		zh = "未配置 WEREAD_API_KEY。请在启动 ccuv 的 Shell 中导出该环境变量。"
	}
	return map[string]any{"protocol": "ccuv.custom/v2", "request_id": envelope.RequestID, "request_kind": envelope.RequestKind, "dataset_id": protocol.DatasetID, "metric_id": envelope.MetricID, "status": "error", "error": map[string]any{"code": "source_unavailable", "message": map[string]string{"en": en, "zh-CN": zh}}}
}

func v2Descriptor() map[string]any {
	period14d, period1y := "14d", "1y"
	book, top10 := "book", 10
	text := func(en, zh string) map[string]string { return map[string]string{"en": en, "zh-CN": zh} }
	chart := func(granularities []string, groupBy any, periods []string) map[string]any {
		result := map[string]any{"granularities": granularities, "group_by": groupBy, "filters": []any{}}
		if len(periods) > 0 {
			result["periods"] = periods
		}
		return result
	}
	return map[string]any{
		"revision": "weread-2", "version": "0.0.1", "source_timezone": "Asia/Shanghai", "default_metric_id": "reading_time",
		"dataset": map[string]any{"title": text("WeRead", "微信读书"), "description": text("WeRead reading metrics", "微信读书阅读指标")},
		"metrics": []any{map[string]any{
			"id": "reading_time", "title": text("Reading time", "阅读时长"), "description": text("Daily reading time from your WeRead activity.", "基于微信读书活动统计每日阅读时长。"),
			"unit": map[string]any{"id": "minute", "label": text("minutes", "分钟"), "placement": "suffix", "decimal_places": 2}, "aggregation": "sum", "summary": "comparisons",
			"capabilities": map[string]any{
				"group_by": []any{map[string]any{"id": "book", "label": text("Book", "书籍")}, map[string]any{"id": "category", "label": text("Category", "分类")}},
				"filters":  []any{},
				"charts": map[string]any{
					"timeline": chart([]string{"day"}, []string{}, nil),
					"calendar": chart([]string{"day"}, []string{}, nil),
					"stack":    chart([]string{"day"}, []string{}, nil),
					"ranking":  chart([]string{"day"}, []string{"book", "category"}, []string{"1d", "7d", "14d", "30d", "90d", "180d", "365d", "1w", "1mo", "1y"}),
				},
			},
			"defaults": map[string]any{
				"timeline": map[string]any{"period": &period14d, "group_by": nil, "top": nil}, "calendar": map[string]any{"period": &period14d, "group_by": nil, "top": nil}, "stack": map[string]any{"period": &period14d, "group_by": nil, "top": nil}, "ranking": map[string]any{"period": &period1y, "group_by": &book, "top": &top10},
			},
		}, map[string]any{
			"id": "total_notes", "title": text("Total notes", "总笔记数"), "description": text("Current all-time notes: highlights, thoughts, and bookmarks.", "当前全量笔记数：划线、想法和书签。"),
			"unit": map[string]any{"id": "note", "label": text("notes", "笔记"), "placement": "suffix", "decimal_places": 0}, "aggregation": "sum", "summary": "total",
			"capabilities": map[string]any{"group_by": []any{map[string]any{"id": "book", "label": text("Book", "书籍")}}, "filters": []any{}, "charts": map[string]any{"ranking": chart([]string{"day"}, []any{"book"}, []string{"all"})}},
			"defaults":     map[string]any{"ranking": map[string]any{"period": "all", "group_by": "book", "top": 10}},
		}},
		"environment": []any{map[string]any{"name": "WEREAD_API_KEY", "required": true, "sensitive": true, "label": text("WeRead API key", "微信读书 API 密钥"), "how_to_get": text("Export WEREAD_API_KEY in the shell that starts ccuv.", "请在启动 ccuv 的 Shell 中导出 WEREAD_API_KEY。"), "help_url": "https://weread.qq.com/r/weread-skills"}},
	}
}

func write(response any) {
	encoded, err := json.Marshal(response)
	if err != nil {
		fmt.Fprintln(os.Stderr, "encode response:", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(append(encoded, '\n')); err != nil {
		fmt.Fprintln(os.Stderr, "write response:", err)
		os.Exit(1)
	}
}
