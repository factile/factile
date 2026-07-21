package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFailurePreservesExistingDestinationAndCleansTemporary(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "factile.toml")
	if err := os.WriteFile(filename, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("rename failed")
	err := writeWithOperations(filename, []byte("complete new\n"), 0o644, operations{
		rename: func(string, string) error { return wantErr },
		link:   os.Link,
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	data, err := os.ReadFile(filename)
	if err != nil || string(data) != "old\n" {
		t.Fatalf("failed replacement changed destination: %q, %v", data, err)
	}
	assertNoTemporaryFiles(t, dir)
}

func TestCreateFailureLeavesDestinationAbsentAndCleansTemporary(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "overview.md")
	wantErr := errors.New("link failed")
	created, err := createWithOperations(filename, []byte("complete new\n"), 0o644, operations{
		rename: os.Rename,
		link:   func(string, string) error { return wantErr },
	})
	if created || !errors.Is(err, wantErr) {
		t.Fatalf("created=%v error=%v, want false and %v", created, err, wantErr)
	}
	if _, err := os.Stat(filename); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed creation published a destination: %v", err)
	}
	assertNoTemporaryFiles(t, dir)
}

func TestCreateDoesNotReplaceConcurrentDestination(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "index.md")
	created, err := createWithOperations(filename, []byte("generated\n"), 0o644, operations{
		rename: os.Rename,
		link: func(_, target string) error {
			if err := os.WriteFile(target, []byte("authored\n"), 0o600); err != nil {
				return err
			}
			return os.ErrExist
		},
	})
	if err != nil || created {
		t.Fatalf("created=%v error=%v, want clean lost race", created, err)
	}
	data, err := os.ReadFile(filename)
	if err != nil || string(data) != "authored\n" {
		t.Fatalf("create-only publication replaced authored data: %q, %v", data, err)
	}
	assertNoTemporaryFiles(t, dir)
}

func TestWriteAndCreatePublishCompleteFiles(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing")
	if err := os.WriteFile(existing, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(existing, []byte("complete replacement"), 0o640); err != nil {
		t.Fatal(err)
	}
	createdPath := filepath.Join(dir, "created")
	created, err := Create(createdPath, []byte("complete creation"), 0o640)
	if err != nil || !created {
		t.Fatalf("created=%v error=%v", created, err)
	}
	for filename, want := range map[string]string{existing: "complete replacement", createdPath: "complete creation"} {
		data, err := os.ReadFile(filename)
		if err != nil || string(data) != want {
			t.Fatalf("%s = %q, %v; want %q", filename, data, err, want)
		}
	}
	assertNoTemporaryFiles(t, dir)
}

func assertNoTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".*.factile-tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}
