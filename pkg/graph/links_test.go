package graph

import "testing"

func TestExtractMarkdownLinksFiltersExternalAnchorsAndTrimsFragments(t *testing.T) {
	links := ExtractMarkdownLinks(`
[Runbook](../runbooks/ocr-failure.md#steps)
[Absolute](/policies/retry.md)
[Web](https://example.com/doc.md)
[Anchor](#local)
[Empty](   )
[Factile](factile://shared/doc)
`)
	if len(links) != 3 {
		t.Fatalf("expected three local links, got %#v", links)
	}
	wantTargets := []string{"../runbooks/ocr-failure.md", "/policies/retry.md", "factile://shared/doc"}
	for i, want := range wantTargets {
		if links[i].Target != want {
			t.Fatalf("link %d target = %q, want %q; links=%#v", i, links[i].Target, want, links)
		}
	}
	if links[0].Raw != "../runbooks/ocr-failure.md#steps" {
		t.Fatalf("raw link should preserve fragment, got %#v", links[0])
	}
}

func TestResolveLink(t *testing.T) {
	cases := []struct {
		name   string
		from   string
		target string
		want   string
		ok     bool
	}{
		{name: "relative markdown", from: "/product-docs/workflows/invoice-import", target: "../runbooks/ocr-failure.md", want: "/product-docs/runbooks/ocr-failure", ok: true},
		{name: "absolute markdown", from: "/product-docs/workflows/invoice-import", target: "/policies/retry.md", want: "/policies/retry", ok: true},
		{name: "same folder", from: "/product-docs/workflows/invoice-import", target: "approval.md", want: "/product-docs/workflows/approval", ok: true},
		{name: "factile URI", from: "/product-docs/workflows/invoice-import", target: "factile://shared/doc", ok: false},
		{name: "empty", from: "/product-docs/workflows/invoice-import", target: "", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ResolveLink(tc.from, tc.target)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("ResolveLink(%q, %q) = %q, %v; want %q, %v", tc.from, tc.target, got, ok, tc.want, tc.ok)
			}
		})
	}
}
