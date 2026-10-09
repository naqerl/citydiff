---
name: citydiff-tour
description: Write a guided "tour" of a commit range's changes that the citydiff 3D viewer plays step by step (focus nodes, notes, call paths, camera, code diffs). Use when asked to explain, walk through, present or review what a Go or Rust commit range changed, visually.
---

# citydiff tour

A tour is a JSON script over the citydiff scene of one commit range. The
viewer plays it: each step moves the camera, lights nodes or a call path,
and shows a markdown note in the tour sidebar on the right. The sidebar
holds the tour title and range, the note and code, a roadmap of every step,
and play/pause, prev/next and a step counter at the bottom.

## Workflow

1. **List the nodes** of the range. Use these names in the script.

   ```sh
   citydiff nodes -path REPO -range BASE..HEAD -changed          # kind, change, name
   citydiff nodes -path REPO -range BASE..HEAD -changed -json    # [{name, kind, id, package, change, file}]
   citydiff nodes -path REPO -range BASE..HEAD -kind method      # all methods, changed or not
   ```

   Read the diff too (`git diff BASE..HEAD`, or `citydiff -path REPO -range BASE..HEAD -json`)
   so the notes say what changed and why, not only where.

2. **Write the script**, e.g. `tour.json` (schema below). Tell a story:
   start wide (overview of the change), then go package by package, then down
   to the functions and call paths that matter. 5–12 steps is a good length.

3. **Validate** it. This is the only check a tour needs: `citydiff tour
   validate` is the **sole source of truth** for whether a tour is correct. It
   resolves every name against the real scene the viewer builds, so a tour it
   accepts needs no other validation — do not open the viewer, take a
   screenshot, or drive a browser to confirm one. Fix every problem it prints;
   unknown names come with suggestions, ambiguous names list the candidates to
   pick from.

   ```sh
   citydiff tour validate -path REPO tour.json            # range comes from the script
   citydiff tour validate -path REPO tour.json -json      # the resolved script (scene ids, call paths)
   ```

4. **Open** it. This is for watching, not for checking: validation already
   decided whether the script is correct. The server never exits, so run it in
   the background.

   ```sh
   nohup citydiff tour serve -path REPO -addr 127.0.0.1:8787 tour.json > /tmp/citydiff-tour.log 2>&1 &
   # then open http://127.0.0.1:8787/
   ```

   Equivalent: `citydiff -path REPO -view -tour tour.json`. Either way the
   viewer shows the script's `range`, whatever `-range` says.
   A running viewer also plays `?tour=<url>` (fetched by the browser) or a
   script dropped onto the page. The server checks it the same way.

## Script

```json
{
  "version": 1,
  "title": "What this range does",
  "range": "dbdafc00..061e9aed",
  "steps": [
    { "title": "The whole change", "note": "Markdown **note**.", "mode": "changes", "camera": "overview" },
    { "title": "The generator", "select": "service/flashcard/generator" },
    { "title": "Handler to view", "path": { "from": "handler.handlePostSelectFinal", "to": "meetings/view.versionBar" } },
    { "title": "Picking the final", "focus": "review.Service.SelectFinal", "code": "review.Service.SelectFinal", "duration": 10 },
    { "title": "Storage", "highlight": ["db.Queries.SetGenerationFinal", "generator.FinalVersion"], "dim": true }
  ]
}
```

Top level: `version` (must be `1`), `title`, `range` (`base..head` as
for `-range`; abbreviated hashes and refs are fine, they are compared by
commit), `steps` (one or more).

Step fields (only `title` is required). Each step starts from a clean view;
nothing carries over from the step before except the mode.

| field | type | effect |
| --- | --- | --- |
| `title` | string | Roadmap entry and note heading. |
| `note` | markdown | Note panel text: paragraphs, `**bold**`, `*italic*`, `` `code` ``, fenced code, `-`/`1.` lists, `#` headings, http(s) links. No raw HTML. |
| `duration` | seconds | How long autoplay stays on the step. Default 8. |
| `mode` | `"changes"` \| `"full"` | `changes` colours the diff and draws dependency arcs; `full` is the plain city. Sticks until a later step changes it. |
| `select` | package name | Selects a package (or external import) and flies to it; its changed calls or dependencies are drawn as arcs. |
| `focus` | any name | A function or method opens its call focus (callees fanned out); a type or variable is selected; a package is selected. Not with `select`. |
| `highlight` | names | Lights these nodes and shades the rest; calls between them are drawn as arcs. |
| `path` | `{from, to}` | Lights the shortest resolved call path between two functions/methods, with an arc per hop. |
| `dim` | bool | With `highlight`/`path`: `false` keeps the rest of the city bright. Default `true`. |
| `camera` | `"overview"` \| `"top"` \| `"fit"` \| `"close"` | `overview` frames the city, `top` looks down on the step's nodes, `fit` frames them, `close` moves in. Default: the select/focus camera, else `fit` when the step names nodes, else `overview`. |
| `zoom` | number > 0 | Multiplies the camera distance after the camera move: `0.5` is twice as close, `1.5` farther. |
| `code` | declaration | Shows its source under the note: a line diff when it changed, the new or old body when added or removed. |

### Names

Names are matched against the scene the way you would write them:

- Package: its path, `barse/service/flashcard/generator`, or any unique tail, `flashcard/generator`, `generator`.
- Go declaration: `pkg/path.Func`, `pkg.Type.Method`, `Type.Method`, `Func`. Any unique tail of whole segments works.
- Rust: `crate::module::func`, `module::Type::method`, `<T as Trait>::m`, `<T as fmt::Display>::fmt`. A trait method is also found as `T::m` when nothing else matches.
- A scene id (`file#kind#Name` from `nodes -json`) always works.
- Case is ignored only if nothing matches exactly.

A name that matches several nodes is an error, never a guess: qualify it
with more segments (`gen.New` instead of `New`). `select` takes packages
only, `path` takes functions and methods only, and `code` takes
declarations only.

The JSON Schema is printed by `citydiff tour schema`, and lives at
https://raw.githubusercontent.com/naqerl/citydiff/main/lib/tour/tour.schema.json.
A full example over a real range is
https://github.com/naqerl/citydiff/blob/main/examples/barse-flashcard-versions.tour.json.

## Player

| key | action |
| --- | --- |
| `space` | play / pause (autoplay uses each step's duration) |
| `,` `.` | previous / next step |
| `t` | hide / show the tour sidebar (`b` does the left one) |
| `?` | the legend, with every key the viewer answers to |

Clicking a roadmap entry jumps to it. Dragging or clicking in the city
pauses autoplay. The × in the sidebar header closes the tour. Loading a
tour switches the viewer to the tour's range, so the left sidebar shows
that range's changes; a range that does not resolve is reported in the
tour sidebar.

## Tips

- Use `nodes -changed` for the cast, but a `path` may pass through unchanged
  functions; that is often the point.
- Calls through interfaces are not resolved, so a `path` can stop at an
  interface call. If validate says there is no path, point `path` at a
  concrete hop, or use `highlight` on both ends instead.
- Keep notes short (2–4 sentences). Put names in backticks.
- Validate after every edit. Validate exits 1 on any problem.
- `citydiff tour validate` is the sole source of truth. A tour it accepts is
  correct, so never re-check one in a browser, a screenshot, or by reading the
  viewer's JavaScript — `serve` is for watching a tour, not for validating it.
