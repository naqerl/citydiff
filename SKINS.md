# Skins

A skin recolours the citydiff viewer. It is one JSON file. Each top-level key is one drawn element: the sky, the floor, a kind of block, a call arc, the name on a building, or the page around the city.

A file may set any subset of those keys. A key you leave out keeps the value from [dark](skins/dark/skin.json). [light](skins/light/skin.json) is the other built-in skin, the same city on a light ground. Both are always available. Custom skins stay outside this repository. Put them in the skins directory, or pass one path with `-skin`.

The default column in the tables below is the dark skin. The link on each field opens the line that reads it.

## Contents

- [Choose a skin](#choose-a-skin)
- [A script](#a-script)
- [Shaders and textures](#shaders-and-textures)
- [Write a file](#write-a-file)
- [Scene](#scene)
- [City](#city)
- [Page](#page)
- [A custom skin](#a-custom-skin)

## Choose a skin

```sh
citydiff -path . -view
citydiff -path . -view -skin light
citydiff -path . -view -skin paper
citydiff -path . -view -skin ~/skins/paper
citydiff -path . -view -skins ~/skins
```

`-skin` and `-skins` also work on `citydiff tour serve`. An empty `-skin` selects dark.

### Built-in skins

`dark` and `light` ship inside the program. `dark` is the skin the page opens on. A folder of the same name in the skins directory does not replace a built-in. To open that folder, pass its path to `-skin`.

### The skins directory

`-skins` is a directory of extra skins. Its default is `~/.config/citydiff/skins`. When that directory is missing, it is left uncreated and the only skins are the built-in ones.

Each entry in the directory is one skin:

| Entry | Name |
| --- | --- |
| `paper/skin.json` | `paper`. Files next to `skin.json` belong to that skin, so a relative image resolves. |
| `paper/skin.js` | `paper`, when the directory has no `skin.json`. The page runs the script once and uses the JSON object it returns. |
| `ink.json` | `ink`. The file is the whole skin. |

A name is letters, digits, `_`, and `-`. Other files in the directory are ignored. When both `paper/skin.json` and `paper.json` exist, the directory is the skin.

### `-skin`

`-skin` chooses the skin the page opens on.

| `-skin` value | What opens |
| --- | --- |
| empty | `dark` |
| a built-in name | That skin. `dark` and `light` are built in. |
| A name from the skins directory | That skin. The other skins in the directory stay available. |
| A path to a directory | `skin.json` in that directory, or `skin.js` when there is no `skin.json`. Files next to it are served with the skin. |
| A path to a file | That file. A `.js` file is run the same way as `skin.js`. |

A path that exists is used as the skin. A bare name selects a built-in, then a skin from the skins directory. An unknown name, or a directory with no `skin.json` or `skin.js`, is an error and the viewer does not start. A path passed to `-skin` is the skin the page opens on. It is not added to the skins directory.

### On the page

`?skin=` takes a skin name. A path or a URL in that parameter is refused, and the page opens dark.

| Query | What loads |
| --- | --- |
| no `skin` parameter, or `?skin=` | The skin selected with `-skin`. |
| `?skin=light` | The built-in light skin. |
| `?skin=paper` | The skin of that name from the skins directory. |

The name may contain letters, digits, `_`, and `-`. Every skin is filled in from dark. If the name cannot be read, the page uses dark and says so in the sidebar note.

The brush button in the bottom-right corner lists the built-in skins and the skins loaded from the skins directory. A path passed to `-skin` is on that list too. Choosing a named skin sets `?skin=` and recolours the open city. The camera, the selection, and the tour stay where they are. Choosing the path skin clears `?skin=`.

## A script

The viewer always reads a JSON object. `skin.json` is that object. A directory may contain `skin.js` instead. The page imports it once, before the city is built. The default export may be the object, or a function that returns it. The return value is serialized, so a function or `undefined` does not become part of the skin.

```js
export default function skin() {
  return {
    name: "paper",
    plane: { color: "#e4ded4" },
  };
}
```

When a directory contains both files, `skin.json` is the skin. Shaders and images stay files next to the script. The script names them. It does not run again after the first frame.

## Shaders and textures

Shaders and textures are optional. Leave them out and the element keeps the material it has in `dark`.

| Field | Where | What it does |
| --- | --- | --- |
| `map` | `plane`, `ground`, `horizon`, `package`, `external`, `entity` | An image next to the skin. The element colour tints it. |
| `repeat` | the same elements | Two numbers, how many times `map` tiles. |
| `maps` | the same elements | Named images a fragment shader samples. The name is the uniform, so `"uWall": "wall.png"` is `uniform sampler2D uWall`. |
| `vertex` | the same elements | A GLSL file inserted where the vertex position is built. Write `transformed`. [`uTime`](view/shade.js#L73) is seconds. `vUv` is the surface coordinate. |
| `fragment` | the same elements | A GLSL file inserted after the colour is chosen. Write `diffuseColor`. `vUv` and `uTime` are available here too. |
| `fragment` | `fog` | Replaces the fog on the city meshes. `fogFactor`, `fogColor`, `vFogDepth`, `vSkinWorld`, and `gl_FragColor` are in scope. |
| `segments` | `plane` | How finely the floor grid is divided when it has a vertex shader. The default is 64. |

On buildings, [`aHouse.x`](view/main.js#L1172) is `0` for a type, `1` for a function, and `2` for a method. `aHouse.y` is a variation from 0 to 1, steady for each declaration. Copy them to `vHouse` to use them in the fragment shader. `vLocalUp` is yours to set from `normal.y` when a fragment needs to know the top face. These names are read while the shader is compiled ([shade.js](view/shade.js)).

A shader that names `uTime` keeps the picture moving. The floor around the disc can be a displaced grid. The disc itself stays the ground under the city.

## Write a file

```json
{
  "name": "paper",
  "plane": { "color": "#e4ded4" },
  "label": { "background": "transparent", "text": "#1c1917" }
}
```

`name` is your label for the file. It is not drawn.

Colours on the city are `#rrggbb`. Page colours that need to be translucent may be `rgba()`. Numbers are plain JSON numbers.

A nested object merges with the dark object, so setting `hud.bg` leaves the other `hud` fields alone. A list replaces the whole list: if you set `package.steps`, include every step. A string or list skybox replaces the dark gradient object.

The keys are [`background`](view/skin.js#L7), [`fog`](view/skin.js#L8), [`plane`](view/skin.js#L9), [`ground`](view/skin.js#L10), [`horizon`](view/skin.js#L11), [`light`](view/skin.js#L12), [`package`](view/skin.js#L13), [`external`](view/skin.js#L14), [`type`](view/skin.js#L15), [`function`](view/skin.js#L16), [`method`](view/skin.js#L17), [`entity`](view/skin.js#L18), [`change`](view/skin.js#L19), [`call`](view/skin.js#L20), [`selection`](view/skin.js#L21), [`label`](view/skin.js#L22), [`dim`](view/skin.js#L23), and [`hud`](view/skin.js#L24).

## Scene

The city stands on a disc. A larger floor sits under that disc. Fog fades the city toward the horizon. The sky is a skybox, and fog leaves the sky alone. The usual camera looks down, so most of the sky you see is the horizon band, with the top colour in the upper corners.

The floor ignores clicks. The camera frames the city.

### [`background`](view/skin.js#L7)

| Field | Default | What it paints |
| --- | --- | --- |
| [`color`](view/main.js#L53) | `#09090b` | Clear colour behind the scene. Used on its own when `skybox` is omitted. |
| [`skybox`](view/sky.js#L30) | a gradient | The sky. Three shapes, below. |

`skybox` is one of:

- An object `{ "top", "horizon", "bottom" }` paints a generated sky. [`top`](view/sky.js#L56) is the zenith, [`horizon`](view/sky.js#L57) is the band at eye level, [`bottom`](view/sky.js#L58) is below the city. Dark uses `#07080d`, `#1a1c24`, and `#09090b`.
- A string, such as `"sky.webp"`, is one equirectangular image ([read here](view/sky.js#L36)). A relative path is resolved against the skin file. A path that starts with `/`, or a URL, is used as written.
- A list of six images, `["px", "nx", "py", "ny", "pz", "nz"]`, is the faces of a cube in +x, −x, +y, −y, +z, −z order ([read here](view/sky.js#L44)). Paths resolve the same way as the single image.

### [`fog`](view/skin.js#L8)

| Field | Default | What it paints |
| --- | --- | --- |
| [`color`](view/main.js#L54) | `#09090b` | Fog colour. Match it to the sky when you want the city to fade into the background. |
| [`falloff`](view/main.js#L1054) | `0.07` | How fast the fog thickens. It is divided by the width of the city, so the same number fades a large city and a small city at a similar rate. Raise it to hide the distance sooner. |

### [`plane`](view/skin.js#L9)

| Field | Default | What it paints |
| --- | --- | --- |
| [`color`](view/main.js#L1060) | `#2a2a32` | The floor under the city. |

### [`ground`](view/skin.js#L10)

| Field | Default | What it paints |
| --- | --- | --- |
| [`color`](view/main.js#L1068) | `#18181b` | The disc the packages stand on. |

### [`horizon`](view/skin.js#L11)

| Field | Default | What it paints |
| --- | --- | --- |
| [`color`](view/main.js#L1077) | `#3f3f46` | The ring around the disc. |
| [`opacity`](view/main.js#L1080) | `0.7` | How solid that ring is, from 0 to 1. |

### [`light`](view/skin.js#L12)

Three lights and an exposure. Their directions are fixed. You set colour and strength.

| Field | Default | What it paints |
| --- | --- | --- |
| [`exposure`](view/main.js#L52) | `1.08` | Overall brightness of the rendering. |
| [`hemiSky`](view/main.js#L724) | `#f4f4f5` | Sky side of the hemisphere light, the light from above. |
| [`hemiGround`](view/main.js#L724) | `#27272a` | Ground side of that light, the light bounced up from the floor. |
| [`hemiIntensity`](view/main.js#L724) | `0.55` | Strength of the hemisphere light. |
| [`key`](view/main.js#L726) | `#fafafa` | Colour of the main light. |
| [`keyIntensity`](view/main.js#L726) | `0.8` | Strength of the main light. |
| [`rim`](view/main.js#L729) | `#d4d4d8` | Colour of the rim light, on the opposite side from the key. |
| [`rimIntensity`](view/main.js#L729) | `0.18` | Strength of the rim light. |

## City

Overview draws each declaration in its kind colour: [`type`](view/main.js#L41), [`function`](view/main.js#L42), or [`method`](view/main.js#L43). Towers of one kind still differ a little in lightness.

The changes view paints a package or a declaration with the matching [`change`](view/main.js#L34) colour when its change is added, removed, modified, or moved. An unchanged declaration keeps its kind colour.

A package that is a dependency of the selection takes [`call.color`](view/main.js#L39) and the stronger dependency glow. Call arcs that changed take the change colour. An unchanged arc uses `call.color`, or [`call.std`](view/main.js#L40) when the other package is the standard library.

Anything outside the current focus is multiplied by [`dim`](view/skin.js#L23). `1` leaves a colour alone. `0` pushes it to black.

### [`package`](view/skin.js#L13)

A package is a block. Deeper packages walk the `steps` list and stop on the last colour. The legend's package swatch uses the second step.

| Field | Default | What it paints |
| --- | --- | --- |
| [`steps`](view/main.js#L1338) | `#27272a`, `#3f3f46`, `#52525b`, `#71717a` | Fill by depth. The first colour is the shallowest package. |
| [`synthetic`](view/main.js#L1337) | `#18181b` | The root block that is not a real package. |
| [`emissive`](view/main.js#L1092) | `#ffffff` | Glow colour. The glow takes on the package colour. |
| [`emissiveIntensity`](view/main.js#L1093) | `0.06` | Glow while the package is idle. |
| [`depEmissive`](view/main.js#L1938) | `0.62` | Glow while the package is a dependency of the selection. |
| [`markedEmissive`](view/main.js#L1938) | `0.22` | Glow while the changes view has marked the package. |

### [`external`](view/skin.js#L14)

A package from outside the snapshot. This is the one block with metalness and roughness. The others are lit without those fields.

| Field | Default | What it paints |
| --- | --- | --- |
| [`color`](view/main.js#L1110) | `#3f3f46` | Fill. |
| [`metalness`](view/main.js#L1111) | `0.35` | How metallic the surface is, from 0 to 1. |
| [`roughness`](view/main.js#L1112) | `0.6` | How rough the surface is, from 0 to 1. |
| [`emissive`](view/main.js#L1113) | `#000000` | Glow colour. |
| [`emissiveIntensity`](view/main.js#L1114) | `0.2` | Idle glow. |
| [`depEmissive`](view/main.js#L1926) | `0.7` | Glow while it is a dependency of the selection. |

### Kind colours

| Field | Default | What it paints |
| --- | --- | --- |
| [`type.color`](view/main.js#L41) | `#d4d4d8` | A type block. |
| [`function.color`](view/main.js#L42) | `#fafafa` | A function tower. |
| [`method.color`](view/main.js#L43) | `#a1a1aa` | A method tower. |

### [`entity`](view/skin.js#L18)

The material shared by every tower. The kind colour, or the change colour, tints each tower on top of it. Leave `color` white and those tints show on their own.

| Field | Default | What it paints |
| --- | --- | --- |
| [`color`](view/main.js#L1147) | `#ffffff` | Tint of the shared material. |
| [`emissive`](view/main.js#L1148) | `#ffffff` | Glow on every tower. |
| [`emissiveIntensity`](view/main.js#L1149) | `0.28` | Strength of that glow. |

### [`change`](view/skin.js#L19)

These colours paint marked packages and declarations, and changed call arcs. The legend swatches use the same four colours ([added](view/skin.js#L85), [removed](view/skin.js#L86), [modified](view/skin.js#L87), [moved](view/skin.js#L88)). The words in the sidebar are the `hud` text colours below, so a light object colour and a dark word can differ.

| Field | Default | What it paints |
| --- | --- | --- |
| [`added`](view/main.js#L34) | `#71d083` | Added. |
| [`removed`](view/main.js#L35) | `#e5484d` | Removed. |
| [`modified`](view/main.js#L36) | `#ffc53d` | Modified. |
| [`moved`](view/main.js#L37) | `#7d66d9` | Moved. |
| [`same`](view/main.js#L38) | `#f4f4f5` | The colour asked for when a change is unchanged. |

### [`call`](view/skin.js#L20)

| Field | Default | What it paints |
| --- | --- | --- |
| [`color`](view/main.js#L39) | `#23afd0` | A call arc, and a package shown as a dependency. |
| [`std`](view/main.js#L40) | `#d4d4d8` | An arc whose other end is the standard library. |

### [`selection`](view/skin.js#L21)

| Field | Default | What it paints |
| --- | --- | --- |
| [`color`](view/main.js#L44) | `#23afd0` | The halo on the selected declaration. |
| [`ring`](view/main.js#L55) | `#a1a1aa` | The ring drawn on the roof of an entered package. |
| [`ringOpacity`](view/main.js#L56) | `0.35` | How solid that ring is, from 0 to 1. |

### [`label`](view/skin.js#L22)

The name drawn on the faces of a package. The floating name that follows the pointer uses [`hud.fg`](view/skin.js#L72) and [`hud.shadow`](view/skin.js#L80).

| Field | Default | What it paints |
| --- | --- | --- |
| [`background`](view/main.js#L1281) | `transparent` | Plate behind the name. Leave it empty or `"transparent"` to draw the text alone. Any other colour fills the plate. |
| [`text`](view/main.js#L1282) | `#fafafa` | The name itself. |

On a light skin, keep the object colours light enough that this text still reads on top of them. The light skin does that, and keeps the sidebar words dark through the `hud` text colours.

### [`dim`](view/skin.js#L23)

Applied to whatever is outside the current focus.

| Field | Default | What it multiplies |
| --- | --- | --- |
| [`entity`](view/main.js#L2199) | `0.04` | A tower that is not part of the selection. |
| [`package`](view/main.js#L1941) | `0.2` | A package that is not part of the selection. |
| [`external`](view/main.js#L1930) | `0.22` | An external package in that same state. |
| [`emissive`](view/main.js#L1942) | `0.02` | The package glow while it is dimmed. |
| [`plate`](view/main.js#L1943) | `0.35` | Opacity of the name plate while its package is dimmed, from 0 to 1. |

## Page

[`hud`](view/skin.js#L24) colours the sidebar, the search box, the tour, the legend, and the menus. [`scheme`](view/skin.js#L67) is `"dark"` or `"light"` and sets the page colour scheme, including form controls and scrollbars.

The words for a change are separate from the 3D colours, so the legend swatch can stay pastel while the word stays dark. If you omit a text colour, the word uses the matching `change` colour.

| Field | Default | Where it shows |
| --- | --- | --- |
| [`bg`](view/skin.js#L68) | `#09090b` | Page background. |
| [`elev`](view/skin.js#L69) | `#18181b` | A raised row, and the legend figure. |
| [`panel`](view/skin.js#L70) | `rgba(9, 9, 11, 0.4)` | Sidebars. |
| [`search`](view/skin.js#L71) | `rgba(9, 9, 11, 0.35)` | Search field. |
| [`fg`](view/skin.js#L72) | `#fafafa` | Primary text, and the name that follows the pointer. |
| [`dim`](view/skin.js#L73) | `#d4d4d8` | Secondary text. |
| [`muted`](view/skin.js#L74) | `#a1a1aa` | Quieter text. |
| [`faint`](view/skin.js#L75) | `#71717a` | Faint text. |
| [`fainter`](view/skin.js#L76) | `#52525b` | The faintest text. |
| [`line`](view/skin.js#L77) | `#27272a` | Hairlines. |
| [`lineStrong`](view/skin.js#L78) | `#3f3f46` | Stronger borders and scrollbar thumbs. |
| [`edge`](view/skin.js#L79) | `rgba(255, 255, 255, 0.08)` | Sidebar edge. |
| [`shadow`](view/skin.js#L80) | `#000000` | Shadow under the floating name. |
| [`link`](view/skin.js#L81) | `#93c5fd` | Links. |
| [`code`](view/skin.js#L82) | `#fde68a` | Inline code. |
| [`accent`](view/skin.js#L83) | `#ffc53d` | Accent: the current tour step, the progress bar. |
| [`pre`](view/skin.js#L84) | `#e4e4e7` | Preformatted text. |
| [`addedText`](view/skin.js#L89) | `#71d083` | The word "added". |
| [`removedText`](view/skin.js#L90) | `#e5484d` | The word "removed". |
| [`modifiedText`](view/skin.js#L91) | `#ffc53d` | The word "modified". |
| [`movedText`](view/skin.js#L92) | `#7d66d9` | The word "moved". |
| [`addedBg`](view/skin.js#L98) | `rgba(34, 197, 94, 0.14)` | Background of an added code line. |
| [`addedFg`](view/skin.js#L99) | `#bbf7d0` | Text of an added code line. |
| [`removedBg`](view/skin.js#L100) | `rgba(239, 68, 68, 0.14)` | Background of a removed code line. |
| [`removedFg`](view/skin.js#L101) | `#fecaca` | Text of a removed code line. |
| [`addedLine`](view/skin.js#L102) | `#4ade80` | Mark on an added row. |
| [`removedLine`](view/skin.js#L103) | `#f87171` | Mark on a removed row. |
| [`modifiedLine`](view/skin.js#L104) | `#facc15` | Mark on a modified row. |
| [`codeBg`](view/skin.js#L105) | `rgba(12, 12, 14, 0.8)` | Code block background. |
| [`drop`](view/skin.js#L106) | `rgba(9, 9, 11, 0.6)` | Dropped-file target. |
| [`dropBorder`](view/skin.js#L107) | `#facc15` | Border of that target. |
| [`scrim`](view/skin.js#L108) | `rgba(9, 9, 11, 0.78)` | Scrim behind a menu. |
| [`on`](view/skin.js#L109) | `rgba(24, 24, 27, 0.55)` | The pressed state of a button. |
| [`currentBg`](view/skin.js#L110) | `rgba(255, 197, 61, 0.08)` | The current tour step. |

## A custom skin

A directory skin with its own sky:

```
paper/
  skin.json
  sky.webp
```

```json
{
  "name": "paper",
  "background": {
    "color": "#f4f1ea",
    "skybox": "sky.webp"
  },
  "fog": { "color": "#e7e2d8", "falloff": 0.07 },
  "plane": { "color": "#e4ded4" },
  "ground": { "color": "#f4f1ea" },
  "function": { "color": "#d9d3c8" },
  "label": { "background": "transparent", "text": "#1c1917" },
  "hud": {
    "scheme": "light",
    "bg": "#faf8f4",
    "fg": "#1c1917",
    "addedText": "#1f7a4d",
    "removedText": "#d12b31",
    "modifiedText": "#a16207",
    "movedText": "#6e56cf"
  }
}
```

Put that directory at `~/.config/citydiff/skins/paper` and open `http://127.0.0.1:8787/?skin=paper`. The page opens on dark until the query names the skin. To open paper immediately:

```sh
citydiff -path . -view -skin paper
```

A skin that lives somewhere else is passed by path. That path is the skin the page opens on, and `?skin=` still switches among `dark`, `light`, and the skins directory.

```sh
citydiff -path . -view -skin ~/skins/paper
```

`?skin=light` compares it with the built-in light skin. `?skin=` with no value returns to the skin `-skin` selected.
