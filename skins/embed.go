// Package skins holds the built-in viewer themes.
// A theme is a directory with a skin.json file.
package skins

import "embed"

//go:embed dark light
var FS embed.FS
