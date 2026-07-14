package vfs

import (
	"net"
	"net/url"
	"regexp"
	"strings"
)

const (
	SourceKindLocal   = "local"
	SourceKindFactile = "factile"
	SourceKindGit     = "git"
)

type SourceClassification struct {
	Source    string `json:"source"`
	Kind      string `json:"kind"`
	GitRemote string `json:"git_remote,omitempty"`
}

var scpRemotePattern = regexp.MustCompile(`^(?:[A-Za-z0-9._~+-]+@)?(?:[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?|\[[A-Za-z0-9.:%-]+\]):[^\x00-\x20\x7f\\]+$`)

// ClassifySource classifies mount source syntax without consulting the
// filesystem. Source is preserved verbatim; only GitRemote removes the legacy
// git+ compatibility prefix.
func ClassifySource(source string) (SourceClassification, error) {
	if source == "" || strings.TrimSpace(source) != source || hasASCIIControl(source) {
		return SourceClassification{}, sourceValidationError("Mount source is invalid")
	}
	if strings.HasPrefix(source, "factile://") {
		return SourceClassification{Source: source, Kind: SourceKindFactile}, nil
	}
	if strings.HasPrefix(source, "git+") {
		remote := strings.TrimPrefix(source, "git+")
		if strings.HasPrefix(remote, "git+") || !isGitRemote(remote) {
			return SourceClassification{}, sourceValidationError("git+ must prefix one valid Git remote")
		}
		return SourceClassification{Source: source, Kind: SourceKindGit, GitRemote: remote}, nil
	}
	if hasGitURIPrefix(source) {
		if !validGitURI(source) {
			return SourceClassification{}, sourceValidationError("Git remote URI is invalid")
		}
		return SourceClassification{Source: source, Kind: SourceKindGit, GitRemote: source}, nil
	}
	if isExplicitLocalPath(source) {
		return SourceClassification{Source: source, Kind: SourceKindLocal}, nil
	}
	if hasUnbracketedIPv6Host(source) {
		return SourceClassification{}, sourceValidationError("SCP-style Git remotes require bracketed IPv6 hosts")
	}
	if validSCPRemote(source) {
		return SourceClassification{Source: source, Kind: SourceKindGit, GitRemote: source}, nil
	}
	return SourceClassification{Source: source, Kind: SourceKindLocal}, nil
}

func sourceValidationError(message string) error {
	return &Error{Code: "validation_failed", Message: message}
}

func hasASCIIControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func hasGitURIPrefix(source string) bool {
	for _, prefix := range []string{"https://", "http://", "ssh://", "git://", "file://"} {
		if strings.HasPrefix(source, prefix) {
			return true
		}
	}
	return false
}

func validGitURI(source string) bool {
	if strings.ContainsAny(source, "\\ \t\r\n") {
		return false
	}
	parsed, err := url.Parse(source)
	if err != nil || parsed.Path == "" || parsed.Path == "/" {
		return false
	}
	if parsed.Scheme == "file" {
		return strings.HasPrefix(parsed.Path, "/")
	}
	return parsed.Host != ""
}

func isExplicitLocalPath(source string) bool {
	if strings.HasPrefix(source, "/") || strings.HasPrefix(source, "\\") ||
		strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") ||
		strings.HasPrefix(source, `.\`) || strings.HasPrefix(source, `..\`) {
		return true
	}
	return len(source) >= 2 && isASCIIAlpha(source[0]) && source[1] == ':'
}

func isASCIIAlpha(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func isGitRemote(source string) bool {
	return hasGitURIPrefix(source) && validGitURI(source) || validSCPRemote(source)
}

func validSCPRemote(source string) bool {
	return !strings.Contains(source, "://") && !hasUnbracketedIPv6Host(source) && scpRemotePattern.MatchString(source)
}

func hasUnbracketedIPv6Host(source string) bool {
	if strings.HasPrefix(source, "[") || strings.Contains(source, "\\") {
		return false
	}
	if at := strings.LastIndex(source, "@"); at >= 0 {
		source = source[at+1:]
	}
	for index, r := range source {
		if r != ':' || index == 0 {
			continue
		}
		candidate := source[:index]
		if zone := strings.LastIndex(candidate, "%"); zone >= 0 {
			candidate = candidate[:zone]
		}
		if strings.Contains(candidate, ":") && net.ParseIP(candidate) != nil {
			return true
		}
	}
	return false
}
