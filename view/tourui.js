// The tour sidebar on the right: title and range, the note, the code, and
// the player controls at the bottom. It mirrors the left sidebar: t hides
// and shows it like b does the left one. Everything here runs on a step
// change or a click, never per frame. The step itself is applied by the
// scene through apply().

import { createPlayer, reloadFor, rangeLabel } from "./tour.js";
import { tourAction } from "./keys.js";
import { renderMarkdown, lineDiff, escapeHTML } from "./markdown.js";

export function mountTour({ apply, clear, layout, open = true, hold = false }) {
  const root = document.getElementById("tour-side");
  const drop = document.getElementById("tour-drop");
  const el = (id) => document.getElementById(id);
  const ui = {
    scroll: el("tour-scroll"),
    stepTitle: el("tour-step-title"),
    body: el("tour-body"),
    code: el("tour-code"),
    title: el("tour-title"),
    range: el("tour-range"),
    play: el("tour-play"),
    counter: el("tour-counter"),
    // Held upright on a touch screen the player sits outside the sidebar.
    progress: document.querySelector("#tour-progress i"),
  };

  let tour = null;
  let player = null;
  const codeCache = new Map();
  let codeWant = null;

  function show(index, step) {
    ui.stepTitle.textContent = step.title;
    ui.body.innerHTML = renderMarkdown(step.note || "");
    ui.code.hidden = true;
    ui.code.innerHTML = "";
    codeWant = step.targets && step.targets.code ? step.targets.code.id : null;
    if (codeWant) loadCode(codeWant);
    // A step change starts at the top, so the new step's note and code read
    // from their first line; the player's counter and bar carry the position.
    ui.scroll.scrollTop = 0;
    apply(step);
  }

  function change(state) {
    ui.counter.textContent = `${state.index + 1} / ${state.count}`;
    ui.play.textContent = state.playing ? "❚❚" : "▶";
    root.classList.toggle("is-playing", state.playing);
    el("tour-prev").disabled = state.index <= 0;
    el("tour-next").disabled = state.index >= state.count - 1;
    // The progress bar is a CSS animation restarted per step: no frame work here.
    const bar$ = ui.progress;
    bar$.style.animation = "none";
    void bar$.offsetWidth;
    if (state.playing && state.index >= 0) {
      bar$.style.animation = `tour-progress ${Math.max(0.5, tour.steps[state.index].duration)}s linear forwards`;
    } else {
      bar$.style.width = state.ended ? "100%" : "0";
    }
  }

  async function loadCode(id) {
    let view = codeCache.get(id);
    if (!view) {
      try {
        const res = await fetch("./api/code?id=" + encodeURIComponent(id));
        if (!res.ok) return;
        view = await res.json();
        codeCache.set(id, view);
      } catch {
        return;
      }
    }
    if (codeWant !== id) return;
    const head = `<div class="tour-code-head">${escapeHTML(view.file)} <span class="change-${escapeHTML(view.change)}">${escapeHTML(view.change)}</span></div>`;
    let lines;
    if (view.before && view.after && view.before !== view.after) lines = lineDiff(view.before, view.after);
    else lines = (view.after || view.before || "").split("\n").map((text) => ({ op: view.after ? (view.before ? " " : "+") : "-", text }));
    if (view.change === "same") lines = lines.map((l) => ({ op: " ", text: l.text }));
    const body = lines
      .map((l) => `<span class="ln ln-${l.op === "+" ? "add" : l.op === "-" ? "del" : "same"}">${escapeHTML(l.op + " " + l.text)}</span>`)
      .join("");
    ui.code.innerHTML = head + `<pre>${body}</pre>`;
    ui.code.hidden = false;
  }

  // hold: the address bar already named a view, so the first tour comes up
  // without applying a step; play or next starts it.
  let holdFirst = hold;
  function start(resolved) {
    if (player) player.stop();
    tour = resolved;
    ui.title.textContent = resolved.title || "Tour";
    ui.title.title = resolved.title || "";
    const label = rangeLabel(resolved.range, resolved.scene && resolved.scene.range);
    ui.range.innerHTML = label ? `range <code>${escapeHTML(label)}</code>` : "no range: the plain tree";
    show$(true);
    player = createPlayer(resolved.steps, { show, change });
    if (holdFirst) change(player.state);
    else player.start();
    holdFirst = false;
  }

  function close() {
    if (player) player.stop();
    player = null;
    tour = null;
    show$(false);
    clear();
  }

  // show$ puts the sidebar up or takes it away, and tells the scene its
  // width changed so the camera re-centres in the free area.
  // The first tour opens the way the address bar left it (tourside=closed);
  // every later one opens.
  let startOpen = open;
  function show$(on) {
    root.hidden = !on;
    if (on) root.classList.toggle("is-collapsed", !startOpen);
    if (on) startOpen = true;
    layout();
  }

  function setOpen(open) {
    if (root.hidden) return;
    root.classList.toggle("is-collapsed", !open);
    layout();
  }

  function problems(list, source) {
    // Hidden before the layout runs, so a phone takes the player off its tabs.
    el("tour-controls").hidden = true;
    show$(true);
    ui.stepTitle.textContent = "This tour does not fit the scene";
    ui.body.innerHTML =
      `<p>${escapeHTML(source)}</p><ul>` +
      list.map((p) => `<li><code>${escapeHTML(p.step ? "step " + p.step + " " + p.field : p.field)}</code> ${escapeHTML(p.message)}</li>`).join("") +
      "</ul>";
    ui.code.hidden = true;
    ui.title.textContent = "Tour";
    ui.range.textContent = "";
  }

  // The server resolves every name against the scene it serves, so a
  // script from a URL or a drop gets the same checks as the CLI.
  async function loadText(text, source) {
    let res;
    try {
      res = await fetch("./api/tour", { method: "POST", headers: { "Content-Type": "application/json" }, body: text });
    } catch (err) {
      problems([{ field: "script", message: err.message }], source);
      return false;
    }
    const doc = await res.json().catch(() => ({}));
    if (!res.ok) {
      problems(doc.problems || [{ field: "script", message: res.statusText }], source);
      return false;
    }
    // A tour for another range moved the server's scene: reload to draw it.
    const next = reloadFor(doc, location.href);
    if (next) {
      problems([], "Loading the tour's range " + (doc.scene.range || "") + "…");
      ui.stepTitle.textContent = "Switching the scene";
      location.replace(next);
      return true;
    }
    el("tour-controls").hidden = false;
    start(doc);
    return true;
  }

  async function loadURL(url) {
    try {
      const res = await fetch(url);
      if (!res.ok) {
        if (url === "./tour.json") return false;
        throw new Error(res.status + " " + res.statusText);
      }
      return loadText(await res.text(), url);
    } catch (err) {
      problems([{ field: "script", message: "cannot read " + url + ": " + err.message }], url);
      return false;
    }
  }

  el("tour-prev").addEventListener("click", () => player && player.prev());
  el("tour-next").addEventListener("click", () => player && player.next());
  el("tour-play").addEventListener("click", () => player && player.toggle());
  el("tour-close").addEventListener("click", close);

  // A mouse folds the tour on the way past the arrow. A tap fires an
  // emulated mouseenter too, so only a real mouse counts here.
  el("tour-toggle").addEventListener("pointerenter", (event) => {
    if (event.pointerType === "mouse") setOpen(false);
  });
  // Folded into a phone's bottom sheet the arrow stays on screen, and opens it.
  el("tour-toggle").addEventListener("click", () => setOpen(root.classList.contains("is-collapsed")));
  el("tour-logo").addEventListener("click", () => setOpen(true));

  // Capture phase, ahead of the scene's own keys on the window.
  window.addEventListener("keydown", (event) => {
    if (!player) return;
    const action = tourAction(event);
    if (!action) return;
    if (action === "toggle") player.toggle();
    else if (action === "next") player.next();
    else if (action === "prev") player.prev();
    else if (action === "sidebar") setOpen(root.classList.contains("is-collapsed"));
    event.preventDefault();
    event.stopImmediatePropagation();
  }, true);

  let dragDepth = 0;
  window.addEventListener("dragenter", (event) => {
    if (![...(event.dataTransfer && event.dataTransfer.types) || []].includes("Files")) return;
    dragDepth++;
    drop.hidden = false;
  });
  window.addEventListener("dragleave", () => {
    dragDepth = Math.max(0, dragDepth - 1);
    if (!dragDepth) drop.hidden = true;
  });
  window.addEventListener("dragover", (event) => event.preventDefault());
  window.addEventListener("drop", async (event) => {
    event.preventDefault();
    dragDepth = 0;
    drop.hidden = true;
    const file = event.dataTransfer && event.dataTransfer.files[0];
    if (file) loadText(await file.text(), file.name);
  });

  return {
    loadURL,
    loadText,
    close,
    setOpen,
    // A drag or click in the city takes over from autoplay.
    userTookOver() {
      if (player && player.state.playing) player.pause();
    },
    get active() {
      return !!player;
    },
  };
}
