package weread

import (
	"context"
	"testing"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/protocol/v2"
)

func TestFakeGatewayCopiesTypedFacts(t *testing.T) {
	gateway := FakeGateway{Records: []DailyRecord{{Date: "2026-09-17", Seconds: 60}}, RankingRecords: []ReadingRanking{{ID: "book:1", Label: "Book", Seconds: 120}}, Notes: []NoteCount{{BookID: "1", Label: "Book", Count: 3}}}
	records, err := gateway.DailyRecords(context.Background(), []v2.Range{{StartDate: "2026-09-17", EndDate: "2026-09-17"}})
	if err != nil || records[0].Seconds != 60 {
		t.Fatalf("records = %#v, err = %v", records, err)
	}
	records[0].Seconds = 0
	if gateway.Records[0].Seconds != 60 {
		t.Fatal("gateway leaked record backing storage")
	}
	counts, err := gateway.NotebookCounts(context.Background())
	if err != nil || counts[0].Count != 3 {
		t.Fatalf("counts = %#v, err = %v", counts, err)
	}
}
