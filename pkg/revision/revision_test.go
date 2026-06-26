package revision

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDigestBytes(t *testing.T) {
	got := DigestBytes([]byte("hello"))
	want := "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Fatalf("DigestBytes = %s, want %s", got, want)
	}
}

func TestDigestFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "doc.md")
	data := []byte("file contents")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := DigestFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if want := DigestBytes(data); got != want {
		t.Fatalf("DigestFile = %s, want %s", got, want)
	}
	if _, err := DigestFile(filepath.Join(t.TempDir(), "missing.md")); err == nil {
		t.Fatal("expected missing file error")
	}
}
