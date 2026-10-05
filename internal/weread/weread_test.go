package weread

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/protocol"
)

func fixtureRequest(t *testing.T) protocol.Request {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "chart-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var request protocol.Request
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	return request
}

func TestChartReturnsMinuteTotalsAndAllCapabilities(t *testing.T) {
	handler := NewHandler(FakeGateway{Records: []DailyRecord{{Date: "2026-09-17", Seconds: "28"}, {Date: "2026-09-18", Seconds: "40"}}})
	response, ok := handler.Handle(context.Background(), fixtureRequest(t)).(protocol.ChartResponse)
	if !ok {
		t.Fatalf("response = %#v", response)
	}
	if got := response.Data.Series[0].Points; len(response.Data.Series) != 1 || len(got) != 2 || got[0].Value != "0.47" || got[1].Value != "0.67" {
		t.Fatalf("points = %#v", response.Data)
	}
	for _, kind := range []string{"timeline", "calendar", "stack"} {
		capability, ok := response.Capabilities.Charts[kind]
		if !ok || len(capability.Granularities) != 1 || capability.Granularities[0] != "day" || len(capability.GroupBy) != 0 {
			t.Fatalf("capability %s = %#v", kind, capability)
		}
	}
	ranking := response.Capabilities.Charts["ranking"]
	if len(ranking.GroupBy) != 2 || ranking.GroupBy[0] != "book" || ranking.GroupBy[1] != "category" {
		t.Fatalf("ranking capability = %#v", ranking)
	}
}

func TestChartRoundsExactSecondsToMinutes(t *testing.T) {
	request := fixtureRequest(t)
	request.Range = &protocol.Range{StartDate: "2026-09-17", EndDate: "2026-09-19"}
	handler := NewHandler(FakeGateway{Records: []DailyRecord{
		{Date: "2026-09-17", Seconds: "1"},
		{Date: "2026-09-18", Seconds: "3"},
		{Date: "2026-09-19", Seconds: "5999"},
	}})
	response, ok := handler.Handle(context.Background(), request).(protocol.ChartResponse)
	if !ok {
		t.Fatalf("response = %#v", response)
	}
	got := response.Data.Series[0].Points
	want := []string{"0.02", "0.05", "99.98"}
	for index, value := range want {
		if got[index].Value != value {
			t.Fatalf("point %d = %s, want %s", index, got[index].Value, value)
		}
	}
}

func TestSummarySumsRawSecondsBeforeConvertingToMinutes(t *testing.T) {
	zone := "Asia/Shanghai"
	request := protocol.Request{
		Protocol: protocol.Version, RequestID: "summary-one", RequestKind: "summary", DatasetID: protocol.DatasetID, Timezone: &zone,
		Query: protocol.Query{Filters: map[string][]string{}},
		Summary: &protocol.Summary{Scope: "fixed", Intervals: map[string]protocol.Range{
			"current": {StartDate: "2026-09-17", EndDate: "2026-09-18"},
		}},
	}
	response, ok := NewHandler(FakeGateway{Records: []DailyRecord{{Date: "2026-09-17", Seconds: "1"}, {Date: "2026-09-18", Seconds: "1"}}}).Handle(context.Background(), request).(protocol.SummaryResponse)
	if !ok {
		t.Fatalf("response = %#v", response)
	}
	if response.Summary.Metric.Current.Value != "0.03" {
		t.Fatalf("current = %#v", response.Summary.Metric.Current)
	}
}

func TestNonShanghaiTimezoneReturnsSuccessWarning(t *testing.T) {
	request := fixtureRequest(t)
	zone := "America/New_York"
	request.Timezone = &zone
	response, ok := NewHandler(FakeGateway{Records: []DailyRecord{{Date: "2026-09-17", Seconds: "28"}, {Date: "2026-09-18", Seconds: "40"}}}).Handle(context.Background(), request).(protocol.ChartResponse)
	if !ok || len(response.Warnings) != 1 || response.Warnings[0].Code != "timezone_not_supported" {
		t.Fatalf("response = %#v", response)
	}
}

func TestAbsentCoverageIsRangeNotAvailable(t *testing.T) {
	handler := NewHandler(FakeGateway{Records: []DailyRecord{{Date: "2026-09-17", Seconds: "28"}}})
	response, ok := handler.Handle(context.Background(), fixtureRequest(t)).(protocol.ErrorResponse)
	if !ok || response.Error.Code != "range_not_available" {
		t.Fatalf("response = %#v", response)
	}
}

func TestGroupedRankingRejectsUnsupportedPeriod(t *testing.T) {
	request := fixtureRequest(t)
	groupBy := "book"
	request.Query.GroupBy = &groupBy
	period := "14d"
	request.Range.Period = &period
	response, ok := NewHandler(FakeGateway{}).Handle(context.Background(), request).(protocol.ErrorResponse)
	if !ok || response.Error.Code != "unsupported_query" || response.Error.Unsupported == nil || response.Error.Unsupported.Field != "range.period" || response.Error.Unsupported.Value != "14d" {
		t.Fatalf("response = %#v", response)
	}
	if response.Error.Message.EN != "WeRead grouped summaries support only This week (1w), This month (1mo), and This year (1y). Switch to one of these periods." || response.Error.Message.ZhCN != "微信读书分组汇总仅支持本周（1w）、本月（1mo）和本年（1y）。请切换到这三个周期之一。" {
		t.Fatalf("message = %#v", response.Error.Message)
	}
}

func TestGroupedRankingRejectsUnsupportedGroupingBeforeCallingGateway(t *testing.T) {
	for _, groupBy := range []string{"author", "publisher"} {
		t.Run(groupBy, func(t *testing.T) {
			request := fixtureRequest(t)
			period := "1mo"
			request.Query.GroupBy = &groupBy
			request.Range.Period = &period
			request.Range.StartDate = "2026-09-01"

			response, ok := NewHandler(panicGateway{}).Handle(context.Background(), request).(protocol.ErrorResponse)
			if !ok || response.Error.Code != "unsupported_query" || response.Error.Unsupported == nil || response.Error.Unsupported.Field != "query.group_by" || response.Error.Unsupported.Value != groupBy {
				t.Fatalf("response = %#v", response)
			}
			if response.Error.Message.EN != "WeRead does not support the selected grouping. Supported groupings are Total, Book, and Category." || response.Error.Message.ZhCN != "微信读书不支持所选分组。支持的分组为总计、书籍和分类。" {
				t.Fatalf("message = %#v", response.Error.Message)
			}
		})
	}
}

type panicGateway struct{}

func (panicGateway) DailyRecords(context.Context, []protocol.Range) ([]DailyRecord, error) {
	panic("DailyRecords must not be called")
}

func (panicGateway) PeriodRanking(context.Context, protocol.Range, string) ([]RankedRecord, error) {
	panic("PeriodRanking must not be called")
}

func TestGroupedRankingFailureMessagesAreSafeAndCategorized(t *testing.T) {
	request := fixtureRequest(t)
	groupBy := "book"
	period := "1mo"
	request.Query.GroupBy = &groupBy
	request.Range.Period = &period
	request.Range.StartDate = "2026-09-01"

	for _, test := range []struct {
		name string
		err  error
		en   string
		zh   string
	}{
		{"period", newSourceFailure(sourceFailurePeriod), "could not match", "无法将所选周期匹配"},
		{"request", newSourceFailure(sourceFailureRequest), "could not complete", "无法完成"},
		{"upgrade", newSourceFailure(sourceFailureUpgrade), "requires a source upgrade", "需要完成数据源升级"},
		{"response", newSourceFailure(sourceFailureResponse), "invalid response", "无效响应"},
		{"ranking", newSourceFailure(sourceFailureRanking), "valid ranking data", "有效的排行数据"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response, ok := NewHandler(FakeGateway{Err: test.err}).Handle(context.Background(), request).(protocol.ErrorResponse)
			if !ok || response.Error.Code != "source_unavailable" {
				t.Fatalf("response = %#v", response)
			}
			if !strings.Contains(response.Error.Message.EN, test.en) || !strings.Contains(response.Error.Message.ZhCN, test.zh) {
				t.Fatalf("message = %#v", response.Error.Message)
			}
			if strings.Contains(response.Error.Message.EN, "test-key") || strings.Contains(response.Error.Message.ZhCN, "test-key") {
				t.Fatalf("message leaked a credential: %#v", response.Error.Message)
			}
		})
	}
}

func TestGroupedRankingUsesRangeEndForOfficialSummary(t *testing.T) {
	request := fixtureRequest(t)
	groupBy := "book"
	period := "1mo"
	request.Query.GroupBy = &groupBy
	request.Range.Period = &period
	request.Range.StartDate = "2026-09-01"
	handler := NewHandler(FakeGateway{RankingRecords: []RankedRecord{{ID: "book:42", Label: "Example", Seconds: "120"}}})
	response, ok := handler.Handle(context.Background(), request).(protocol.ChartResponse)
	if !ok || len(response.Data.Series) != 1 {
		t.Fatalf("response = %#v", response)
	}
	point := response.Data.Series[0].Points[0]
	if point.Date != request.Range.EndDate || point.Value != "2.00" {
		t.Fatalf("point = %#v", point)
	}
}

func TestUnsupportedQueryIsInvalidRequest(t *testing.T) {
	request := fixtureRequest(t)
	request.Query.Granularity = "month"
	response, ok := NewHandler(FakeGateway{}).Handle(context.Background(), request).(protocol.ErrorResponse)
	if !ok || response.Error.Code != "invalid_request" {
		t.Fatalf("response = %#v", response)
	}
}
