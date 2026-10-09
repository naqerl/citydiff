// Package view serves the 3D scene.
package view

import "embed"

//go:embed index.html main.js layout.js search.js fly.js edges.js tour.js tourui.js markdown.js keys.js viewport.js changes.js vendor/three.module.js vendor/OrbitControls.js
var FS embed.FS
