// Package packagebuild writes deterministic native dataset archives.
package packagebuild

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const sourceDateEpoch = 0

// Write creates the exact two-member release archive required by ccuv.
func Write(output, manifestPath, executablePath, entrypoint string) error {
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	executable, err := os.ReadFile(executablePath)
	if err != nil {
		return fmt.Errorf("read executable: %w", err)
	}
	if len(executable) == 0 {
		return fmt.Errorf("executable is empty")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	file, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}
	defer file.Close()

	gzipWriter, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("create gzip writer: %w", err)
	}
	gzipWriter.Name = ""
	gzipWriter.Comment = ""
	gzipWriter.ModTime = time.Unix(sourceDateEpoch, 0).UTC()
	gzipWriter.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	stamp := time.Unix(sourceDateEpoch, 0).UTC()
	members := []struct {
		name string
		mode int64
		data []byte
	}{
		{name: "manifest.json", mode: 0644, data: manifest},
		{name: entrypoint, mode: 0755, data: executable},
	}
	for _, member := range members {
		header := &tar.Header{
			Name:     member.name,
			Mode:     member.mode,
			Size:     int64(len(member.data)),
			Typeflag: tar.TypeReg,
			ModTime:  stamp,
			Format:   tar.FormatUSTAR,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("write tar header: %w", err)
		}
		if _, err := tarWriter.Write(member.data); err != nil {
			return fmt.Errorf("write tar member: %w", err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		return fmt.Errorf("close tar archive: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return fmt.Errorf("close gzip archive: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close archive: %w", err)
	}
	return nil
}

// CopyExecutable copies a compiled binary into a staging location under the requested mode.
func CopyExecutable(destination, source string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
