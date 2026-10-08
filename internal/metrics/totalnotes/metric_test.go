package totalnotes

import (
	"context"
	"testing"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/protocol/v2"
	"github.com/ccusage-viz/ccuv-dataset-weread/internal/weread"
)

func TestChartProducesAllTimeBookCounts(t *testing.T) {
	period, groupBy := "all", "book"
	request := v2.Request{RequestID: "notes-1", RequestKind: "chart", DatasetID: v2.DatasetID, MetricID: ID, Timezone: "Asia/Shanghai", Chart: &v2.ChartRequest{Range: v2.Range{StartDate: "1971-01-01", EndDate: "2026-10-09", Period: &period}, Query: v2.Query{Filters: map[string][]string{}, GroupBy: &groupBy, Granularity: "day"}}}
	response := New(weread.FakeGateway{Notes: []weread.NoteCount{{BookID: "42", Label: "Book", Count: 9}}}).Chart(context.Background(), request)
	series := response.Payload["data"].(map[string]any)["series"].([]v2.Series)
	if response.Status != "ok" || series[0].Points[0].Value != "9" {
		t.Fatalf("response = %#v", response)
	}
}

func TestSummaryRejectsNonTotalScopeBeforeGateway(t *testing.T) {
	request := v2.Request{RequestID: "notes-2", RequestKind: "summary", DatasetID: v2.DatasetID, MetricID: ID, Summary: &v2.SummaryRequest{Query: v2.Query{Filters: map[string][]string{}}, Summary: v2.Summary{Scope: "fixed", Intervals: map[string]v2.Range{"current": {StartDate: "1971-01-01", EndDate: "2026-10-09"}}}}}
	response := New(panicSource{}).Summary(context.Background(), request)
	if response.Error == nil || response.Error.Code != "unsupported_query" || response.Error.Unsupported == nil || response.Error.Unsupported.Field != "summary.scope" {
		t.Fatalf("response = %#v", response)
	}
}

type panicSource struct{}

func (panicSource) NotebookCounts(context.Context) ([]weread.NoteCount, error) {
	panic("unexpected source call")
}
