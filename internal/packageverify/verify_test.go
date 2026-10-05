package packageverify

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

type archiveMember struct {
	name     string
	contents string
	mode     int64
	typeflag byte
}

func archive(t *testing.T, members []archiveMember) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "dataset.tar.gz")
	file, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, member := range members {
		header := &tar.Header{Name: member.name, Mode: member.mode, Size: int64(len(member.contents)), Typeflag: member.typeflag}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write([]byte(member.contents)); err != nil {
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

func validMembers() []archiveMember {
	return []archiveMember{
		{name: "manifest.json", contents: `{"entrypoint":"bin/ccuv-dataset-weread"}`, mode: 0644, typeflag: tar.TypeReg},
		{name: "bin/ccuv-dataset-weread", contents: "binary", mode: 0755, typeflag: tar.TypeReg},
	}
}

func TestVerifyValidArchive(t *testing.T) {
	if err := Verify(archive(t, validMembers())); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsUnexpectedMember(t *testing.T) {
	members := append(validMembers(), archiveMember{name: "extra", contents: "not allowed", mode: 0600, typeflag: tar.TypeReg})
	if err := Verify(archive(t, members)); err == nil {
		t.Fatal("accepted unexpected member")
	}
}

func TestVerifyRejectsUnsafeAndInvalidEntrypointPaths(t *testing.T) {
	for _, members := range [][]archiveMember{
		{{name: "manifest.json", contents: `{"entrypoint":"../binary"}`, mode: 0644, typeflag: tar.TypeReg}, {name: "../binary", contents: "binary", mode: 0755, typeflag: tar.TypeReg}},
		{{name: "manifest.json", contents: `{"entrypoint":"binary"}`, mode: 0644, typeflag: tar.TypeReg}, {name: "binary", contents: "binary", mode: 0755, typeflag: tar.TypeReg}},
	} {
		if err := Verify(archive(t, members)); err == nil {
			t.Fatal("accepted unsafe archive path")
		}
	}
}

func TestVerifyRejectsNonRegularMembers(t *testing.T) {
	members := validMembers()
	members[1].typeflag = tar.TypeSymlink
	members[1].contents = ""
	if err := Verify(archive(t, members)); err == nil {
		t.Fatal("accepted link member")
	}
}
