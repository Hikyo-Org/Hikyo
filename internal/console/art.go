package console

import _ "embed"

//go:embed full-art.txt
var fullArtwork string

// FullArtwork returns the byte-exact supplied logo for explicit display commands.
func FullArtwork() string { return fullArtwork }
