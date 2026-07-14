package vfs

import (
	"reflect"
	"testing"
)

func TestClassifySource(t *testing.T) {
	tests := []struct {
		name   string
		source string
		kind   string
		remote string
	}{
		{name: "native HTTPS", source: "https://github.com/senseware/coding-practice.git", kind: SourceKindGit, remote: "https://github.com/senseware/coding-practice.git"},
		{name: "native HTTP without suffix", source: "http://example.test/team/docs", kind: SourceKindGit, remote: "http://example.test/team/docs"},
		{name: "native SSH", source: "ssh://git@github.com/senseware/coding-practice.git", kind: SourceKindGit, remote: "ssh://git@github.com/senseware/coding-practice.git"},
		{name: "native Git protocol", source: "git://example.test/team/docs", kind: SourceKindGit, remote: "git://example.test/team/docs"},
		{name: "native file URI", source: "file:///srv/remotes/docs.git", kind: SourceKindGit, remote: "file:///srv/remotes/docs.git"},
		{name: "SCP with user", source: "git@github.com:senseware/coding-practice.git", kind: SourceKindGit, remote: "git@github.com:senseware/coding-practice.git"},
		{name: "SCP without user or suffix", source: "example.test:team/docs", kind: SourceKindGit, remote: "example.test:team/docs"},
		{name: "SCP bracketed IPv6", source: "git@[2001:db8::1]:team/docs.git", kind: SourceKindGit, remote: "git@[2001:db8::1]:team/docs.git"},
		{name: "git plus URI", source: "git+https://github.com/senseware/coding-practice.git", kind: SourceKindGit, remote: "https://github.com/senseware/coding-practice.git"},
		{name: "git plus SCP", source: "git+git@github.com:senseware/coding-practice.git", kind: SourceKindGit, remote: "git@github.com:senseware/coding-practice.git"},
		{name: "hosted Factile", source: "factile://public/shared", kind: SourceKindFactile},
		{name: "POSIX absolute", source: "/srv/knowledge", kind: SourceKindLocal},
		{name: "POSIX explicit relative", source: "./docs:archive", kind: SourceKindLocal},
		{name: "POSIX parent relative", source: "../knowledge", kind: SourceKindLocal},
		{name: "Windows drive absolute", source: `C:\knowledge\coding`, kind: SourceKindLocal},
		{name: "Windows drive slash", source: `C:/knowledge/coding`, kind: SourceKindLocal},
		{name: "Windows drive relative", source: `C:knowledge\coding`, kind: SourceKindLocal},
		{name: "Windows UNC", source: `\\server\share\coding`, kind: SourceKindLocal},
		{name: "Windows explicit relative", source: `.\docs:archive`, kind: SourceKindLocal},
		{name: "ordinary relative", source: "knowledge/coding", kind: SourceKindLocal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ClassifySource(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			want := SourceClassification{Source: tc.source, Kind: tc.kind, GitRemote: tc.remote}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("classification mismatch\nwant: %#v\ngot:  %#v", want, got)
			}
			again, err := ClassifySource(tc.source)
			if err != nil || !reflect.DeepEqual(again, got) {
				t.Fatalf("classification was not deterministic: first=%#v second=%#v err=%v", got, again, err)
			}
		})
	}
}

func TestClassifySourceRejectsInvalidGitIntent(t *testing.T) {
	for _, source := range []string{
		"",
		" git@example.test:team/docs.git",
		"git+",
		"git+git+https://example.test/team/docs.git",
		"git+./local",
		"git+ftp://example.test/team/docs.git",
		"git+factile://public/shared",
		"https://",
		"ssh://example.test",
		"file://relative",
		"2001:db8::1:team/docs.git",
	} {
		t.Run(source, func(t *testing.T) {
			_, err := ClassifySource(source)
			if err == nil {
				t.Fatalf("invalid Git source %q was accepted", source)
			}
			if typed, ok := err.(*Error); !ok || typed.Code != "validation_failed" {
				t.Fatalf("invalid Git source %q returned %T %v", source, err, err)
			}
		})
	}
}
