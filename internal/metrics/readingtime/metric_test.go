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

func TestDescriptorDeclaresTheBookRankingSourceLimit(t *testing.T) {
	capability := Descriptor().Capabilities.Charts["ranking"]
	if capability.MaxItems == nil || *capability.MaxItems != 10 || capability.Comparison != "none" {
		t.Fatalf("ranking capability = %#v", capability)
	}
}

func TestBookRankingAddsReadablePresentation(t *testing.T) {
	period, groupBy := "1mo", "book"
	request := v2.Request{RequestID: "ranking-1", RequestKind: "chart", DatasetID: v2.DatasetID, MetricID: ID, Timezone: "Asia/Shanghai", Chart: &v2.ChartRequest{Range: v2.Range{StartDate: "2026-10-01", EndDate: "2026-10-10", Period: &period}, Query: v2.Query{Filters: map[string][]string{}, GroupBy: &groupBy, Granularity: "day"}}}
	response := New(weread.FakeGateway{RankingRecords: []weread.ReadingRanking{{ID: "book:42", Label: "Book", Seconds: 7440, Author: "Author", ReaderLink: "https://weread.qq.com/web/reader/42"}}}).Chart(context.Background(), request)
	series := response.Payload["data"].(map[string]any)["series"].([]v2.Series)
	item := series[0]
	if response.Status != "ok" || item.Display == nil || item.Display.EN != "2h 4m" || item.Display.ZhCN != "2小时4分钟" || item.Description == nil || item.Description.EN != "Author" || item.Link != "https://weread.qq.com/web/reader/42" {
		t.Fatalf("response = %#v", response)
	}
}

func TestCategoryRankingOmitsBookPresentation(t *testing.T) {
	period, groupBy := "1mo", "category"
	request := v2.Request{RequestID: "ranking-2", RequestKind: "chart", DatasetID: v2.DatasetID, MetricID: ID, Timezone: "Asia/Shanghai", Chart: &v2.ChartRequest{Range: v2.Range{StartDate: "2026-10-01", EndDate: "2026-10-10", Period: &period}, Query: v2.Query{Filters: map[string][]string{}, GroupBy: &groupBy, Granularity: "day"}}}
	response := New(weread.FakeGateway{RankingRecords: []weread.ReadingRanking{{ID: "category:7", Label: "History", Seconds: 59, Author: "Ignored", ReaderLink: "https://weread.qq.com/web/reader/42"}}}).Chart(context.Background(), request)
	item := response.Payload["data"].(map[string]any)["series"].([]v2.Series)[0]
	if item.Display == nil || item.Display.EN != "0m" || item.Description != nil || item.Link != "" {
		t.Fatalf("series = %#v", item)
	}
}

type panicSource struct{}

func (panicSource) DailyRecords(context.Context, []v2.Range) ([]weread.DailyRecord, error) {
	panic("unexpected source call")
}
func (panicSource) PeriodRanking(context.Context, v2.Range, string) ([]weread.ReadingRanking, error) {
	panic("unexpected source call")
}
