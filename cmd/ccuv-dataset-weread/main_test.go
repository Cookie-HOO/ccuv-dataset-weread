package main

import (
	"encoding/json"
	"testing"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/dataset"
	"github.com/ccusage-viz/ccuv-dataset-weread/internal/weread"
)

func TestProbeDoesNotCreateGateway(t *testing.T) {
	response, err := handle([]byte(`{"protocol":"ccuv.custom/v2","request_id":"probe-v2","request_kind":"probe"}`), func() (dataset.Gateway, error) {
		t.Fatal("gateway was created")
		return nil, nil
	})
	if err != nil || response.Status != "ok" {
		t.Fatalf("response = %#v, err = %v", response, err)
	}
	encoded, _ := json.Marshal(response)
	var body struct {
		Descriptor struct {
			DefaultMetricID string `json:"default_metric_id"`
			Metrics         []struct {
				ID string `json:"id"`
			} `json:"metrics"`
		} `json:"descriptor"`
	}
	if json.Unmarshal(encoded, &body) != nil || body.Descriptor.DefaultMetricID != "reading_time" || len(body.Descriptor.Metrics) != 2 || body.Descriptor.Metrics[1].ID != "total_notes" {
		t.Fatalf("unexpected probe: %s", encoded)
	}
}

func TestHandleRoutesTotalNotesChart(t *testing.T) {
	input := []byte(`{"protocol":"ccuv.custom/v2","request_id":"notes-v2","request_kind":"chart","dataset_id":"weread","metric_id":"total_notes","timezone":"Asia/Shanghai","range":{"start_date":"1971-01-01","end_date":"2026-10-08","period":"all"},"query":{"filters":{},"group_by":"book","granularity":"day"}}`)
	response, err := handle(input, func() (dataset.Gateway, error) {
		return weread.FakeGateway{Notes: []weread.NoteCount{{BookID: "42", Label: "Book", Count: 9}}}, nil
	})
	if err != nil || response.Status != "ok" || response.MetricID != "total_notes" {
		t.Fatalf("response = %#v, err = %v", response, err)
	}
	encoded, _ := json.Marshal(response)
	if string(encoded) == "" || !contains(encoded, []byte(`"value":"9"`)) {
		t.Fatalf("response must retain decimal strings: %s", encoded)
	}
}

func TestHandleRejectsV1(t *testing.T) {
	if _, err := handle([]byte(`{"protocol":"ccuv.custom/v1"}`), func() (dataset.Gateway, error) { t.Fatal("gateway was created"); return nil, nil }); err == nil {
		t.Fatal("accepted V1")
	}
}

func contains(value, item []byte) bool {
	for start := 0; start+len(item) <= len(value); start++ {
		matched := true
		for index := range item {
			if value[start+index] != item[index] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}
