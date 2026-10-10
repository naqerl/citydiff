// Package view serves the 3D scene.
package view

import "embed"

//go:embed index.html main.js skin.js shade.js sky.js layout.js search.js fly.js tour.js tourui.js markdown.js keys.js viewport.js changes.js state.js vendor/three.module.js editor.js bird.js vendor/OrbitControls.js vendor/loaders/GLTFLoader.js vendor/utils/BufferGeometryUtils.js vendor/ghostty/ghostty-web.js vendor/ghostty/__vite-browser-external-2447137e.js vendor/ghostty/LICENSE diff.js highlight.js
var FS embed.FS
