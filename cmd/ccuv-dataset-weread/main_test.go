package main

import (
	"encoding/json"
	"testing"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/protocol"
	"github.com/ccusage-viz/ccuv-dataset-weread/internal/weread"
)

func TestHandleProbeDoesNotCreateGatewayAndReturnsRankingDefaults(t *testing.T) {
	input := []byte(`{
		"protocol":"ccuv.custom/v1",
		"request_id":"probe-test",
		"request_kind":"probe"
	}`)
	called := false

	response, err := handle(input, func() (weread.Gateway, error) {
		called = true
		return nil, nil
	})

	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("gateway factory was called")
	}
	result, ok := response.(protocol.ProbeResponse)
	if !ok {
		t.Fatalf("response = %#v", response)
	}
	if result.DatasetID != protocol.DatasetID {
		t.Fatalf("dataset_id = %q", result.DatasetID)
	}
	defaults := result.Descriptor.Defaults["ranking"]
	if defaults.Period == nil || *defaults.Period != "1y" || defaults.GroupBy == nil || *defaults.GroupBy != "book" || defaults.Top == nil || *defaults.Top != 10 {
		t.Fatalf("ranking defaults = %#v", defaults)
	}
	if len(result.Descriptor.Environment) != 1 || result.Descriptor.Environment[0].Name != "WEREAD_API_KEY" || !result.Descriptor.Environment[0].Required || !result.Descriptor.Environment[0].Sensitive {
		t.Fatalf("environment = %#v", result.Descriptor.Environment)
	}
}

func TestHandleV2ProbeDefaultsToReadingTimeWithoutMonitor(t *testing.T) {
	input := []byte(`{"protocol":"ccuv.custom/v2","request_id":"probe-v2","request_kind":"probe"}`)

	response, err := handle(input, func() (weread.Gateway, error) {
		t.Fatal("gateway factory was called")
		return nil, nil
	})

	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Protocol   string `json:"protocol"`
		Descriptor struct {
			DefaultMetricID string `json:"default_metric_id"`
			Metrics         []struct {
				ID           string `json:"id"`
				Capabilities struct {
					Charts map[string]json.RawMessage `json:"charts"`
				} `json:"capabilities"`
			} `json:"metrics"`
		} `json:"descriptor"`
	}
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	if body.Protocol != "ccuv.custom/v2" || body.Descriptor.DefaultMetricID != "reading_time" || len(body.Descriptor.Metrics) != 2 {
		t.Fatalf("unexpected V2 probe: %s", encoded)
	}
	charts := body.Descriptor.Metrics[0].Capabilities.Charts
	if body.Descriptor.Metrics[0].ID != "reading_time" || body.Descriptor.Metrics[1].ID != "total_notes" || charts["monitor"] != nil || body.Descriptor.Metrics[1].Capabilities.Charts["monitor"] != nil {
		t.Fatalf("unexpected metric capability: %s", encoded)
	}
}

func TestHandleV2TotalNotesSupportsOnlyAllBookRanking(t *testing.T) {
	input := []byte(`{"protocol":"ccuv.custom/v2","request_id":"notes-v2","request_kind":"chart","dataset_id":"weread","metric_id":"total_notes","timezone":"Asia/Shanghai","range":{"start_date":"1971-01-01","end_date":"2026-10-08","period":"all"},"query":{"filters":{},"group_by":"book","granularity":"day"}}`)
	response, err := handle(input, func() (weread.Gateway, error) {
		return weread.FakeGateway{NotebookRecords: []weread.RankedRecord{{ID: "book:42", Label: "Book", Seconds: "9"}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(response)
	var body struct {
		Status   string `json:"status"`
		MetricID string `json:"metric_id"`
		Data     struct {
			Series []any `json:"series"`
		} `json:"data"`
	}
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ok" || body.MetricID != "total_notes" || len(body.Data.Series) != 1 {
		t.Fatalf("unexpected notes response: %s", encoded)
	}
}

func TestHandleRejectsAuthorBeforeCreatingGateway(t *testing.T) {
	input := []byte(`{
		"protocol":"ccuv.custom/v1",
		"request_id":"author-test",
		"request_kind":"chart",
		"dataset_id":"weread",
		"timezone":"Asia/Shanghai",
		"range":{"start_date":"2026-09-01","end_date":"2026-09-30","period":"1mo"},
		"query":{"filters":{},"group_by":"author","granularity":"day"}
	}`)
	called := false

	response, err := handle(input, func() (weread.Gateway, error) {
		called = true
		return nil, nil
	})

	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("gateway factory was called")
	}
	result, ok := response.(protocol.ErrorResponse)
	if !ok || result.Error.Code != "unsupported_query" || result.Error.Unsupported == nil || result.Error.Unsupported.Field != "query.group_by" || result.Error.Unsupported.Value != "author" {
		t.Fatalf("response = %#v", response)
	}
}

func TestHandleRejectsUnsupportedGroupedPeriodBeforeCreatingGateway(t *testing.T) {
	input := []byte(`{
		"protocol":"ccuv.custom/v1",
		"request_id":"period-test",
		"request_kind":"chart",
		"dataset_id":"weread",
		"timezone":"Asia/Shanghai",
		"range":{"start_date":"2026-09-17","end_date":"2026-09-30","period":"14d"},
		"query":{"filters":{},"group_by":"book","granularity":"day"}
	}`)
	called := false

	response, err := handle(input, func() (weread.Gateway, error) {
		called = true
		return nil, nil
	})

	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("gateway factory was called")
	}
	result, ok := response.(protocol.ErrorResponse)
	if !ok {
		t.Fatalf("response = %#v", response)
	}
	if result.Error.Code != "unsupported_query" || result.Error.Unsupported == nil {
		t.Fatalf("response = %#v", result)
	}
	if result.Error.Unsupported.Field != "range.period" || result.Error.Unsupported.Value != "14d" {
		t.Fatalf("unsupported = %#v", result.Error.Unsupported)
	}
}
