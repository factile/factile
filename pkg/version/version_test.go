package version

import "testing"

func TestCurrentUsesDefaultVersion(t *testing.T) {
	oldVersion := Version
	t.Cleanup(func() { Version = oldVersion })

	Version = ""
	info := Current()
	if info.Name != "factile" || info.Version != defaultVersion {
		t.Fatalf("unexpected version info: %#v", info)
	}
}

func TestInfoStringIncludesBuildMetadataWhenPresent(t *testing.T) {
	info := Info{Name: "factile", Version: "v0.1.0", Commit: "abc123", Date: "2026-06-26T00:00:00Z"}
	want := "factile v0.1.0 commit abc123 built 2026-06-26T00:00:00Z"
	if got := info.String(); got != want {
		t.Fatalf("Info.String() = %q, want %q", got, want)
	}
}
