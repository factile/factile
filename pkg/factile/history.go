package factile

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/factile/factile/pkg/gitsource"
	"github.com/factile/factile/pkg/patch"
	"github.com/factile/factile/pkg/storage"
)

type HistoryResult struct {
	Path      string          `json:"path"`
	Available bool            `json:"available"`
	Reason    string          `json:"reason,omitempty"`
	Commits   []HistoryCommit `json:"commits"`
	Compared  string          `json:"compared_revision,omitempty"`
	Diff      string          `json:"diff,omitempty"`
}

type HistoryCommit struct {
	Revision string `json:"revision"`
	Date     string `json:"date"`
	Subject  string `json:"subject"`
	filename string
}

func (w *LocalWorkspace) History(ctx context.Context, path, comparison string) (HistoryResult, error) {
	stat, err := w.Stat(ctx, path, StatOptions{})
	if err != nil {
		return HistoryResult{}, err
	}
	result := HistoryResult{Path: stat.Card.Path, Commits: []HistoryCommit{}}
	filename := stat.Card.SourcePath
	if filename == "" {
		result.Reason = "Git history requires a document in a local checkout."
		return result, nil
	}
	git := gitsource.NewRunner()
	git.Timeout = 15 * time.Second
	dir := filepath.Dir(filename)
	root, err := git.Run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		result.Reason = "No accessible Git checkout owns this document."
		if errors.Is(err, gitsource.ErrGitUnavailable) {
			result.Reason = "Git is unavailable."
		}
		return result, ctx.Err()
	}
	rootDir := strings.TrimRight(string(root), "\r\n")
	data, err := git.Run(ctx, rootDir, "--literal-pathspecs", "log", "--follow", "--name-only", "-z",
		"--format=%x00%H%x00%aI%x00%s", "--", filename)
	if err != nil {
		result.Reason = "Could not read the document's Git history."
		return result, ctx.Err()
	}
	if len(data) == 0 {
		result.Reason = "This document has no recorded Git commits."
		return result, nil
	}
	fields := bytes.Split(bytes.TrimSuffix(data, []byte{0}), []byte{0})
	if len(fields)%5 != 0 {
		result.Reason = "Git returned an unexpected document history format."
		return result, nil
	}
	for start := 0; start < len(fields); start += 5 {
		result.Commits = append(result.Commits, HistoryCommit{
			Revision: string(fields[start+1]), Date: string(fields[start+2]), Subject: string(fields[start+3]),
			filename: string(bytes.TrimPrefix(fields[start+4], []byte("\n"))),
		})
	}
	result.Available = true
	if comparison == "" {
		return result, nil
	}
	commit, err := git.Run(ctx, rootDir, "rev-parse", "--verify", "--end-of-options", comparison+"^{commit}")
	if err != nil {
		return result, errorf(ErrRevisionNotAvailable, "Cannot resolve Git commit %q", comparison)
	}
	selected := strings.TrimSpace(string(commit))
	relativeFilename, err := filepath.Rel(rootDir, filename)
	if err != nil {
		return result, NormalizeError(err)
	}
	historicalFilename := filepath.ToSlash(relativeFilename)
	for _, item := range result.Commits {
		if item.Revision == selected {
			historicalFilename = item.filename
			break
		}
	}
	before, err := git.Run(ctx, rootDir, "show", "--no-ext-diff", "--no-textconv", selected+":"+historicalFilename)
	if err != nil {
		return result, errorf(ErrRevisionNotAvailable, "Document %s is unavailable at commit %s", path, selected)
	}
	store, err := storage.NewLocal(dir)
	if err != nil {
		return result, NormalizeError(err)
	}
	after, _, err := store.ReadConcept(filepath.Base(filename))
	if err != nil {
		return result, NormalizeError(err)
	}
	result.Compared = selected
	result.Diff = patch.Diff(result.Path+".md", string(before), string(after))
	return result, nil
}
