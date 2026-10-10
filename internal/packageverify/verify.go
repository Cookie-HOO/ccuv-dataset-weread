// Package packageverify validates the exact ccuv dataset archive layout.
package packageverify

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

const MaxArchiveBytes = 16 << 20
const MaxMemberBytes = 8 << 20

type manifest struct {
	Schema     string `json:"schema"`
	Protocol   string `json:"protocol"`
	Entrypoint string `json:"entrypoint"`
	Platform   struct {
		OS   string `json:"os"`
		Arch string `json:"arch"`
	} `json:"platform"`
	Probe struct {
		DatasetID  string          `json:"dataset_id"`
		Descriptor json.RawMessage `json:"descriptor"`
	} `json:"probe"`
}

// Verify requires exactly manifest.json and the declared executable, each regular.
// A distributable WeRead archive must carry the current V2 static Probe metadata.
func Verify(name string) error {
	members, err := readArchive(name)
	if err != nil {
		return err
	}
	parsed, err := parseManifest(members["manifest.json"])
	if err != nil {
		return err
	}
	if len(members) != 2 {
		return fmt.Errorf("archive must contain exactly two files")
	}
	if !validEntrypoint(parsed.Entrypoint) {
		return fmt.Errorf("invalid manifest entrypoint")
	}
	binary, ok := members[parsed.Entrypoint]
	if !ok || len(binary) == 0 {
		return fmt.Errorf("missing declared executable")
	}
	return nil
}

func readArchive(name string) (map[string][]byte, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	limited := io.LimitReader(file, MaxArchiveBytes+1)
	gzipReader, err := gzip.NewReader(limited)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	members := map[string][]byte{}
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > MaxMemberBytes || !safeMemberName(header.Name) {
			return nil, fmt.Errorf("invalid archive member")
		}
		if _, exists := members[header.Name]; exists {
			return nil, fmt.Errorf("duplicate archive member")
		}
		contents, err := io.ReadAll(io.LimitReader(tarReader, header.Size+1))
		if err != nil || int64(len(contents)) != header.Size {
			return nil, fmt.Errorf("invalid archive member content")
		}
		members[header.Name] = contents
	}
	if _, ok := members["manifest.json"]; !ok {
		return nil, fmt.Errorf("missing manifest.json")
	}
	return members, nil
}

func parseManifest(contents []byte) (manifest, error) {
	var value manifest
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil || decoder.More() {
		return manifest{}, fmt.Errorf("invalid V2 manifest")
	}
	if value.Schema != "ccuv.dataset/v1" || value.Protocol != "ccuv.custom/v2" || value.Entrypoint == "" || value.Platform.OS == "" || value.Platform.Arch == "" || value.Probe.DatasetID != "weread" {
		return manifest{}, fmt.Errorf("invalid V2 manifest")
	}
	var descriptor struct {
		Revision        string `json:"revision"`
		Version         string `json:"version"`
		DefaultMetricID string `json:"default_metric_id"`
		Dataset         struct {
			Title       map[string]string `json:"title"`
			Description map[string]string `json:"description"`
		} `json:"dataset"`
		Metrics     []json.RawMessage `json:"metrics"`
		Environment []json.RawMessage `json:"environment"`
	}
	if err := json.Unmarshal(value.Probe.Descriptor, &descriptor); err != nil || descriptor.Revision == "" || descriptor.Version == "" || descriptor.DefaultMetricID == "" || len(descriptor.Metrics) == 0 || !localized(descriptor.Dataset.Title) || !localized(descriptor.Dataset.Description) {
		return manifest{}, fmt.Errorf("invalid V2 manifest descriptor")
	}
	return value, nil
}

func localized(value map[string]string) bool { return value["en"] != "" && value["zh-CN"] != "" }

func safeMemberName(name string) bool {
	if name == "" || strings.ContainsRune(name, '\x00') || strings.HasPrefix(name, "/") || path.Clean(name) != name {
		return false
	}
	return !strings.HasPrefix(name, "../") && name != ".."
}

func validEntrypoint(name string) bool {
	return strings.HasPrefix(name, "bin/") && strings.Count(name, "/") == 1 && len(strings.TrimPrefix(name, "bin/")) > 0 && safeMemberName(name)
}

func SHA256(name string) (string, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}
