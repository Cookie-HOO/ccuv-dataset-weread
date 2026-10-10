// ccuv-dataset-weread-verify validates a release archive before distribution.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ccusage-viz/ccuv-dataset-weread/internal/packageverify"
)

const probeRequest = `{"protocol":"ccuv.custom/v2","request_id":"package-probe","request_kind":"probe"}`

type manifest struct {
	Entrypoint string `json:"entrypoint"`
	Probe      struct {
		DatasetID  string          `json:"dataset_id"`
		Descriptor json.RawMessage `json:"descriptor"`
	} `json:"probe"`
}

func main() {
	probe := flag.Bool("probe", false, "execute the packaged V2 Probe")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: ccuv-dataset-weread-verify [--probe] ARCHIVE")
		os.Exit(2)
	}
	archive := flag.Arg(0)
	var err error
	if *probe {
		err = acceptProbe(archive)
	} else {
		err = packageverify.Verify(archive)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid archive:", err)
		os.Exit(1)
	}
	if !*probe {
		digest, err := packageverify.SHA256(archive)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(digest)
	}
}

func acceptProbe(archive string) error {
	if err := packageverify.Verify(archive); err != nil {
		return err
	}
	contents, err := extractArchive(archive)
	if err != nil {
		return err
	}
	var static manifest
	if err := json.Unmarshal(contents["manifest.json"], &static); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	directory, err := os.MkdirTemp("", "ccuv-dataset-weread-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	for name, data := range contents {
		destination := filepath.Join(directory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if name == static.Entrypoint {
			mode = 0755
		}
		if err := os.WriteFile(destination, data, mode); err != nil {
			return err
		}
	}
	command := exec.Command(filepath.Join(directory, filepath.FromSlash(static.Entrypoint)), "ccuv")
	command.Stdin = bytes.NewBufferString(probeRequest + "\n")
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("run packaged Probe: %w", err)
	}
	var response struct {
		Protocol    string          `json:"protocol"`
		RequestID   string          `json:"request_id"`
		RequestKind string          `json:"request_kind"`
		DatasetID   string          `json:"dataset_id"`
		Status      string          `json:"status"`
		Descriptor  json.RawMessage `json:"descriptor"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return fmt.Errorf("parse packaged Probe: %w", err)
	}
	if response.Protocol != "ccuv.custom/v2" || response.RequestID != "package-probe" || response.RequestKind != "probe" || response.DatasetID != static.Probe.DatasetID || response.Status != "ok" || !jsonEqual(response.Descriptor, static.Probe.Descriptor) {
		return fmt.Errorf("packaged Probe does not match static manifest")
	}
	return nil
}

func extractArchive(name string) (map[string][]byte, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	contents := map[string][]byte{}
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return contents, nil
		}
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(tarReader)
		if err != nil {
			return nil, err
		}
		contents[header.Name] = data
	}
}

func jsonEqual(left, right []byte) bool {
	var leftValue, rightValue any
	return json.Unmarshal(left, &leftValue) == nil && json.Unmarshal(right, &rightValue) == nil && fmt.Sprintf("%#v", leftValue) == fmt.Sprintf("%#v", rightValue)
}
