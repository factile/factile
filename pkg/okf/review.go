package okf

import (
	"fmt"
	"strings"
)

type ReviewState struct {
	Verified           []map[string]any `json:"verified"`
	Tier               string           `json:"tier"`
	Status             string           `json:"status"`
	LastVerifiedAt     string           `json:"last_verified_at,omitempty"`
	ChangedSinceReview *bool            `json:"changed_since_review"`
	Stale              *bool            `json:"stale"`
	EvaluatedAt        string           `json:"evaluated_at"`
}

// CompareDatetimes retains sub-nanosecond precision as well as timezone semantics.
// Callers validate both inputs with Datetime first.
func CompareDatetimes(a, b string) int {
	left, _ := Datetime(a)
	right, _ := Datetime(b)
	if left.Unix() < right.Unix() {
		return -1
	}
	if left.Unix() > right.Unix() {
		return 1
	}
	fraction := func(value string) string {
		if len(value) <= 19 || value[19] != '.' {
			return ""
		}
		end := 20
		for end < len(value) && value[end] >= '0' && value[end] <= '9' {
			end++
		}
		return strings.TrimRight(value[20:end], "0")
	}
	return strings.Compare(fraction(a), fraction(b))
}

func Review(fields map[string]any, evaluatedAt string) (ReviewState, error) {
	state := ReviewState{Verified: []map[string]any{}, Tier: "unverified", Status: "stable", EvaluatedAt: evaluatedAt}
	if _, valid := Datetime(evaluatedAt); !valid {
		return state, fmt.Errorf("evaluated_at must be a timezone-qualified datetime")
	}
	if status, ok := fields["status"].(string); ok && (status == "draft" || status == "stable" || status == "deprecated") {
		state.Status = status
	}
	events, _ := metadataList(fields["verified"])
	if event, ok := fields["verified"].(map[string]any); ok {
		events = []any{event}
	}
	for _, raw := range events {
		event, ok := raw.(map[string]any)
		if !ok || len(MetadataDiagnostics(map[string]any{"verified": event})) != 0 {
			continue
		}
		state.Verified = append(state.Verified, event)
		by := event["by"].(string)
		at := event["at"].(string)
		if strings.HasPrefix(by, "human:") && strings.TrimSpace(strings.TrimPrefix(by, "human:")) != "" {
			state.Tier = "human-reviewed"
		} else if state.Tier == "unverified" {
			state.Tier = "machine-confirmed"
		}
		if state.LastVerifiedAt == "" || CompareDatetimes(at, state.LastVerifiedAt) > 0 {
			state.LastVerifiedAt = at
		}
	}
	if generated, ok := fields["generated"].(map[string]any); ok && len(MetadataDiagnostics(map[string]any{"generated": generated})) == 0 && state.LastVerifiedAt != "" {
		if at, ok := generated["at"].(string); ok {
			changed := CompareDatetimes(at, state.LastVerifiedAt) > 0
			state.ChangedSinceReview = &changed
		}
	}
	if _, valid := Datetime(fields["stale_after"]); valid {
		stale := CompareDatetimes(evaluatedAt, fields["stale_after"].(string)) >= 0
		state.Stale = &stale
	}
	return state, nil
}
