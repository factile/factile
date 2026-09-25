package contextpack

import "encoding/json"

// EvidenceTokens counts the complete serialized concept and included summary.
// It is an estimate, not a model tokenizer; selection never truncates evidence.
func EvidenceTokens(concept, summary any) (int, error) {
	data, err := json.Marshal([]any{concept, summary})
	return (len(data) + 3) / 4, err
}

func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	tokens := len(text) / 4
	if tokens == 0 {
		return 1
	}
	return tokens
}
