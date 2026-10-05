// Package packageverify validates the exact ccuv dataset archive layout.
package packageverify

import (
	"archive/tar"
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

// Verify requires exactly manifest.json and the declared executable, each regular.
func Verify(name string) error {
	file, err := os.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	limited := io.LimitReader(file, MaxArchiveBytes+1)
	gzipReader, err := gzip.NewReader(limited)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
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
			return fmt.Errorf("read archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > MaxMemberBytes || !safeMemberName(header.Name) {
			return fmt.Errorf("invalid archive member")
		}
		if _, exists := members[header.Name]; exists {
			return fmt.Errorf("duplicate archive member")
		}
		contents, err := io.ReadAll(io.LimitReader(tarReader, header.Size+1))
		if err != nil || int64(len(contents)) != header.Size {
			return fmt.Errorf("invalid archive member content")
		}
		members[header.Name] = contents
	}
	manifestBytes, ok := members["manifest.json"]
	if !ok {
		return fmt.Errorf("missing manifest.json")
	}
	var manifest struct {
		Entrypoint string `json:"entrypoint"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil || manifest.Entrypoint == "" {
		return fmt.Errorf("invalid manifest")
	}
	if len(members) != 2 {
		return fmt.Errorf("archive must contain exactly two files")
	}
	if !validEntrypoint(manifest.Entrypoint) {
		return fmt.Errorf("invalid manifest entrypoint")
	}
	binary, ok := members[manifest.Entrypoint]
	if !ok || len(binary) == 0 {
		return fmt.Errorf("missing declared executable")
	}
	return nil
}

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
