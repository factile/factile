package version

import (
	"strings"
)

const (
	Name           = "factile"
	defaultVersion = "v0.1.0"
)

var (
	Version = defaultVersion
	Commit  = ""
	Date    = ""
)

type Info struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	Date    string `json:"date,omitempty"`
}

func Current() Info {
	value := strings.TrimSpace(Version)
	if value == "" {
		value = defaultVersion
	}
	return Info{
		Name:    Name,
		Version: value,
		Commit:  strings.TrimSpace(Commit),
		Date:    strings.TrimSpace(Date),
	}
}

func (i Info) String() string {
	parts := []string{i.Name, i.Version}
	if i.Commit != "" {
		parts = append(parts, "commit "+i.Commit)
	}
	if i.Date != "" {
		parts = append(parts, "built "+i.Date)
	}
	return strings.Join(parts, " ")
}
