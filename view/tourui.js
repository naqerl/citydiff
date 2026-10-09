// The tour sidebar on the right: title and range, the note, the code, the
// roadmap, and the player controls at the bottom. It mirrors the left
// sidebar: t hides and shows it like b does the left one. Everything here
// runs on a step change or a click,
// never per frame. The step itself is applied by the scene through apply().

import { createPlayer, reloadFor, rangeLabel } from "./tour.js";
import { tourAction } from "./keys.js";
import { renderMarkdown, lineDiff, escapeHTML } from "./markdown.js";

export function mountTour({ apply, clear, layout }) {
  const root = document.getElementById("tour-side");
  const drop = document.getElementById("tour-drop");
  const el = (id) => document.getElementById(id);
  const ui = {
    count: el("tour-count"),
    stepTitle: el("tour-step-title"),
    body: el("tour-body"),
    code: el("tour-code"),
    title: el("tour-title"),
    range: el("tour-range"),
    steps: el("tour-steps"),
    play: el("tour-play"),
    counter: el("tour-counter"),
    progress: root.querySelector("#tour-progress i"),
  };

  let tour = null;
  let player = null;
  const codeCache = new Map();
  let codeWant = null;

  function show(index, step) {
    const total = tour.steps.length;
    ui.count.textContent = `${index + 1} / ${total}`;
    ui.stepTitle.textContent = step.title;
    ui.body.innerHTML = renderMarkdown(step.note || "");
    ui.code.hidden = true;
    ui.code.innerHTML = "";
    codeWant = step.targets && step.targets.code ? step.targets.code.id : null;
    if (codeWant) loadCode(codeWant);
    for (const [i, li] of [...ui.steps.children].entries()) {
      li.classList.toggle("is-current", i === index);
      li.classList.toggle("is-done", i < index);
    }
    const current = ui.steps.children[index];
    if (current) current.scrollIntoView({ block: "nearest" });
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

  function start(resolved) {
    if (player) player.stop();
    tour = resolved;
    ui.title.textContent = resolved.title || "Tour";
    ui.title.title = resolved.title || "";
    const label = rangeLabel(resolved.range, resolved.scene && resolved.scene.range);
    ui.range.innerHTML = label ? `range <code>${escapeHTML(label)}</code>` : "no range: the plain tree";
    ui.steps.innerHTML = "";
    resolved.steps.forEach((step, i) => {
      const li = document.createElement("li");
      const btn = document.createElement("button");
      btn.type = "button";
      btn.textContent = step.title;
      btn.addEventListener("click", () => player.jump(i));
      li.append(btn);
      ui.steps.append(li);
    });
    show$(true);
    player = createPlayer(resolved.steps, { show, change });
    player.start();
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
  function show$(on) {
    root.hidden = !on;
    if (on) root.classList.remove("is-collapsed");
    layout();
  }

  function setOpen(open) {
    if (root.hidden) return;
    root.classList.toggle("is-collapsed", !open);
    layout();
  }

  function problems(list, source) {
    show$(true);
    el("tour-controls").hidden = true;
    ui.count.textContent = "";
    ui.stepTitle.textContent = "This tour does not fit the scene";
    ui.body.innerHTML =
      `<p>${escapeHTML(source)}</p><ul>` +
      list.map((p) => `<li><code>${escapeHTML(p.step ? "step " + p.step + " " + p.field : p.field)}</code> ${escapeHTML(p.message)}</li>`).join("") +
      "</ul>";
    ui.code.hidden = true;
    ui.title.textContent = "Tour";
    ui.range.textContent = "";
    ui.steps.innerHTML = "";
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

  el("tour-toggle").addEventListener("mouseenter", () => setOpen(false));
  el("tour-toggle").addEventListener("click", () => setOpen(false));
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
    // A drag or click in the city takes over from autoplay.
    userTookOver() {
      if (player && player.state.playing) player.pause();
    },
    get active() {
      return !!player;
    },
  };
}
