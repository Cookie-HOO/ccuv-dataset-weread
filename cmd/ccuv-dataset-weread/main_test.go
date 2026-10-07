package main

import (
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
