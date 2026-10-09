// The tour panels: the note in the top-right corner, the roadmap under it
// and the player bar. Everything here runs on a step change or a click,
// never per frame. The step itself is applied by the scene through apply().

import { createPlayer } from "./tour.js";
import { renderMarkdown, lineDiff, escapeHTML } from "./markdown.js";

export function mountTour({ apply, clear }) {
  const root = document.createElement("div");
  root.id = "tour";
  root.hidden = true;
  root.innerHTML = `
    <section class="tour-card" id="tour-note">
      <header><span id="tour-count"></span><h2 id="tour-step-title"></h2></header>
      <div id="tour-body"></div>
      <div id="tour-code" hidden></div>
    </section>
    <section class="tour-card" id="tour-road">
      <header><h3 id="tour-title"></h3><button type="button" id="tour-close" title="Close the tour">×</button></header>
      <ol id="tour-steps"></ol>
    </section>`;
  const bar = document.createElement("div");
  bar.id = "tour-bar";
  bar.hidden = true;
  bar.innerHTML = `
    <button type="button" id="tour-prev" title="Previous step (←)">‹</button>
    <button type="button" id="tour-play" title="Play or pause (space)">▶</button>
    <button type="button" id="tour-next" title="Next step (→)">›</button>
    <span id="tour-counter"></span>
    <div id="tour-progress"><i></i></div>`;
  const drop = document.createElement("div");
  drop.id = "tour-drop";
  drop.hidden = true;
  drop.textContent = "Drop a tour script";
  document.body.append(root, bar, drop);

  const el = (id) => document.getElementById(id);
  const ui = {
    count: el("tour-count"),
    stepTitle: el("tour-step-title"),
    body: el("tour-body"),
    code: el("tour-code"),
    title: el("tour-title"),
    steps: el("tour-steps"),
    play: el("tour-play"),
    counter: el("tour-counter"),
    progress: bar.querySelector("#tour-progress i"),
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
    bar.classList.toggle("is-playing", state.playing);
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
    root.hidden = false;
    bar.hidden = false;
    document.body.classList.add("has-tour");
    player = createPlayer(resolved.steps, { show, change });
    player.start();
  }

  function close() {
    if (player) player.stop();
    player = null;
    tour = null;
    root.hidden = true;
    bar.hidden = true;
    document.body.classList.remove("has-tour");
    clear();
  }

  function problems(list, source) {
    root.hidden = false;
    bar.hidden = true;
    ui.count.textContent = "";
    ui.stepTitle.textContent = "This tour does not fit the scene";
    ui.body.innerHTML =
      `<p>${escapeHTML(source)}</p><ul>` +
      list.map((p) => `<li><code>${escapeHTML(p.step ? "step " + p.step + " " + p.field : p.field)}</code> ${escapeHTML(p.message)}</li>`).join("") +
      "</ul>";
    ui.code.hidden = true;
    ui.title.textContent = "Tour";
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

  const typing = (target) => target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable);
  // Capture phase: these keys belong to the tour while it is open, ahead of
  // the fly keys bound on the window.
  window.addEventListener("keydown", (event) => {
    if (!player || typing(event.target) || event.metaKey || event.ctrlKey || event.altKey) return;
    let used = true;
    if (event.key === " ") player.toggle();
    else if (event.key === "ArrowRight") player.next();
    else if (event.key === "ArrowLeft") player.prev();
    else used = false;
    if (used) {
      event.preventDefault();
      event.stopImmediatePropagation();
    }
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
