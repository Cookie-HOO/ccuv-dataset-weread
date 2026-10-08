package dataset

import (
	"context"
	"reflect"
	"testing"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/protocol/v2"
	"github.com/ccusage-viz/ccuv-dataset-weread/internal/weread"
)

func TestManifestUsesProbeDescriptorAndWindowsEntrypoint(t *testing.T) {
	manifest, err := BuildManifest("windows", "amd64")
	if err != nil || manifest.Entrypoint != "bin/ccuv-dataset-weread.exe" || !reflect.DeepEqual(manifest.Probe.Descriptor, Descriptor()) {
		t.Fatalf("manifest = %#v, err = %v", manifest, err)
	}
}

func TestUnknownMetricDoesNotCreateGateway(t *testing.T) {
	request := v2.Request{RequestID: "request-1", RequestKind: "chart", DatasetID: v2.DatasetID, MetricID: "other_metric"}
	response := NewRegistry(func() (Gateway, error) { t.Fatal("gateway was created"); return weread.FakeGateway{}, nil }).Handle(context.Background(), request)
	if response.Error == nil || response.Error.Code != "unsupported_chart_kind" {
		t.Fatalf("response = %#v", response)
	}
}

func TestUnsupportedMetricQueryDoesNotCreateGateway(t *testing.T) {
	period, groupBy := "14d", "book"
	request := v2.Request{RequestID: "request-2", RequestKind: "chart", DatasetID: v2.DatasetID, MetricID: "total_notes", Chart: &v2.ChartRequest{Range: v2.Range{StartDate: "2026-09-01", EndDate: "2026-09-14", Period: &period}, Query: v2.Query{Filters: map[string][]string{}, GroupBy: &groupBy, Granularity: "day"}}}
	response := NewRegistry(func() (Gateway, error) { t.Fatal("gateway was created"); return weread.FakeGateway{}, nil }).Handle(context.Background(), request)
	if response.Error == nil || response.Error.Code != "unsupported_query" {
		t.Fatalf("response = %#v", response)
	}
}
