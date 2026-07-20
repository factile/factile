package storage

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/factile/factile/pkg/okf"
)

var ErrUnsafePath = errors.New("unsafe source path")

// ErrLockTimeout reports that a local lock could not be acquired before the deadline.
var ErrLockTimeout = errors.New("lock timeout")

var (
	fileLockTimeout       = 5 * time.Second
	fileLockRetryInterval = 20 * time.Millisecond
)

type Local struct {
	Root string
}

type ScaffoldFile struct {
	Name string
	Data []byte
}

func NewLocal(root string) (Local, error) {
	if root == "" {
		return Local{}, fmt.Errorf("%w: empty root", ErrUnsafePath)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Local{}, err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return Local{}, err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return Local{}, err
	}
	if !info.IsDir() {
		return Local{}, fmt.Errorf("%w: root is not a directory", ErrUnsafePath)
	}
	return Local{Root: filepath.Clean(canonical)}, nil
}

func (s Local) ConceptFile(conceptID string) (string, error) {
	rel, err := okf.RelFromConceptID(conceptID)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnsafePath, err)
	}
	return s.safeJoin(rel)
}

func (s Local) safeJoin(rel string) (string, error) {
	rel = filepath.Clean(filepath.FromSlash(rel))
	if rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", fmt.Errorf("%w: %s", ErrUnsafePath, rel)
	}
	conceptPath := strings.TrimSuffix(filepath.ToSlash(rel), ".md")
	for _, part := range strings.Split(conceptPath, "/") {
		if strings.EqualFold(part, ".factile") || strings.EqualFold(part, ".git") {
			return "", fmt.Errorf("%w: internal path %s", ErrUnsafePath, rel)
		}
	}
	target := filepath.Join(s.Root, rel)
	cleanTarget := filepath.Clean(target)
	if cleanTarget != s.Root && !strings.HasPrefix(cleanTarget, s.Root+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %s", ErrUnsafePath, rel)
	}
	if err := rejectSymlinkComponents(s.Root, cleanTarget); err != nil {
		return "", err
	}
	return cleanTarget, nil
}

func rejectSymlinkComponents(root string, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink path component %s", ErrUnsafePath, current)
		}
	}
	return nil
}

func (s Local) ReadConcept(conceptID string) ([]byte, string, error) {
	file, err := s.ConceptFile(conceptID)
	if err != nil {
		return nil, "", err
	}
	if err := rejectSymlink(file); err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(file)
	return data, file, err
}

func (s Local) ListConceptIDs(prefix string) ([]string, error) {
	prefix = okf.NormalizeConceptID(prefix)
	root := s.Root
	if prefix != "" {
		var err error
		root, err = s.safeJoin(prefix)
		if err != nil {
			return nil, err
		}
		if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
			if statErr != nil {
				return nil, statErr
			}
			return nil, fmt.Errorf("%s is not a directory", prefix)
		}
	}
	var ids []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if p != root && (strings.EqualFold(name, ".factile") || strings.EqualFold(name, ".git")) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") || okf.IsReservedFile(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(s.Root, p)
		if err != nil {
			return err
		}
		id, ok := okf.ConceptIDFromRel(filepath.ToSlash(rel))
		if ok {
			ids = append(ids, id)
		}
		return nil
	})
	sort.Strings(ids)
	return ids, err
}

func (s Local) AtomicReplace(conceptID string, data []byte) error {
	file, err := s.ConceptFile(conceptID)
	if err != nil {
		return err
	}
	if err := rejectSymlink(file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), "."+filepath.Base(file)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, file)
}

func (s Local) CreateExclusive(conceptID string, data []byte) error {
	file, err := s.ConceptFile(conceptID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (s Local) CreateDirectoryScaffold(rel string, files []ScaffoldFile) error {
	dir, err := s.safeJoin(rel)
	if err != nil {
		return err
	}
	fullFiles := make([]string, 0, len(files))
	for _, file := range files {
		if file.Name == "" || file.Name != filepath.Base(file.Name) || strings.ContainsAny(file.Name, `/\`) {
			return fmt.Errorf("%w: %s", ErrUnsafePath, file.Name)
		}
		fullFiles = append(fullFiles, filepath.Join(dir, file.Name))
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		return err
	}
	created := make([]string, 0, len(fullFiles))
	cleanup := func() {
		for i := len(created) - 1; i >= 0; i-- {
			_ = os.Remove(created[i])
		}
		_ = os.Remove(dir)
	}
	for i, file := range files {
		f, err := os.OpenFile(fullFiles[i], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			cleanup()
			return err
		}
		created = append(created, fullFiles[i])
		if _, err := f.Write(file.Data); err != nil {
			f.Close()
			cleanup()
			return err
		}
		if err := f.Sync(); err != nil {
			f.Close()
			cleanup()
			return err
		}
		if err := f.Close(); err != nil {
			cleanup()
			return err
		}
	}
	return nil
}

func (s Local) DeleteConcept(conceptID string) error {
	file, err := s.ConceptFile(conceptID)
	if err != nil {
		return err
	}
	return os.Remove(file)
}

func (s Local) RenameConcept(oldID, newID string) error {
	oldFile, err := s.ConceptFile(oldID)
	if err != nil {
		return err
	}
	newFile, err := s.ConceptFile(newID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(newFile), 0o755); err != nil {
		return err
	}
	if err := rejectSymlink(oldFile); err != nil {
		return err
	}
	in, err := os.Open(oldFile)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(newFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	removeNew := true
	defer func() {
		if removeNew {
			_ = os.Remove(newFile)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Remove(oldFile); err != nil {
		return err
	}
	removeNew = false
	return nil
}

func WithFileLock(target string, fn func() error) error {
	return WithFileLocks([]string{target}, fn)
}

func WithFileLocks(targets []string, fn func() error) error {
	if len(targets) == 0 {
		return fn()
	}
	locks := make([]string, 0, len(targets))
	seen := map[string]bool{}
	for _, target := range targets {
		lockPath := target + ".lock"
		if !seen[lockPath] {
			locks = append(locks, lockPath)
			seen[lockPath] = true
		}
	}
	sort.Strings(locks)
	var held []string
	defer func() {
		for i := len(held) - 1; i >= 0; i-- {
			_ = os.Remove(held[i])
		}
	}()
	deadline := time.Now().Add(fileLockTimeout)
	for _, lockPath := range locks {
		if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
			return err
		}
		for {
			f, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err == nil {
				_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
				_ = f.Close()
				held = append(held, lockPath)
				break
			}
			if !errors.Is(err, os.ErrExist) {
				return err
			}
			if time.Now().After(deadline) {
				return lockTimeoutError(lockPath, err)
			}
			time.Sleep(fileLockRetryInterval)
		}
	}
	return fn()
}

func lockTimeoutError(lockPath string, cause error) error {
	holder := lockHolderDescription(lockPath)
	return fmt.Errorf(
		"%w: %s is still locked after %s (%s); Factile does not remove stale lock files automatically; remove %s only after confirming no Factile process is running: %w",
		ErrLockTimeout,
		lockPath,
		fileLockTimeout,
		holder,
		lockPath,
		cause,
	)
}

func lockHolderDescription(lockPath string) string {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return "holder unknown; could not read lock file"
	}
	line := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
	if line == "" {
		return "holder pid unknown"
	}
	return "holder pid " + line
}

func rejectSymlink(file string) error {
	info, err := os.Lstat(file)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: symlink concept file %s", ErrUnsafePath, file)
	}
	return nil
}
