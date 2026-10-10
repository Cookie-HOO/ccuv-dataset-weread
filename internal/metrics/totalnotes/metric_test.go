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
	response := New(weread.FakeGateway{Notes: []weread.NoteCount{{BookID: "42", Label: "Book", Highlights: 2, ThoughtsReviews: 3, Bookmarks: 4}}}).Chart(context.Background(), request)
	series := response.Payload["data"].(map[string]any)["series"].([]v2.Series)
	if response.Status != "ok" || series[0].Points[0].Value != "9" {
		t.Fatalf("response = %#v", response)
	}
}

func TestChartProducesNoteTypeComposition(t *testing.T) {
	period, groupBy := "all", "note_type"
	request := v2.Request{RequestID: "notes-type", RequestKind: "chart", DatasetID: v2.DatasetID, MetricID: ID, Timezone: "Asia/Shanghai", Chart: &v2.ChartRequest{Range: v2.Range{StartDate: "1971-01-01", EndDate: "2026-10-09", Period: &period}, Query: v2.Query{Filters: map[string][]string{}, GroupBy: &groupBy, Granularity: "day"}}}
	response := New(weread.FakeGateway{Notes: []weread.NoteCount{{BookID: "1", Label: "One", Highlights: 2, ThoughtsReviews: 3, Bookmarks: 5}, {BookID: "2", Label: "Two", Highlights: 7, ThoughtsReviews: 11, Bookmarks: 13}}}).Chart(context.Background(), request)
	series := response.Payload["data"].(map[string]any)["series"].([]v2.Series)
	if response.Status != "ok" || len(series) != 3 || series[0].ID != "highlight" || series[0].Points[0].Value != "9" || series[1].ID != "thought-review" || series[1].Points[0].Value != "14" || series[2].ID != "bookmark" || series[2].Points[0].Value != "18" {
		t.Fatalf("response = %#v", response)
	}
}

func TestDescriptorDeclaresNoteTypeGrouping(t *testing.T) {
	descriptor := Descriptor()
	if len(descriptor.Capabilities.GroupBy) != 2 || descriptor.Capabilities.GroupBy[0].ID != "book" || descriptor.Capabilities.GroupBy[1].ID != "note_type" {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	capability := descriptor.Capabilities.Charts["ranking"]
	if len(capability.GroupBy) != 2 || capability.GroupBy[0] != "book" || capability.GroupBy[1] != "note_type" || len(capability.Periods) != 1 || capability.Periods[0] != "all" {
		t.Fatalf("capability = %#v", capability)
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
