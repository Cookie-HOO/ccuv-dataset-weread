package readingtime

import (
	"context"
	"testing"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/protocol/v2"
	"github.com/ccusage-viz/ccuv-dataset-weread/internal/weread"
)

func TestChartFormatsRawSecondsAtWireBoundary(t *testing.T) {
	request := v2.Request{RequestID: "chart-1", RequestKind: "chart", DatasetID: v2.DatasetID, MetricID: ID, Timezone: "Asia/Shanghai", Chart: &v2.ChartRequest{Range: v2.Range{StartDate: "2026-09-17", EndDate: "2026-09-17"}, Query: v2.Query{Filters: map[string][]string{}, Granularity: "day"}}}
	response := New(weread.FakeGateway{Records: []weread.DailyRecord{{Date: "2026-09-17", Seconds: 28}}}).Chart(context.Background(), request)
	data := response.Payload["data"].(map[string]any)
	series := data["series"].([]v2.Series)
	if response.Status != "ok" || series[0].Points[0].Value != "0.47" {
		t.Fatalf("response = %#v", response)
	}
}

func TestGroupedChartRejectsUnsupportedPeriodBeforeGateway(t *testing.T) {
	period, groupBy := "14d", "book"
	request := v2.Request{RequestID: "chart-2", RequestKind: "chart", DatasetID: v2.DatasetID, MetricID: ID, Timezone: "Asia/Shanghai", Chart: &v2.ChartRequest{Range: v2.Range{StartDate: "2026-09-17", EndDate: "2026-09-30", Period: &period}, Query: v2.Query{Filters: map[string][]string{}, GroupBy: &groupBy, Granularity: "day"}}}
	response := New(panicSource{}).Chart(context.Background(), request)
	if response.Error == nil || response.Error.Code != "unsupported_query" || response.Error.Unsupported.Field != "range.period" {
		t.Fatalf("response = %#v", response)
	}
}

type panicSource struct{}

func (panicSource) DailyRecords(context.Context, []v2.Range) ([]weread.DailyRecord, error) {
	panic("unexpected source call")
}
func (panicSource) PeriodRanking(context.Context, v2.Range, string) ([]weread.ReadingRanking, error) {
	panic("unexpected source call")
}
