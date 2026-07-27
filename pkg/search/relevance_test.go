package search

import (
	"math"
	"slices"
	"strings"
	"testing"
)

type relevanceJudgment struct {
	name                    string
	query                   string
	primaryPath             string
	acceptableTopThreePaths []string
	hardNegativePaths       []string
	wantRank                int
	wantHardNegativeHits    int
	wantSnippetContains     string
	wantSnippetEmpty        bool
}

type relevanceMetrics struct {
	topOneAccuracy     float64
	topThreeRecall     float64
	meanReciprocalRank float64
}

func TestDeterministicRelevance(t *testing.T) {
	corpus := relevanceCorpus()
	judgments := relevanceJudgments()
	validateRelevanceJudgments(t, corpus, judgments)

	var topOne, topThree int
	var reciprocalRank float64
	for _, judgment := range judgments {
		t.Run(judgment.name, func(t *testing.T) {
			results := Score(judgment.query, corpus)
			rank := scoredPathRank(results, judgment.primaryPath)
			if rank != judgment.wantRank {
				t.Fatalf("%q primary rank = %d, want %d; results=%#v", judgment.query, rank, judgment.wantRank, results)
			}

			hardNegativeHits := 0
			for _, path := range judgment.hardNegativePaths {
				if scoredPathRank(results, path) > 0 {
					hardNegativeHits++
				}
			}
			if hardNegativeHits != judgment.wantHardNegativeHits {
				t.Fatalf("%q hard-negative hits = %d, want %d; results=%#v", judgment.query, hardNegativeHits, judgment.wantHardNegativeHits, results)
			}

			if judgment.wantSnippetContains != "" {
				result := scoredPath(results, judgment.primaryPath)
				if result == nil || !strings.Contains(strings.ToLower(result.Snippet), strings.ToLower(judgment.wantSnippetContains)) {
					t.Fatalf("%q primary snippet = %q, want marker %q", judgment.query, resultSnippet(result), judgment.wantSnippetContains)
				}
			}
			if judgment.wantSnippetEmpty {
				result := scoredPath(results, judgment.primaryPath)
				if result == nil || result.Snippet != "" {
					t.Fatalf("%q primary snippet = %q, want empty metadata-only snippet", judgment.query, resultSnippet(result))
				}
			}

			if rank == 1 {
				topOne++
			}
			if rank > 0 && rank <= 3 {
				topThree++
			}
			if rank > 0 {
				reciprocalRank += 1 / float64(rank)
			}
		})
	}

	got := relevanceMetrics{
		topOneAccuracy:     float64(topOne) / float64(len(judgments)),
		topThreeRecall:     float64(topThree) / float64(len(judgments)),
		meanReciprocalRank: reciprocalRank / float64(len(judgments)),
	}
	want := relevanceMetrics{
		topOneAccuracy:     14.0 / 14.0,
		topThreeRecall:     14.0 / 14.0,
		meanReciprocalRank: 14.0 / 14.0,
	}
	if !relevanceMetricsEqual(got, want) {
		t.Fatalf("relevance metrics = %#v, want %#v", got, want)
	}
	t.Logf("relevance: top-one %.4f, top-three %.4f, MRR %.4f", got.topOneAccuracy, got.topThreeRecall, got.meanReciprocalRank)
}

func relevanceCorpus() []Fields {
	return []Fields{
		{
			Path:      "/workflows/project-alignment",
			ConceptID: "workflows/project-alignment",
			Title:     "Project Alignment",
			Body:      "# Project Alignment\n\nAlign an existing repository with shared engineering practices.\n",
		},
		{Path: "/noise/a", Title: "Noise A", Body: strings.Repeat("an ", 30)},
		{Path: "/noise/b", Title: "Noise B", Body: strings.Repeat("an ", 30)},
		{Path: "/noise/c", Title: "Noise C", Body: strings.Repeat("an ", 30)},
		{
			Path:  "/practices/boundary-contracts",
			Title: "Boundary Contracts",
			Body:  "How do contract releases work? How do boundaries evolve?",
		},
		{
			Path:  "/practices/project-releases",
			Title: "Project Releases",
			Tags:  []string{"releases"},
			Body:  "# Project Releases\n\nRoll back a release by restoring the prior artifact.",
		},
		{Path: "/animals/cat", Title: "Cat", Body: "# Cat\n\nA cat rests here."},
		{Path: "/false/application", Title: "False One", Body: "application"},
		{Path: "/false/publication", Title: "False Two", Body: "publication"},
		{Path: "/false/education", Title: "False Three", Body: "education"},
		{Path: "/false/concatenate", Title: "False Four", Body: "concatenate"},
		{
			Path:      "/identifiers",
			ConceptID: "/docs/file.md",
			Resource:  "factile:test/path",
			Body:      "snake_case kebab-case key:value",
		},
		{Path: "/coverage/complete", Title: "Complete Evidence", Body: "quartz beacon ember"},
		{Path: "/coverage/repeated", Title: "Repeated Evidence", Body: strings.Repeat("quartz ", 12)},
		{Path: "/authority/title", Title: "Cobalt Safety"},
		{Path: "/authority/tags", Title: "Tagged Evidence", Tags: []string{"cobalt", "safety"}},
		{
			Path:  "/passage/coherent",
			Title: "Coherent Passage",
			Body: "# Passage Notes\n\nLumen appears alone in an early note.\n\n" +
				strings.Repeat("Background material without the search terms. ", 20) +
				"\n\n## Recovery\n\nUse the lumen harbor procedure for the complete recovery.\n",
		},
		{
			Path:  "/passage/separated",
			Title: "Separated Evidence",
			Body:  "## First\n\n" + strings.Repeat("lumen ", 8) + "\n\n## Second\n\n" + strings.Repeat("harbor ", 8),
		},
		{Path: "/metadata/only", Description: "Violet Atlas"},
		{Path: "/tie/a", Resource: "tie-marker"},
		{Path: "/tie/b", Resource: "tie-marker"},
	}
}

func relevanceJudgments() []relevanceJudgment {
	return []relevanceJudgment{
		{
			name:                    "full project alignment question",
			query:                   "how should an existing repository align with shared engineering practices?",
			primaryPath:             "/workflows/project-alignment",
			acceptableTopThreePaths: []string{"/workflows/project-alignment"},
			wantRank:                1,
		},
		{
			name:                    "stripped project alignment query",
			query:                   "existing repository align shared engineering practices",
			primaryPath:             "/workflows/project-alignment",
			acceptableTopThreePaths: []string{"/workflows/project-alignment"},
			wantRank:                1,
		},
		{
			name:                    "release question",
			query:                   "how do releases roll back?",
			primaryPath:             "/practices/project-releases",
			acceptableTopThreePaths: []string{"/practices/project-releases"},
			wantRank:                1,
		},
		{
			name:                    "substring hard negatives",
			query:                   "cat",
			primaryPath:             "/animals/cat",
			acceptableTopThreePaths: []string{"/animals/cat"},
			hardNegativePaths:       []string{"/false/application", "/false/publication", "/false/education", "/false/concatenate"},
			wantRank:                1,
			wantHardNegativeHits:    0,
		},
		{
			name:                    "resource identifier",
			query:                   "factile:test/path",
			primaryPath:             "/identifiers",
			acceptableTopThreePaths: []string{"/identifiers"},
			wantRank:                1,
		},
		{
			name:                    "path identifier",
			query:                   "/docs/file.md",
			primaryPath:             "/identifiers",
			acceptableTopThreePaths: []string{"/identifiers"},
			wantRank:                1,
		},
		{
			name:                    "snake case identifier",
			query:                   "snake_case",
			primaryPath:             "/identifiers",
			acceptableTopThreePaths: []string{"/identifiers"},
			wantRank:                1,
		},
		{
			name:                    "kebab case identifier",
			query:                   "kebab-case",
			primaryPath:             "/identifiers",
			acceptableTopThreePaths: []string{"/identifiers"},
			wantRank:                1,
		},
		{
			name:                    "key value identifier",
			query:                   "key:value",
			primaryPath:             "/identifiers",
			acceptableTopThreePaths: []string{"/identifiers"},
			wantRank:                1,
		},
		{
			name:                    "full coverage versus repetition",
			query:                   "quartz beacon ember",
			primaryPath:             "/coverage/complete",
			acceptableTopThreePaths: []string{"/coverage/complete"},
			wantRank:                1,
		},
		{
			name:                    "title phrase and tag authority",
			query:                   "cobalt safety",
			primaryPath:             "/authority/title",
			acceptableTopThreePaths: []string{"/authority/title", "/authority/tags"},
			wantRank:                1,
		},
		{
			name:                    "coherent passage",
			query:                   "lumen harbor",
			primaryPath:             "/passage/coherent",
			acceptableTopThreePaths: []string{"/passage/coherent"},
			wantRank:                1,
			wantSnippetContains:     "lumen harbor",
		},
		{
			name:                    "metadata only",
			query:                   "violet atlas",
			primaryPath:             "/metadata/only",
			acceptableTopThreePaths: []string{"/metadata/only"},
			wantRank:                1,
			wantSnippetEmpty:        true,
		},
		{
			name:                    "deterministic tie",
			query:                   "tie-marker",
			primaryPath:             "/tie/a",
			acceptableTopThreePaths: []string{"/tie/a", "/tie/b"},
			wantRank:                1,
		},
	}
}

func validateRelevanceJudgments(t *testing.T, corpus []Fields, judgments []relevanceJudgment) {
	t.Helper()
	paths := make(map[string]bool, len(corpus))
	for _, field := range corpus {
		if field.Path == "" || paths[field.Path] {
			t.Fatalf("relevance corpus contains an empty or duplicate path %q", field.Path)
		}
		paths[field.Path] = true
	}
	names := make(map[string]bool, len(judgments))
	for _, judgment := range judgments {
		if judgment.name == "" || names[judgment.name] {
			t.Fatalf("relevance judgments contain an empty or duplicate name %q", judgment.name)
		}
		names[judgment.name] = true
		if judgment.query == "" || !paths[judgment.primaryPath] || judgment.wantRank < 1 {
			t.Fatalf("invalid relevance judgment %#v", judgment)
		}
		if !slices.Contains(judgment.acceptableTopThreePaths, judgment.primaryPath) {
			t.Fatalf("%q does not include primary path %q among acceptable top-three paths", judgment.name, judgment.primaryPath)
		}
		for _, path := range append(slices.Clone(judgment.acceptableTopThreePaths), judgment.hardNegativePaths...) {
			if !paths[path] {
				t.Fatalf("%q refers to missing corpus path %q", judgment.name, path)
			}
		}
	}
}

func scoredPathRank(results []Scored, path string) int {
	for i, result := range results {
		if result.Path == path {
			return i + 1
		}
	}
	return 0
}

func scoredPath(results []Scored, path string) *Scored {
	for i := range results {
		if results[i].Path == path {
			return &results[i]
		}
	}
	return nil
}

func resultSnippet(result *Scored) string {
	if result == nil {
		return ""
	}
	return result.Snippet
}

func relevanceMetricsEqual(a, b relevanceMetrics) bool {
	const tolerance = 1e-12
	return math.Abs(a.topOneAccuracy-b.topOneAccuracy) < tolerance &&
		math.Abs(a.topThreeRecall-b.topThreeRecall) < tolerance &&
		math.Abs(a.meanReciprocalRank-b.meanReciprocalRank) < tolerance
}
