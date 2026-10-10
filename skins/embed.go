// Package skins holds the built-in viewer themes.
// A theme is a directory with a skin.json file.
package skins

import "embed"

//go:embed dark light
//go:embed catppuccin catppuccin-latte ethereal everforest flexoki-light gruvbox hackerman
//go:embed kanagawa last-horizon lumon lupine matte-black miasma nord osaka-jade
//go:embed retro-82 ristretto rose-pine solitude starship tokyo-night vantablack white
var FS embed.FS
