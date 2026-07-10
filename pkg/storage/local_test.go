package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestListAndReadRejectSymlinkConcept(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "bundle")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(tmp, "outside.md")
	if err := os.WriteFile(outside, []byte("---\ntype: Secret\n---\n\n# Outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "leak.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	store, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := store.ListConceptIDs("")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("symlink concept should be skipped, got %#v", ids)
	}
	if _, _, err := store.ReadConcept("leak"); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("expected unsafe symlink read, got %v", err)
	}
}

func TestListConceptIDsScansHiddenKnowledgeDirsButSkipsToolPrivateDirs(t *testing.T) {
	root := t.TempDir()
	mustWriteStorageTestFile(t, filepath.Join(root, ".well-known", "source.md"), []byte("---\ntype: Reference\n---\n\n# Source\n"))
	mustWriteStorageTestFile(t, filepath.Join(root, ".factile", "private.md"), []byte("# Tool metadata\n"))
	mustWriteStorageTestFile(t, filepath.Join(root, ".git", "ignored.md"), []byte("# Git internals\n"))

	store, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := store.ListConceptIDs("")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != ".well-known/source" {
		t.Fatalf("unexpected concept IDs: %#v", ids)
	}
}

func TestLocalWriteRenameAndDeletePrimitives(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := store.AtomicReplace("guides/setup", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	data, _, err := store.ReadConcept("guides/setup")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "v1" {
		t.Fatalf("AtomicReplace wrote %q, want v1", string(data))
	}

	if err := store.AtomicReplace("guides/setup", []byte("v2")); err != nil {
		t.Fatal(err)
	}
	data, _, err = store.ReadConcept("guides/setup")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "v2" {
		t.Fatalf("AtomicReplace overwrite wrote %q, want v2", string(data))
	}

	if err := store.CreateExclusive("guides/new", []byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateExclusive("guides/new", []byte("again")); !errors.Is(err, os.ErrExist) {
		t.Fatalf("CreateExclusive existing error = %v, want os.ErrExist", err)
	}

	if err := store.RenameConcept("guides/new", "archive/new"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReadConcept("guides/new"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old concept after rename error = %v, want not exist", err)
	}
	data, _, err = store.ReadConcept("archive/new")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("renamed concept data = %q, want new", string(data))
	}

	if err := store.DeleteConcept("archive/new"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReadConcept("archive/new"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted concept error = %v, want not exist", err)
	}
}

func TestLocalRejectsUnsafeConceptIDs(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConceptFile("../outside"); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("ConceptFile unsafe path error = %v, want ErrUnsafePath", err)
	}
	if err := store.AtomicReplace("../outside", []byte("nope")); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("AtomicReplace unsafe path error = %v, want ErrUnsafePath", err)
	}
}

func TestWithFileLockCreatesAndReleasesLock(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "doc.md")
	lockPath := target + ".lock"
	var lockData []byte

	if err := WithFileLock(target, func() error {
		var err error
		lockData, err = os.ReadFile(lockPath)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(lockData)) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("lock file should contain current pid, got %q", string(lockData))
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock file should be removed after release, got %v", err)
	}
}

func TestWithFileLockContentionWaitsForRelease(t *testing.T) {
	setFileLockTiming(t, time.Second, time.Millisecond)
	tmp := t.TempDir()
	target := filepath.Join(tmp, "doc.md")
	contenderAcquired := make(chan struct{})
	contenderDone := make(chan error, 1)

	err := WithFileLock(target, func() error {
		go func() {
			contenderDone <- WithFileLock(target, func() error {
				close(contenderAcquired)
				return nil
			})
		}()
		select {
		case <-contenderAcquired:
			return errors.New("contender acquired lock while first lock was held")
		case <-time.After(40 * time.Millisecond):
			return nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-contenderDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("contender did not acquire lock after release")
	}
	select {
	case <-contenderAcquired:
	default:
		t.Fatal("contender completed without acquiring lock")
	}
}

func TestWithFileLocksTimeoutLeavesStaleLockAndReleasesHeldLocks(t *testing.T) {
	setFileLockTiming(t, 30*time.Millisecond, time.Millisecond)
	tmp := t.TempDir()
	first := filepath.Join(tmp, "a.md")
	second := filepath.Join(tmp, "b.md")
	firstLock := first + ".lock"
	secondLock := second + ".lock"
	if err := os.WriteFile(secondLock, []byte("999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	called := false

	err := WithFileLocks([]string{first, second}, func() error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("expected lock timeout, got %v", err)
	}
	if called {
		t.Fatal("lock callback should not run after timeout")
	}
	if _, statErr := os.Stat(firstLock); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("held lock should be released after timeout, got %v", statErr)
	}
	if _, statErr := os.Stat(secondLock); statErr != nil {
		t.Fatalf("pre-existing stale lock should remain, got %v", statErr)
	}
	message := err.Error()
	for _, want := range []string{secondLock, "holder pid 999999", "does not remove stale lock files automatically"} {
		if !strings.Contains(message, want) {
			t.Fatalf("timeout error should contain %q, got %q", want, message)
		}
	}
}

func setFileLockTiming(t *testing.T, timeout time.Duration, retryInterval time.Duration) {
	t.Helper()
	oldTimeout := fileLockTimeout
	oldRetryInterval := fileLockRetryInterval
	fileLockTimeout = timeout
	fileLockRetryInterval = retryInterval
	t.Cleanup(func() {
		fileLockTimeout = oldTimeout
		fileLockRetryInterval = oldRetryInterval
	})
}

func mustWriteStorageTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
