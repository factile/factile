package render

import (
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
)

const defaultWidth = 88

type ColorMode string

const (
	ColorAuto   ColorMode = "auto"
	ColorAlways ColorMode = "always"
	ColorNever  ColorMode = "never"
)

type Options struct {
	ColorMode        ColorMode
	StdoutIsTerminal bool
	Env              map[string]string
	Width            int
}

type Renderer struct {
	colorEnabled bool
	width        int
	markdown     *glamour.TermRenderer
	styles       Styles
}

func ParseColorMode(value string) (ColorMode, error) {
	switch ColorMode(value) {
	case ColorAuto, ColorAlways, ColorNever:
		return ColorMode(value), nil
	default:
		return "", fmt.Errorf("unsupported color mode: %s", value)
	}
}

func New(opts Options) (*Renderer, error) {
	if opts.ColorMode == "" {
		opts.ColorMode = ColorAuto
	}
	if opts.Width <= 0 {
		opts.Width = defaultWidth
	}
	colorEnabled := ResolveColor(opts)
	markdown, err := newMarkdownRenderer(colorEnabled, opts.Width)
	if err != nil {
		return nil, err
	}
	return &Renderer{
		colorEnabled: colorEnabled,
		width:        opts.Width,
		markdown:     markdown,
		styles:       NewStyles(colorEnabled),
	}, nil
}

func IsTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (r *Renderer) ColorEnabled() bool {
	return r.colorEnabled
}

func (r *Renderer) Width() int {
	return r.width
}

func (r *Renderer) Styles() Styles {
	return r.styles
}

func (r *Renderer) RenderMarkdown(markdown string) (string, error) {
	rendered, err := r.markdown.Render(strings.TrimRight(markdown, "\n"))
	if err != nil {
		return "", err
	}
	if !r.colorEnabled {
		rendered = stripTerminalEscapes(rendered)
	}
	return rendered, nil
}

func newMarkdownRenderer(colorEnabled bool, width int) (*glamour.TermRenderer, error) {
	options := []glamour.TermRendererOption{glamour.WithWordWrap(width)}
	if colorEnabled {
		options = append(options, glamour.WithStandardStyle("dark"))
	} else {
		options = append(options, glamour.WithStyles(styles.ASCIIStyleConfig))
	}
	return glamour.NewTermRenderer(options...)
}

func stripTerminalEscapes(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '\x1b' {
			b.WriteByte(value[i])
			continue
		}
		if i+1 >= len(value) {
			continue
		}
		switch value[i+1] {
		case '[':
			i += 2
			for i < len(value) && (value[i] < 0x40 || value[i] > 0x7e) {
				i++
			}
		case ']':
			i += 2
			for i < len(value) {
				if value[i] == '\a' {
					break
				}
				if value[i] == '\x1b' && i+1 < len(value) && value[i+1] == '\\' {
					i++
					break
				}
				i++
			}
		default:
			i++
		}
	}
	return b.String()
}
