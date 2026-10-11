---
name: citydiff-video
description: Use only when the user directly asks to record a video, screen recording or screencast of the citydiff viewer or of a tour — "record a video of this", "film the citydiff tour", "make a screencast", "record a demo of the 3D diff". Drives a headless Chromium at 1920x1080 against a running citydiff viewer, drives it with real key and mouse input, and writes an H.264 mp4. Do not load this to explain a diff, write a tour, or answer questions about the viewer: that is the citydiff skill.
---

# __SKILL_NAME__ — record the viewer to a video

Recording the viewer is a browser-driving job, not a citydiff job: citydiff only has to be
*served*. Everything that decides whether the result is watchable is headless Chromium, and
the failure modes are all in the browser.

## Use this only when directly asked

Load this when the user asks for a recording. Do not load it because a tour would be nice to
watch, because a README shows videos, or because you want to verify the viewer yourself.

## The one thing that decides the quality

**Headless Chromium with no hardware GL falls back to SwiftShader, which renders this city at
about 1–8 frames per second.** The recorder then pads to 25 fps with duplicate frames, and the
result reads as judder. Nothing else you do matters as much as getting a real GPU.

Measured on this machine at 1920x1080, counting frames a CDP screencast actually delivered while
the camera orbits:

| flags | renderer | real fps |
| --- | --- | --- |
| `--use-angle=swiftshader --enable-unsafe-swiftshader` | SwiftShader | **1.2 – 8** |
| `--use-gl=angle --use-angle=gl` | Intel HD 620 | **31 – 60** |
| `--use-angle=vulkan` | Intel HD 620 | **33** |

1. Launch with `--use-gl=angle --use-angle=gl --enable-gpu`. If those flags give no GPU, find one
   that does — `ls /dev/dri` must list `renderD*`. On a machine with no GPU at all, say so and
   expect single-digit fps; do not ship a padded recording as though it were smooth.
2. Prove the rate **before** recording anything long. `record.mjs --check URL` serves nothing
   itself: point it at a running viewer and it counts the frames the browser actually delivers
   while the camera orbits, exiting non-zero below 20 fps. This is the only number that cannot be
   faked by the muxer, so trust it over the container.

   ```sh
   node record.mjs --check http://127.0.0.1:8890/    # exits 1 if too slow
   ```

   `CITYDIFF_GL_ARGS` overrides the flags, which is how to reproduce the bad case deliberately
   instead of by accident.

## Setup

1. **Install the browser tooling.** Playwright must have both a browser and an ffmpeg of the
   exact revision it expects.

   ```sh
   npm install -g playwright-core          # record.mjs finds it, or set CITYDIFF_PLAYWRIGHT
   npx playwright@1.64.0 install chromium ffmpeg
   ```

   A missing or mismatched ffmpeg does not throw at record time: the recording finishes, writes
   no webm, and fails at the mux step. Check `~/.cache/ms-playwright/` actually has both
   `chromium-*` and `ffmpeg-*`.

2. **Serve the viewer in the background on a free port.** It never exits, so never call it in the
   foreground. Check the port is free first — something already listening there is not yours.

   ```sh
   nohup __BIN_NAME__ -path REPO -range BASE..HEAD -addr 127.0.0.1:8890 > /tmp/cd.log 2>&1 &
   curl -fsS -o /dev/null http://127.0.0.1:8890/     # poll, do not block on the process
   ```

3. **Wait for the city.** The scene builds client-side and shows "Drawing the call arcs" for
   **9–12 s** after load. Any key pressed before that is lost. `record.mjs` already waits 9 s;
   wait longer yourself before driving anything.

## Drive it

Prefer **URL state over clicking**. The viewer writes its selection, mode, call direction and
sidebars into the address bar, so one navigation puts the camera and the sidebars where you want
them — deterministic and reproducible, and no clicking into 3D.

```
?select=citydiff:cmd:cli:viewer:serveDiff&mode=changes&refs=callers&side=closed
```

A method is `…:Type:Name`, one `:` per step from the module root.

Then use real keys for the things only input can do:

| want | do |
| --- | --- |
| search then jump | `/`, type the name, `Enter` |
| the real unified diff | `D` (closes with `esc`) |
| nvim over the city | `E` (closes with `:q!` + `Enter`) |
| bird's-eye district map | `y` |
| overview / changes | `1` / `2` |
| calls ⇄ callers | `c` |
| sidebar, legend | `b`, `?` |
| tour play / step | `space`, `.` |

### Traps that cost a whole recording

- **A tour loads paused on step 1.** Autoplay does not start by itself — press `space` or the
  video is 140 s of a static camera on one step.
- **After `/` search, the input keeps focus.** `view/keys.js` ignores every key typed into an
  `INPUT`, so a following `D` or `E` is silently typed as text instead of acting. Either drive
  the search *last*, or blur it first.
- **Declaration rows sit inside collapsed sections.** A row like `viewer.handler` is under the
  `Methods` section head; Playwright's click is intercepted by that head until you expand it
  (`button.section-head`), and `scrollIntoViewIfNeeded` does not help.
- **`esc` does not close nvim.** nvim consumes the key. `:q!` + `Enter` is the way out.
- **Open the legend from a pulled-back camera.** `1` does *not* zoom out when something is
  selected. Send `esc` then `0` first, or the legend sits on two towers filling the frame.
- **Multi-letter theme jumps are separate presses.** `keyboard.press("ro")` throws; press `r`,
  pause, press `o`.

### Themes

`/` then type `/skin`, `Enter` opens the menu. `←`/`→` and single letters preview in place; a
theme is remembered in `localStorage` under `citydiff.skin`, and a fresh browser context per
recording starts on `dark`.

## Record

`record.mjs` takes the browser args, the fixed 1920x1080 viewport, the wait for the city, and
runs a scenario module you write per video:

```js
import { record } from "./record.mjs";
await record("01-tour", { url: "http://127.0.0.1:8890/", run: async ({ page }) => {
  await page.keyboard.press("space");
  await page.waitForTimeout(140000);
}});
```

It mutes audio, forces `--force-device-scale-factor=1`, converts the webm to H.264 yuv420p
`+faststart` at `-crf 18`, and **reports the measured motion fps of its own output**.

## Check the output before calling it done

Container metadata always claims 25 fps, whether or not the frames are real. Two checks, in
order of trust:

1. **`node record.mjs --check URL` before recording** — counts the frames the browser actually
   delivered. Ground truth; cannot be faked by the muxer.
2. **`python3 motion.py OUT.mp4`** after — counts frames that actually differ from the one
   before. `record.mjs` runs this on every file it writes. It is approximate, because a lossy
   round trip puts a floor under every frame difference: a `median` near 25 means no obvious
   padding, a median in single digits means the source could not keep up.

The "% held" `motion.py` also prints is *expected to be high* — showcases hold on each step on
purpose so a human can read them. Only a low fps-while-moving means judder.

## Checklist

- [ ] `--check` confirms a real GPU at ≥ 20 fps, not assumed from the container.
- [ ] City fully built (9–12 s) before the first key.
- [ ] Tour pressed `space`; multi-letter theme jumps pressed letter by letter.
- [ ] No key meant for the viewer sent while the search input had focus.
- [ ] nvim closed with `:q!`, diff closed with `esc`.
- [ ] `motion.py` reports fps-while-moving near 25 for every output file.

## Files

| file | what |
| --- | --- |
| `SKILL.md` | this file |
| `record.mjs` | the recorder; `--check` measures the renderer, `record()` runs a scenario |
| `motion.py` | the padded-video check |
| `playwright.mjs` | finds `playwright-core` and its Chromium |

Scenario scripts live next to `record.mjs`, one per video; the tour shown in `examples/` at
the repository root is a good shape for one.
