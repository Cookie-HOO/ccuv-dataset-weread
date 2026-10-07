package protocol

import (
	"strings"
	"testing"
)

func timezone(value string) *string { return &value }

func TestDecodeRejectsTrailingJSON(t *testing.T) {
	_, err := DecodeRequest(strings.NewReader(`{} {}`))
	if err == nil {
		t.Fatal("accepted two documents")
	}
}

func TestDecodeRequestPreservesRangePeriodAndIgnoresUnknownFields(t *testing.T) {
	const input = `{
		"protocol":"ccuv.custom/v1",
		"request_id":"pane-1:2",
		"request_kind":"chart",
		"dataset_id":"weread",
		"timezone":"Asia/Shanghai",
		"range":{"start_date":"2026-09-01","end_date":"2026-09-30","period":"1mo","future_range_field":true},
		"query":{"filters":{},"group_by":null,"granularity":"day","future_query_field":"ignored"},
		"future_request_field":{"version":2}
	}`

	request, err := DecodeRequestBytes([]byte(input))
	if err != nil {
		t.Fatalf("DecodeRequestBytes() error = %v", err)
	}
	if request.Range == nil || request.Range.Period == nil || *request.Range.Period != "1mo" {
		t.Fatalf("range period = %#v, want 1mo", request.Range)
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("decoded request failed validation: %v", err)
	}
}

func TestInvalidRequestForPreservesRecoverableEnvelope(t *testing.T) {
	response, ok := InvalidRequestFor([]byte(`{"protocol":"ccuv.custom/v1","request_id":"pane-1:2","request_kind":"chart","dataset_id":"weread","timezone":"Asia/Shanghai","unknown":true}`))
	if !ok {
		t.Fatal("did not recover valid envelope")
	}
	if response.RequestID != "pane-1:2" || response.RequestKind != "chart" || response.DatasetID != DatasetID {
		t.Fatalf("response envelope = %#v", response)
	}
	if response.Error.Code != "invalid_request" {
		t.Fatalf("error code = %q", response.Error.Code)
	}
}

func TestInvalidRequestForRejectsUnrecoverableInput(t *testing.T) {
	for _, input := range [][]byte{[]byte(`{`), []byte(`{} {}`), []byte(`{"protocol":"ccuv.custom/v1","request_id":"Invalid-request","request_kind":"chart","dataset_id":"weread"}`)} {
		if _, ok := InvalidRequestFor(input); ok {
			t.Fatalf("recovered invalid input %s", input)
		}
	}
}

func TestValidateRejectsUnknownDataset(t *testing.T) {
	request := Request{Protocol: Version, RequestID: "request-one", RequestKind: "chart", DatasetID: "other", Timezone: timezone("Asia/Shanghai"), Range: &Range{StartDate: "2026-09-01", EndDate: "2026-09-01"}, Query: Query{Filters: map[string][]string{}, Granularity: "day"}}
	if err := request.Validate(); err == nil {
		t.Fatal("accepted unknown dataset")
	}
}

func TestValidateAcceptsCCUVPaneRequestID(t *testing.T) {
	request := Request{Protocol: Version, RequestID: "pane-1:2", RequestKind: "chart", DatasetID: DatasetID, Timezone: timezone("Asia/Shanghai"), Range: &Range{StartDate: "2026-09-01", EndDate: "2026-09-01"}, Query: Query{Filters: map[string][]string{}, Granularity: "day"}}
	if err := request.Validate(); err != nil {
		t.Fatalf("request was rejected: %v", err)
	}
}

func TestValidateAcceptsSupportedGroupBy(t *testing.T) {
	groupBy := "book"
	request := Request{Protocol: Version, RequestID: "pane-1:2", RequestKind: "chart", DatasetID: DatasetID, Timezone: timezone("Asia/Shanghai"), Range: &Range{StartDate: "2026-09-01", EndDate: "2026-09-01"}, Query: Query{Filters: map[string][]string{}, GroupBy: &groupBy, Granularity: "day"}}
	if err := request.Validate(); err != nil {
		t.Fatalf("rejected supported book grouping: %v", err)
	}
}

func TestValidateRequiresValidTimezone(t *testing.T) {
	base := Request{Protocol: Version, RequestID: "request-one", RequestKind: "chart", DatasetID: DatasetID, Range: &Range{StartDate: "2026-09-01", EndDate: "2026-09-01"}, Query: Query{Filters: map[string][]string{}, Granularity: "day"}}
	for name, zone := range map[string]*string{
		"missing": nil,
		"empty":   timezone(""),
		"invalid": timezone("Mars/Olympus"),
	} {
		t.Run(name, func(t *testing.T) {
			request := base
			request.Timezone = zone
			if err := request.Validate(); err == nil {
				t.Fatal("accepted invalid timezone")
			}
		})
	}
}

func TestProbeRequestValidatesWithoutTimezoneOrQuery(t *testing.T) {
	request := Request{Protocol: Version, RequestID: "probe-test", RequestKind: "probe"}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestProbeRequestRejectsQueryFields(t *testing.T) {
	request := Request{Protocol: Version, RequestID: "probe-test", RequestKind: "probe", Query: Query{Filters: map[string][]string{}}}
	if err := request.Validate(); err == nil {
		t.Fatal("probe query was accepted")
	}
}

func TestProbeResponseDeclaresRankingDefaults(t *testing.T) {
	request := Request{Protocol: Version, RequestID: "probe-test", RequestKind: "probe"}
	response := NewProbeResponse(request)
	defaults := response.Descriptor.Defaults["ranking"]
	if defaults.Period == nil || *defaults.Period != "1y" || defaults.GroupBy == nil || *defaults.GroupBy != "book" || defaults.Top == nil || *defaults.Top != 10 {
		t.Fatalf("ranking defaults = %#v", defaults)
	}
}

func TestChartResponseUsesMinuteUnit(t *testing.T) {
	request := Request{Protocol: Version, RequestID: "request-one", RequestKind: "chart", DatasetID: DatasetID, Timezone: timezone("Asia/Shanghai"), Range: &Range{StartDate: "2026-09-01", EndDate: "2026-09-01"}, Query: Query{Filters: map[string][]string{}, Granularity: "day"}}
	response := NewChartResponse(request, nil)
	if response.Dataset.Unit != (Unit{"minute", LocalizedText{"minutes", "分钟"}, "suffix", 2}) {
		t.Fatalf("unit = %#v", response.Dataset.Unit)
	}
	if len(response.Warnings) != 0 {
		t.Fatalf("warnings = %#v", response.Warnings)
	}
	if len(response.Capabilities.GroupBy) != 2 {
		t.Fatalf("global group_by capability = %#v", response.Capabilities.GroupBy)
	}
	for _, kind := range []string{"timeline", "calendar", "stack"} {
		got := response.Capabilities.Charts[kind]
		if len(got.GroupBy) != 0 || len(got.Filters) != 0 {
			t.Fatalf("%s capability = %#v", kind, got)
		}
	}
	ranking := response.Capabilities.Charts["ranking"]
	if len(ranking.GroupBy) != 2 || len(ranking.Filters) != 0 {
		t.Fatalf("ranking capability = %#v", ranking)
	}
	if len(response.Data.Series) != 1 || response.Data.Series[0].ID != "total" {
		t.Fatalf("series = %#v", response.Data.Series)
	}
}

func TestSuccessResponseWarnsForNonShanghaiTimezone(t *testing.T) {
	request := Request{RequestID: "request-one", Timezone: timezone("America/New_York")}
	chart := NewChartResponse(request, nil)
	summary := NewSummaryResponse(request, "1.02", nil, nil)
	want := Warning{"timezone_not_supported", LocalizedText{"WeRead dates use the Asia/Shanghai source timezone.", "微信读书日期使用 Asia/Shanghai 数据源时区。"}}
	if len(chart.Warnings) != 1 || chart.Warnings[0] != want {
		t.Fatalf("chart warnings = %#v", chart.Warnings)
	}
	if len(summary.Warnings) != 1 || summary.Warnings[0] != want {
		t.Fatalf("summary warnings = %#v", summary.Warnings)
	}
}

func TestSummaryResponseDisplaysMinutes(t *testing.T) {
	request := Request{RequestID: "request-one", Timezone: timezone("Asia/Shanghai")}
	response := NewSummaryResponse(request, "1.02", nil, nil)
	if response.Summary.Metric.Current.Display != (LocalizedText{"1.02 minutes", "1.02 分钟"}) {
		t.Fatalf("display = %#v", response.Summary.Metric.Current.Display)
	}
}
