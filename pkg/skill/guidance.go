package skill

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/factile/factile/pkg/okf"
	"github.com/factile/factile/pkg/version"
)

const checksumField = "  factile-content-sha256: "

// Installation reports one installed skill without changing it or probing MCP.
type Installation struct {
	Scope   string `json:"scope"`
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
	Status  string `json:"status"`
	Mode    string `json:"mode,omitempty"`
	Profile string `json:"profile,omitempty"`
	Message string `json:"message"`
}

func stampSkillMarkdown(content string) string {
	content = strings.NewReplacer(
		"{{VERSION}}", strconv.Quote(version.Current().Version),
		"{{CONTENT_SHA256}}", `""`,
	).Replace(content)
	checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	return strings.Replace(content, checksumField+`""`, checksumField+strconv.Quote(checksum), 1)
}

// The checksum covers every byte with its own value replaced by an empty string.
func inspectSkillMetadata(data []byte, state *installedSkillState) {
	doc, err := okf.ParseConcept("SKILL", data)
	if err != nil {
		state.MetadataInvalid = true
		return
	}
	metadata, _ := doc.Frontmatter["metadata"].(map[string]any)
	versionValue, hasVersion := metadata["version"]
	checksumValue, hasChecksum := metadata["factile-content-sha256"]
	if !hasVersion && !hasChecksum {
		return
	}
	state.Version, _ = versionValue.(string)
	checksum, _ := checksumValue.(string)
	line := checksumField + strconv.Quote(checksum) + "\n"
	if state.Version == "" || len(checksum) != 64 || strings.Count(string(data), line) != 1 {
		state.MetadataInvalid = true
		return
	}
	unsigned := strings.Replace(string(data), line, checksumField+"\"\"\n", 1)
	state.Modified = checksum != fmt.Sprintf("%x", sha256.Sum256([]byte(unsigned)))
}

func guidanceStatus(state installedSkillState) string {
	switch {
	case !state.Recognized:
		return "unrecognized"
	case state.MetadataInvalid:
		return "invalid"
	case state.Modified:
		return "modified"
	case state.Version == "":
		return "unversioned"
	case !state.Current:
		return "outdated"
	default:
		return "current"
	}
}

func guidanceMessage(scope string, state installedSkillState) string {
	label := "Factile " + scope + " skill"
	if state.Version != "" {
		label += " " + state.Version
	}
	repair := "run `factile init` in the selected workspace"
	if scope == "user" {
		command := "factile skill install codex --scope user --mode " + state.Mode
		if state.Profile != "" {
			command += " --profile " + state.Profile
		}
		repair = "run `" + command + "`"
	}
	switch guidanceStatus(state) {
	case "unrecognized":
		return label + " is not recognized as generated guidance; preserve it and move it aside before installing."
	case "invalid":
		return label + " has invalid version or checksum metadata; restore the generated copy or preserve it elsewhere before reinstalling."
	case "modified":
		return label + " has local edits; preserve them elsewhere and restore the generated copy before repair."
	case "unversioned":
		return label + " has no version or checksum; review local edits, then " + repair + " to install guidance for " + version.Current().Version + "."
	case "outdated":
		return label + " differs from the guidance bundled with binary " + version.Current().Version + "; " + repair + "."
	default:
		return label + " matches the running binary."
	}
}

func installedGuidance(workDir string) []Installation {
	result := []Installation{}
	for _, target := range []struct{ scope, path string }{
		{"repo", filepath.Join(workDir, ".agents", "skills", "factile", "SKILL.md")},
		{"user", filepath.Join(codexHome(), "skills", "factile", "SKILL.md")},
	} {
		state := inspectInstalledSkill(target.path)
		if state.Exists {
			result = append(result, Installation{
				Scope: target.scope, Path: target.path, Version: state.Version,
				Status: guidanceStatus(state), Mode: state.Mode, Profile: state.Profile,
				Message: guidanceMessage(target.scope, state),
			})
		}
	}
	return result
}

// GuidanceWarnings checks local installed copies against this binary's assets.
func GuidanceWarnings(workspace string) []string {
	workDir, err := doctorWorkDir(workspace)
	if err != nil {
		return nil
	}
	var warnings []string
	for _, installation := range installedGuidance(workDir) {
		if installation.Status != "current" {
			warnings = append(warnings, installation.Message)
		}
	}
	return warnings
}
