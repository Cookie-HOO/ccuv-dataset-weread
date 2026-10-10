package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const testDescriptor = `{"revision":"weread-2","version":"0.0.1","default_metric_id":"reading_time","dataset":{"title":{"en":"WeRead","zh-CN":"微信读书"},"description":{"en":"Metrics","zh-CN":"指标"}},"metrics":[{"id":"reading_time"}],"environment":[]}`

func archiveWithProbeExecutable(t *testing.T, datasetID string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "dataset.tar.gz")
	manifest := fmt.Sprintf(`{"schema":"ccuv.dataset/v1","protocol":"ccuv.custom/v2","entrypoint":"bin/ccuv-dataset-weread","platform":{"os":"darwin","arch":"arm64"},"probe":{"dataset_id":"weread","descriptor":%s}}`, testDescriptor)
	executable := fmt.Sprintf("#!/bin/sh\ncat >/dev/null\nprintf '%%s\\n' '%s'\n", fmt.Sprintf(`{"protocol":"ccuv.custom/v2","request_id":"package-probe","request_kind":"probe","dataset_id":"%s","status":"ok","descriptor":%s}`, datasetID, testDescriptor))
	file, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, member := range []struct {
		name string
		data string
		mode int64
	}{
		{"manifest.json", manifest, 0644},
		{"bin/ccuv-dataset-weread", executable, 0755},
	} {
		header := &tar.Header{Name: member.name, Mode: member.mode, Size: int64(len(member.data)), Typeflag: tar.TypeReg}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write([]byte(member.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestAcceptProbeAcceptsMatchingV2Archive(t *testing.T) {
	if err := acceptProbe(archiveWithProbeExecutable(t, "weread")); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptProbeRejectsMismatchedDatasetID(t *testing.T) {
	if err := acceptProbe(archiveWithProbeExecutable(t, "wrong")); err == nil {
		t.Fatal("accepted mismatched Probe Dataset ID")
	}
}
