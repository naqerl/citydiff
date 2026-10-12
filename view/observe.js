// The observation hub's commands. A local viewer has no ./api/observe, so
// mountObserve adds nothing there. On a hub, /overview, /range and /tour
// write the address and reload: the range and the tour are the page's, and
// a link to that address opens the same city. /switch lists repos this
// browser has opened, kept in localStorage.

import { rangeLabel } from "./tour.js";

let observation = null;

// observationInfo is the hub's record of this page, or null on a local viewer.
export function observationInfo() {
  return observation;
}

// repoName is owner/repository from a remote URL. The host is not part of it.
export function repoName(remote) {
  let url;
  try {
    url = new URL(String(remote || ""));
  } catch {
    return "";
  }
  const parts = url.pathname.split("/").filter(Boolean);
  if (parts.length < 2) return "";
  const repo = parts[parts.length - 1].replace(/\.git$/i, "");
  const owner = parts[parts.length - 2];
  if (!owner || !repo) return "";
  return owner + "/" + repo;
}

// projectTitle is the sidebar's name: owner/repository on a hub, and the
// module name everywhere else.
export function projectTitle(remote, fallback) {
  return repoName(remote) || String(fallback || "").trim() || "project";
}

// shortRev is a commit as ten hex digits. A branch, a tag, or an already
// short name is left as it was typed.
export function shortRev(rev) {
  const s = String(rev || "").trim();
  return /^[0-9a-f]{12,}$/i.test(s) ? s.slice(0, 10) : s;
}

// shortRange shortens each full hash in a range. The dots, a branch name,
// and a note such as "(scene …)" stay.
export function shortRange(range) {
  return String(range || "").replace(/[0-9a-f]{12,}/gi, (hash) => hash.slice(0, 10));
}

// recentKey is the localStorage entry of repos opened on this hub. It is
// this browser's list, not the server's.
export const recentKey = "citydiff.hub.recent";

const recentLimit = 20;

// readRecent parses what localStorage holds. A broken value, and any entry
// without a remote and an address, is dropped. The order is kept, newest first.
export function readRecent(raw) {
  let parsed;
  try {
    parsed = JSON.parse(String(raw || ""));
  } catch {
    return [];
  }
  if (!Array.isArray(parsed)) return [];
  const seen = new Set();
  const out = [];
  for (const item of parsed) {
    const remote = String(item && item.remote || "").trim();
    const url = String(item && item.url || "").trim();
    if (!remote || !url || seen.has(remote)) continue;
    seen.add(remote);
    out.push({
      remote,
      name: String(item.name || repoName(remote) || remote).trim(),
      url,
      at: Number(item.at) || 0,
    });
    if (out.length >= recentLimit) break;
  }
  return out;
}

// rememberRepo puts this visit first. The same remote updates in place, so
// the list is repos, newest first, and never longer than recentLimit.
export function rememberRepo(list, visit) {
  const remote = String(visit && visit.remote || "").trim();
  const url = String(visit && visit.url || "").trim();
  const kept = readRecent(JSON.stringify(list));
  if (!remote || !url) return kept;
  return readRecent(JSON.stringify([
    { remote, name: repoName(remote) || remote, url, at: Number(visit.at) || Date.now() },
    ...kept,
  ]));
}

// entryMatches is true when every word of the query appears in the entry:
// its kind, its label, or its value. An empty query matches the whole list.
// A commit's value is the full hash, so a prefix past the ten digits on the
// label still hits, and "commit" keeps only the commits.
export function entryMatches(item, query) {
  const words = String(query || "").trim().toLowerCase().split(/\s+/).filter(Boolean);
  if (!words.length) return true;
  const hay = [item && item.kind, item && item.label, item && item.value].join(" ").toLowerCase();
  return words.every((word) => hay.includes(word));
}

// projectSubtitle is the one line under that name. A range, from the tour
// or the scene, wins. With no range an observation names its branch.
export function projectSubtitle(tourRange, sceneRange, head) {
  const range = rangeLabel(tourRange, sceneRange);
  if (range) return shortRange(range);
  return shortRev(head);
}

// pagePath is a URL next to this page. The hub serves each observation under
// /o/<id>/, and the local viewer serves the same files at /.
export function pagePath(name) {
  const path = location.pathname;
  const dir = path.endsWith("/") ? path : path.slice(0, path.lastIndexOf("/") + 1);
  return dir + name;
}

// tourRequest is what the page fetches for a tour= parameter. An http(s)
// value is a script somewhere else. Anything else is a tour stored on this
// observation.
export function tourRequest(tour) {
  if (!tour) return "";
  if (/^https?:\/\//i.test(tour)) return tour;
  return "./tour.json?tour=" + encodeURIComponent(tour);
}

// rangeSpec is a typed A..B or A...B. Anything else is not a range, and the
// command opens the picker instead.
export function rangeSpec(arg) {
  const s = String(arg || "").trim();
  const dots = s.includes("...") ? s.indexOf("...") : s.indexOf("..");
  if (dots <= 0) return "";
  const width = s.includes("...") ? 3 : 2;
  if (!s.slice(dots + width).trim()) return "";
  return s;
}

// shareURL sets range or tour and drops the other. which "" is the overview.
// The view's own parameters stay, so a link can name a node and a range.
export function shareURL(href, which, value) {
  const url = new URL(href);
  const params = new URLSearchParams(url.search);
  params.delete("range");
  params.delete("tour");
  const kept = [...params];
  if (which === "range" && value) kept.push(["range", value]);
  if (which === "tour" && value) kept.push(["tour", value]);
  const search = kept.length
    ? "?" + kept.map(([key, val]) => readable(key) + "=" + readable(val)).join("&")
    : "";
  return url.pathname + search + url.hash;
}

function readable(text) {
  return encodeURIComponent(text).replace(/%(3A|2F|40|2C)/gi, (all) => decodeURIComponent(all));
}

// mountObserve adds the hub commands when this page is an observation.
// It returns [] for a local viewer, and never throws: a missing hub is the
// ordinary case.
export async function mountObserve() {
  observation = null;
  let info = null;
  try {
    const res = await fetch("./api/observe");
    if (res.ok) info = await res.json();
  } catch { /* a local viewer has no such route */ }
  if (!info) return [];
  observation = info;
  saveRecent({ remote: info.remote, url: pagePath(""), at: Date.now() });
  const menu = document.querySelector("#observe-menu");
  if (!menu) return [];
  const title = document.querySelector("#observe-title");
  const note = document.querySelector("#observe-note");
  const find = document.querySelector("#observe-find");
  const list = document.querySelector("#observe-list");
  let all = [];
  let query = "";
  let cursor = 0;
  let onPick = () => {};

  const shown = () => all.filter((item) => entryMatches(item, query));
  const close = () => {
    menu.hidden = true;
    all = [];
    query = "";
    if (find) find.value = "";
    find?.blur();
  };
  const render = () => {
    const items = shown();
    if (cursor >= items.length) cursor = 0;
    list.replaceChildren();
    if (!items.length) {
      if (query.trim()) {
        const empty = document.createElement("p");
        empty.className = "empty";
        empty.textContent = "Nothing matches.";
        list.append(empty);
      }
      return;
    }
    items.forEach((item, index) => {
      const button = document.createElement("button");
      button.type = "button";
      if (index === cursor) button.classList.add("on");
      const kind = document.createElement("span");
      kind.className = "kind";
      kind.textContent = item.kind || "";
      const name = document.createElement("span");
      name.className = "name";
      name.textContent = item.label;
      button.append(kind, name);
      button.addEventListener("click", () => onPick(item));
      list.append(button);
    });
    const on = list.children[cursor];
    if (on) on.scrollIntoView({ block: "nearest" });
  };
  const move = (by) => {
    const items = shown();
    if (!items.length) return;
    cursor = Math.max(0, Math.min(items.length - 1, cursor + by));
    [...list.children].forEach((button, index) => button.classList.toggle("on", index === cursor));
    const on = list.children[cursor];
    if (on) on.scrollIntoView({ block: "nearest" });
  };
  const show = (heading, text, next, pick) => {
    all = next;
    onPick = pick;
    query = "";
    cursor = 0;
    title.textContent = heading;
    note.textContent = text;
    const finding = next.some((item) => item.kind === "repo")
      ? "Find a repo"
      : next.some((item) => item.kind === "tour")
        ? "Find a tour"
        : "Find a branch, tag, or commit";
    if (find) {
      find.value = "";
      find.placeholder = finding;
      find.setAttribute("aria-label", finding);
      find.hidden = next.length === 0;
    }
    menu.hidden = false;
    render();
    if (find && !find.hidden) find.focus();
  };
  find?.addEventListener("input", () => {
    query = find.value;
    cursor = 0;
    render();
  });
  // Keys typed into the box stay there. The city's own keys listen on the
  // window, and a "b" or a "/" must not leave this list.
  find?.addEventListener("keydown", (event) => event.stopPropagation());
  document.querySelector("#observe-cancel").addEventListener("click", close);
  menu.addEventListener("click", (event) => {
    if (event.target === menu) close();
  });
  window.addEventListener("keydown", (event) => {
    if (menu.hidden) return;
    if (event.key === "Escape") {
      close();
    } else if (event.key === "ArrowDown") {
      move(1);
    } else if (event.key === "ArrowUp") {
      move(-1);
    } else if (event.key === "Enter") {
      const item = shown()[cursor];
      if (item) onPick(item);
    } else if (find && all.length && event.target !== find && event.key.length === 1 && !event.metaKey && !event.ctrlKey && !event.altKey) {
      find.hidden = false;
      find.focus();
      find.value += event.key;
      query = find.value;
      cursor = 0;
      render();
    } else {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
  }, true);

  const go = (which, value) => location.assign(shareURL(location.href, which, value));

  async function openRange() {
    let refs = { branches: [], tags: [], commits: [] };
    try {
      const res = await fetch("./api/refs");
      if (res.ok) refs = await res.json();
    } catch { /* the panel says the list is empty */ }
    const choices = [];
    for (const name of refs.branches || []) choices.push({ kind: "branch", label: name, value: name });
    for (const name of refs.tags || []) choices.push({ kind: "tag", label: name, value: name });
    for (const commit of refs.commits || []) {
      const hash = commit.hash || "";
      choices.push({ kind: "commit", label: (shortRev(hash) + " " + (commit.subject || "")).trim(), value: hash });
    }
    if (!choices.length) {
      show("Range", "This clone has no refs to pick.", [], () => {});
      return;
    }
    show("Base", "The start of the range. Then the head.", choices, (base) => {
      show("Head", shortRev(base.value) + "..", choices, (head) => go("range", shortRange(base.value + ".." + head.value)));
    });
  }

  function openSwitch() {
    const list = loadRecent();
    if (!list.length) {
      show("Switch", "No repo has been opened in this browser yet.", [], () => {});
      return;
    }
    show("Switch", "Repos opened in this browser, newest first.", list.map((item) => ({
      kind: "repo",
      label: item.name || item.remote,
      value: item.remote,
      url: item.url,
    })), (item) => location.assign(item.url));
  }

  async function openTour() {
    let tours = [];
    try {
      const res = await fetch("./api/tours");
      if (res.ok) tours = await res.json();
    } catch { /* an empty list */ }
    if (!tours.length) {
      show("Tour", "No tour has been submitted.", [], () => {});
      return;
    }
    show("Tour", "A tour submitted for this observation.", tours.map((tour) => ({
      kind: "tour",
      label: (tour.title || tour.name) + (tour.range ? "  " + shortRange(tour.range) : ""),
      value: tour.name,
    })), (tour) => go("tour", tour.value));
  }

  return [
    { name: "/overview", does: "the whole tree", run: () => go("", "") },
    {
      name: "/range",
      does: "show a commit range",
      run: (arg) => {
        const spec = rangeSpec(arg);
        if (spec) go("range", shortRange(spec));
        else openRange();
      },
    },
    {
      name: "/tour",
      does: "play a submitted tour",
      run: (arg) => {
        const name = String(arg || "").trim();
        if (name) go("tour", name);
        else openTour();
      },
    },
    { name: "/switch", does: "open a repo opened here", run: () => openSwitch() },
  ];
}

function loadRecent() {
  try {
    return readRecent(localStorage.getItem(recentKey));
  } catch {
    return [];
  }
}

function saveRecent(visit) {
  try {
    localStorage.setItem(recentKey, JSON.stringify(rememberRepo(loadRecent(), visit)));
  } catch { /* a browser that refuses storage keeps no list */ }
}
