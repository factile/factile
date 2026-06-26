package render

import "charm.land/lipgloss/v2"

type Styles struct {
	Heading lipgloss.Style
	Label   lipgloss.Style
	Value   lipgloss.Style
	Muted   lipgloss.Style
	Warning lipgloss.Style
	Error   lipgloss.Style
}

func NewStyles(colorEnabled bool) Styles {
	if !colorEnabled {
		return Styles{}
	}
	return Styles{
		Heading: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#3b82f6")),
		Label:   lipgloss.NewStyle().Foreground(lipgloss.Color("#64748b")),
		Value:   lipgloss.NewStyle().Foreground(lipgloss.Color("#e2e8f0")),
		Muted:   lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8")),
		Warning: lipgloss.NewStyle().Foreground(lipgloss.Color("#f59e0b")),
		Error:   lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444")),
	}
}
