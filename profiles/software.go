package profiles

import "embed"

//go:embed software/profile.json software/templates/*.md
var Software embed.FS
