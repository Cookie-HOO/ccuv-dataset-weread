// ccuv-dataset-weread implements the one-request ccuv custom command.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

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
		return protocol.ErrorFor(request, "source_unavailable", "WeRead data is unavailable.", "微信读书数据暂不可用。"), nil
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
	if envelope.RequestKind != "chart" || envelope.MetricID != "reading_time" {
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

func v2Descriptor() map[string]any {
	period14d, period1y := "14d", "1y"
	book, top10 := "book", 10
	text := func(en, zh string) map[string]string { return map[string]string{"en": en, "zh-CN": zh} }
	chart := func(granularities, groupBy, periods []string) map[string]any {
		return map[string]any{"granularities": granularities, "group_by": groupBy, "filters": []any{}, "periods": periods}
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
