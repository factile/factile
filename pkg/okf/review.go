package okf

import (
	"fmt"
	"strings"
)

type ReviewState struct {
	Verified           []map[string]any  `json:"verified"`
	CurrentVerified    *[]map[string]any `json:"current_verified,omitempty"`
	Freshness          string            `json:"freshness,omitempty"`
	Tier               string            `json:"tier"`
	Status             string            `json:"status"`
	LastVerifiedAt     string            `json:"last_verified_at,omitempty"`
	ChangedSinceReview *bool             `json:"changed_since_review"`
	Stale              *bool             `json:"stale"`
	EvaluatedAt        string            `json:"evaluated_at"`
}

// CompareDatetimes retains sub-nanosecond precision as well as timezone semantics.
// Callers validate both inputs with Datetime first.
func CompareDatetimes(a, b string) int {
	left, _ := PortableDatetime(a)
	right, _ := PortableDatetime(b)
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

// PortableReview keeps historical evidence while deriving trust from current events.
func PortableReview(fields map[string]any, evaluatedAt string) ReviewState {
	current := []map[string]any{}
	state := ReviewState{
		Verified: []map[string]any{}, CurrentVerified: &current, Tier: "unverified",
		Status: "stable", Freshness: "unknown", EvaluatedAt: evaluatedAt,
	}
	if status := fields["status"]; !EmptyMetadata(status) {
		state.Status = "unknown"
		if text, ok := status.(string); ok {
			state.Status = text
		}
	}
	generated := fields["generated"]
	generationKnown := false
	var generatedAt string
	if authoring, ok := generated.(map[string]any); ok && PortableActor(authoring["by"]) {
		generatedAt, _ = authoring["at"].(string)
		_, generationKnown = PortableDatetime(generatedAt)
	}
	for _, raw := range MetadataItems(fields["verified"]) {
		event, ok := raw.(map[string]any)
		if !ok || !PortableActor(event["by"]) {
			continue
		}
		at, _ := event["at"].(string)
		if _, ok := PortableDatetime(at); !ok {
			continue
		}
		state.Verified = append(state.Verified, event)
		if state.LastVerifiedAt == "" || CompareDatetimes(at, state.LastVerifiedAt) > 0 {
			state.LastVerifiedAt = at
		}
		if EmptyMetadata(generated) || generationKnown && CompareDatetimes(at, generatedAt) >= 0 {
			current = append(current, event)
			if strings.HasPrefix(event["by"].(string), "human:") {
				state.Tier = "human-reviewed"
			} else if state.Tier == "unverified" {
				state.Tier = "machine-confirmed"
			}
		}
	}
	if generationKnown && state.LastVerifiedAt != "" {
		changed := CompareDatetimes(generatedAt, state.LastVerifiedAt) > 0
		state.ChangedSinceReview = &changed
	}
	if deadline := fields["stale_after"]; !EmptyMetadata(deadline) {
		if _, valid := PortableDatetime(deadline); valid {
			stale := CompareDatetimes(evaluatedAt, deadline.(string)) >= 0
			state.Stale = &stale
			state.Freshness = "fresh"
			if stale {
				state.Freshness = "stale"
			}
		} else {
			state.Freshness = "malformed"
		}
	}
	return state
}
