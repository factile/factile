package gitsource

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

var (
	ErrGitUnavailable = errors.New("git executable is unavailable")
	ErrGitTimeout     = errors.New("git command timed out")
	ErrGitCommand     = errors.New("git command failed")
)

const (
	defaultGitTimeout  = 2 * time.Minute
	maxGitOutputLength = 16 << 20
)

type commandFactory func(context.Context, string, ...string) *exec.Cmd

type Runner struct {
	GitPath string
	Timeout time.Duration
	command commandFactory
}

func NewRunner() Runner {
	return Runner{GitPath: "git", Timeout: defaultGitTimeout}
}

func (r Runner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return r.run(ctx, dir, "", args...)
}

func (r Runner) runWithManagedIndex(ctx context.Context, dir, indexPath string, args ...string) ([]byte, error) {
	return r.run(ctx, dir, indexPath, args...)
}

func (r Runner) run(ctx context.Context, dir, indexPath string, args ...string) ([]byte, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = defaultGitTimeout
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	gitPath := r.GitPath
	if gitPath == "" {
		gitPath = "git"
	}
	factory := r.command
	if factory == nil {
		factory = exec.CommandContext
	}
	commandArgs := append([]string{"-c", "submodule.recurse=false"}, args...)
	cmd := factory(commandCtx, gitPath, commandArgs...)
	cmd.Dir = dir
	cmd.Env = gitEnvironment(os.Environ(), indexPath)
	var output boundedBuffer
	output.limit = maxGitOutputLength
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	if output.truncated && err == nil {
		return nil, fmt.Errorf("%w: output exceeded the safe limit", ErrGitCommand)
	}
	if err == nil {
		return output.Bytes(), nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if commandCtx.Err() == context.DeadlineExceeded {
		return nil, ErrGitTimeout
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return nil, ErrGitUnavailable
	}
	// Git and credential helpers share stderr. Returning subprocess output could
	// expose helper-provided secrets that cannot be reliably recognized, so
	// failed commands expose only the stable error category.
	return nil, ErrGitCommand
}

var repositoryEnvironment = map[string]struct{}{
	"GIT_ALTERNATE_OBJECT_DIRECTORIES": {},
	"GIT_CEILING_DIRECTORIES":          {},
	"GIT_COMMON_DIR":                   {},
	"GIT_DEFAULT_HASH":                 {},
	"GIT_DEFAULT_REF_FORMAT":           {},
	"GIT_DIR":                          {},
	"GIT_DISCOVERY_ACROSS_FILESYSTEM":  {},
	"GIT_GRAFT_FILE":                   {},
	"GIT_IMPLICIT_WORK_TREE":           {},
	"GIT_INDEX_FILE":                   {},
	"GIT_INTERNAL_SUPER_PREFIX":        {},
	"GIT_NAMESPACE":                    {},
	"GIT_NO_REPLACE_OBJECTS":           {},
	"GIT_OBJECT_DIRECTORY":             {},
	"GIT_PREFIX":                       {},
	"GIT_QUARANTINE_PATH":              {},
	"GIT_REPLACE_REF_BASE":             {},
	"GIT_SHALLOW_FILE":                 {},
	"GIT_TEMPLATE_DIR":                 {},
	"GIT_WORK_TREE":                    {},
}

func gitEnvironment(current []string, indexPath string) []string {
	overrides := map[string]string{
		"GCM_INTERACTIVE":     "Never",
		"GIT_LFS_SKIP_SMUDGE": "1",
		"GIT_SSH_COMMAND":     "ssh -oBatchMode=yes",
		"GIT_SSH_VARIANT":     "ssh",
		"GIT_TERMINAL_PROMPT": "0",
	}
	environment := make([]string, 0, len(current)+len(overrides)+1)
	for _, value := range current {
		key := value
		if index := strings.IndexByte(value, '='); index >= 0 {
			key = value[:index]
		}
		if _, blocked := repositoryEnvironment[key]; blocked {
			continue
		}
		if _, replaced := overrides[key]; replaced {
			continue
		}
		environment = append(environment, value)
	}
	for key, value := range overrides {
		environment = append(environment, key+"="+value)
	}
	if indexPath != "" {
		environment = append(environment, "GIT_INDEX_FILE="+indexPath)
	}
	return environment
}

type boundedBuffer struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	written := len(data)
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		b.truncated = true
		return written, nil
	}
	if len(data) > remaining {
		data = data[:remaining]
		b.truncated = true
	}
	_, _ = b.Buffer.Write(data)
	return written, nil
}
