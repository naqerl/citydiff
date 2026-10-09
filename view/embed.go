// Package view serves the 3D scene.
package view

import "embed"

//go:embed index.html main.js layout.js search.js fly.js vendor/three.module.js vendor/OrbitControls.js
var FS embed.FS
