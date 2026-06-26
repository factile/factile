package contextpack

import "testing"

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int
	}{
		{name: "empty", text: "", want: 0},
		{name: "short non-empty", text: "abc", want: 1},
		{name: "four chars", text: "abcd", want: 1},
		{name: "eight chars", text: "abcdefgh", want: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EstimateTokens(tc.text); got != tc.want {
				t.Fatalf("EstimateTokens(%q) = %d, want %d", tc.text, got, tc.want)
			}
		})
	}
}
