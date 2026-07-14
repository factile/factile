package gitsource

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/factile/factile/pkg/storage"
	"github.com/factile/factile/pkg/vfs"
)

var ErrInvalidCache = errors.New("invalid Git cache")

const cacheStateVersion = 1

type Cache struct {
	Root   string
	base   string
	runner Runner
	now    func() time.Time
}

type Entry struct {
	Key            string
	MountPath      string
	Source         string
	Remote         string
	Dir            string
	RepositoryPath string
	SnapshotsPath  string
	StatePath      string
	UpdateTarget   string
}

type State struct {
	Version           int    `json:"version"`
	Key               string `json:"key"`
	MountPath         string `json:"mount_path"`
	Source            string `json:"source"`
	Initialized       bool   `json:"initialized"`
	SelectorMode      string `json:"selector_mode,omitempty"`
	Ref               string `json:"ref,omitempty"`
	RequestedRevision string `json:"requested_revision,omitempty"`
	ResolvedRevision  string `json:"resolved_revision,omitempty"`
	SelectedSnapshot  string `json:"selected_snapshot,omitempty"`
	SelectedMode      string `json:"selected_mode,omitempty"`
	SelectedRef       string `json:"selected_ref,omitempty"`
	SelectedRequest   string `json:"selected_request,omitempty"`
	LastAttemptAt     string `json:"last_attempt_at,omitempty"`
	LastSuccessAt     string `json:"last_success_at,omitempty"`
	LastErrorCode     string `json:"last_error_code,omitempty"`
}

type RepositoryInfo struct {
	Initialized bool   `json:"initialized"`
	Remote      string `json:"remote,omitempty"`
}

func OpenCache(opts vfs.LoadOptions, runner Runner) (*Cache, error) {
	return openCache(opts, runner, true)
}

// OpenCacheWithClock opens an initialized cache with an injected refresh clock.
func OpenCacheWithClock(opts vfs.LoadOptions, runner Runner, now func() time.Time) (*Cache, error) {
	cache, err := openCache(opts, runner, true)
	if err != nil {
		return nil, err
	}
	if now != nil {
		cache.now = now
	}
	return cache, nil
}

// OpenCacheForStatus opens the cache layout without creating or modifying it.
func OpenCacheForStatus(opts vfs.LoadOptions, runner Runner) (*Cache, error) {
	return openCache(opts, runner, false)
}

func openCache(opts vfs.LoadOptions, runner Runner, initialize bool) (*Cache, error) {
	root, err := vfs.RequireRoot(opts)
	if err != nil {
		return nil, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root = filepath.Clean(root)
	if err := requireSafeDirectory(root); err != nil {
		return nil, err
	}
	factileDir := filepath.Join(root, ".factile")
	if err := requireSafeDirectory(factileDir); err != nil {
		return nil, err
	}
	cacheDir := filepath.Join(factileDir, "cache")
	base := filepath.Join(cacheDir, "git")
	if initialize {
		if err := ensurePrivateDirectory(cacheDir); err != nil {
			return nil, err
		}
		if err := atomicWriteFile(filepath.Join(cacheDir, ".gitignore"), []byte("*\n"), 0o600); err != nil {
			return nil, err
		}
		if err := ensurePrivateDirectory(base); err != nil {
			return nil, err
		}
	} else {
		for _, dir := range []string{cacheDir, base} {
			if err := requireSafeDirectory(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
	}
	return &Cache{Root: root, base: base, runner: runner, now: time.Now}, nil
}

func (c *Cache) Entry(mountPath, source string) (Entry, error) {
	entry, err := c.entryPaths(mountPath, source)
	if err != nil {
		return Entry{}, err
	}
	if err := c.validateManagedDirectories(); err != nil {
		return Entry{}, err
	}
	if err := ensurePrivateDirectory(entry.Dir); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func (c *Cache) entryPaths(mountPath, source string) (Entry, error) {
	normalized, err := vfs.ValidateMountPath(mountPath)
	if err != nil {
		return Entry{}, err
	}
	classification, err := vfs.ClassifySource(source)
	if err != nil || classification.Kind != vfs.SourceKindGit {
		return Entry{}, fmt.Errorf("%w: source is not a valid Git remote", ErrInvalidCache)
	}
	if err := ValidateSource(source); err != nil {
		return Entry{}, err
	}
	digest := sha256.Sum256([]byte(normalized + "\x00" + source))
	key := hex.EncodeToString(digest[:])
	dir := filepath.Join(c.base, key)
	if err := validateManagedPath(c.base, dir); err != nil {
		return Entry{}, err
	}
	return Entry{
		Key:            key,
		MountPath:      normalized,
		Source:         source,
		Remote:         classification.GitRemote,
		Dir:            dir,
		RepositoryPath: filepath.Join(dir, "repository.git"),
		SnapshotsPath:  filepath.Join(dir, "snapshots"),
		StatePath:      filepath.Join(dir, "state.json"),
		UpdateTarget:   filepath.Join(dir, "update"),
	}, nil
}

func (c *Cache) WithUpdateLock(entry Entry, fn func() error) error {
	if err := c.validateEntry(entry); err != nil {
		return err
	}
	return storage.WithFileLock(entry.UpdateTarget, fn)
}

func (c *Cache) InitializeRepository(ctx context.Context, entry Entry) error {
	if err := c.validateEntry(entry); err != nil {
		return err
	}
	if info, err := c.InspectRepository(ctx, entry); err == nil && info.Initialized {
		if state, stateErr := c.ReadState(entry); stateErr == nil && state.Initialized {
			return nil
		}
	} else if err != nil {
		return err
	}
	return c.WithUpdateLock(entry, func() error {
		info, err := c.InspectRepository(ctx, entry)
		if err != nil {
			return err
		}
		if !info.Initialized {
			temporary, err := os.MkdirTemp(entry.Dir, ".repository-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(temporary)
			if _, err := c.runner.Run(ctx, "", "init", "--bare", "--", temporary); err != nil {
				return err
			}
			if _, err := c.runner.Run(ctx, "", "-C", temporary, "config", "--local", "--add", "--", "remote.origin.url", entry.Remote); err != nil {
				return err
			}
			if err := os.Rename(temporary, entry.RepositoryPath); err != nil {
				return err
			}
		}

		state, err := c.ReadState(entry)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, ErrInvalidCache) {
				return err
			}
			state = initialState(entry)
			state.Initialized = true
			return c.WriteState(entry, state)
		}
		if !state.Initialized {
			state.Initialized = true
			return c.WriteState(entry, state)
		}
		return nil
	})
}

func initialState(entry Entry) State {
	return State{
		Version:   cacheStateVersion,
		Key:       entry.Key,
		MountPath: entry.MountPath,
		Source:    entry.Source,
	}
}

func (c *Cache) nowUTC() time.Time {
	if c.now == nil {
		return time.Now().UTC()
	}
	return c.now().UTC()
}

func (c *Cache) InspectRepository(ctx context.Context, entry Entry) (RepositoryInfo, error) {
	if err := c.validateEntry(entry); err != nil {
		return RepositoryInfo{}, err
	}
	info, err := os.Lstat(entry.RepositoryPath)
	if errors.Is(err, os.ErrNotExist) {
		return RepositoryInfo{}, nil
	}
	if err != nil {
		return RepositoryInfo{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return RepositoryInfo{}, fmt.Errorf("%w: repository path is not a safe directory", ErrInvalidCache)
	}
	if err := requireSafeDirectory(filepath.Join(entry.RepositoryPath, "objects")); err != nil {
		return RepositoryInfo{}, fmt.Errorf("%w: repository object directory is not safe: %v", ErrInvalidCache, err)
	}
	bare, err := c.runner.Run(ctx, "", "-C", entry.RepositoryPath, "rev-parse", "--is-bare-repository")
	if err != nil || strings.TrimSpace(string(bare)) != "true" {
		if err != nil {
			return RepositoryInfo{}, err
		}
		return RepositoryInfo{}, fmt.Errorf("%w: repository is not bare", ErrInvalidCache)
	}
	remote, err := c.runner.Run(ctx, "", "-C", entry.RepositoryPath, "config", "--get", "--", "remote.origin.url")
	if err != nil {
		return RepositoryInfo{}, err
	}
	configured := strings.TrimSpace(string(remote))
	if configured != entry.Remote {
		return RepositoryInfo{}, fmt.Errorf("%w: repository source identity mismatch", ErrInvalidCache)
	}
	return RepositoryInfo{Initialized: true, Remote: configured}, nil
}

func (c *Cache) WriteState(entry Entry, state State) error {
	if err := c.validateEntry(entry); err != nil {
		return err
	}
	if state.Version != cacheStateVersion || state.Key != entry.Key || state.MountPath != entry.MountPath || state.Source != entry.Source {
		return fmt.Errorf("%w: cache state identity mismatch", ErrInvalidCache)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWriteFile(entry.StatePath, data, 0o600)
}

func (c *Cache) ReadState(entry Entry) (State, error) {
	if err := c.validateEntry(entry); err != nil {
		return State{}, err
	}
	if err := requireSafeRegularFile(entry.StatePath); err != nil {
		return State{}, err
	}
	data, err := os.ReadFile(entry.StatePath)
	if err != nil {
		return State{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var state State
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("%w: malformed cache state", ErrInvalidCache)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return State{}, fmt.Errorf("%w: malformed cache state", ErrInvalidCache)
	}
	if state.Version != cacheStateVersion || state.Key != entry.Key || state.MountPath != entry.MountPath || state.Source != entry.Source {
		return State{}, fmt.Errorf("%w: cache state identity mismatch", ErrInvalidCache)
	}
	return state, nil
}

func (c *Cache) validateEntry(entry Entry) error {
	expected, err := c.entryPaths(entry.MountPath, entry.Source)
	if err != nil {
		return err
	}
	if entry != expected {
		return fmt.Errorf("%w: cache entry identity mismatch", ErrInvalidCache)
	}
	if err := validateManagedPath(c.base, entry.Dir); err != nil {
		return err
	}
	if err := c.validateManagedDirectories(); err != nil {
		return err
	}
	return requireSafeDirectory(entry.Dir)
}

func (c *Cache) validateManagedDirectories() error {
	expectedBase := filepath.Join(c.Root, ".factile", "cache", "git")
	if filepath.Clean(c.base) != filepath.Clean(expectedBase) {
		return fmt.Errorf("%w: cache root identity mismatch", ErrInvalidCache)
	}
	for _, path := range []string{
		c.Root,
		filepath.Join(c.Root, ".factile"),
		filepath.Join(c.Root, ".factile", "cache"),
		c.base,
	} {
		if err := requireSafeDirectory(path); err != nil {
			return err
		}
	}
	return nil
}

// ValidateSource applies the security checks required before a Git source may
// be persisted, displayed, or passed to Git.
func ValidateSource(source string) error {
	classification, err := vfs.ClassifySource(source)
	if err != nil || classification.Kind != vfs.SourceKindGit {
		return fmt.Errorf("%w: source is not a valid Git remote", ErrInvalidCache)
	}
	return validateRemoteSecurity(classification.GitRemote)
}

func validateRemoteSecurity(remote string) error {
	if !strings.Contains(remote, "://") {
		return nil
	}
	parsed, err := url.Parse(remote)
	if err != nil || parsed.ForceQuery || parsed.RawQuery != "" || parsed.Fragment != "" || strings.Contains(remote, "#") {
		return fmt.Errorf("%w: Git remote contains unsupported sensitive URI components", ErrInvalidCache)
	}
	if parsed.User == nil {
		return nil
	}
	_, hasPassword := parsed.User.Password()
	if hasPassword || parsed.Scheme == "http" || parsed.Scheme == "https" || parsed.Scheme == "git" || parsed.Scheme == "file" {
		return fmt.Errorf("%w: Git remote contains unsupported credentials", ErrInvalidCache)
	}
	return nil
}

func validateManagedPath(base, target string) error {
	base = filepath.Clean(base)
	target = filepath.Clean(target)
	relative, err := filepath.Rel(base, target)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("%w: cache path escapes its managed root", ErrInvalidCache)
	}
	return nil
}

func requireSafeDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: cache component is not a safe directory", ErrInvalidCache)
	}
	return nil
}

func requireSafeRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: cache component is not a safe file", ErrInvalidCache)
	}
	return nil
}

func ensurePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: cache component is not a safe directory", ErrInvalidCache)
	}
	return os.Chmod(path, 0o700)
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: cache file is a symlink", ErrInvalidCache)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}
