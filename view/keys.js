// Every key the viewer answers to, in the order the legend lists them. The
// legend and the README are written from this list, and the tour's own keys
// are dispatched by tourAction below.

export const KEYBINDS = [
  { keys: ["/"], does: "search / commands" },
  { keys: ["wasd", "arrows"], does: "move" },
  { keys: ["q", "e"], does: "orbit" },
  { keys: ["-", "+"], does: "zoom" },
  { keys: ["b"], does: "left sidebar" },
  { keys: ["t"], does: "tour sidebar" },
  { keys: ["0"], does: "reset view" },
  { keys: ["m"], does: "overview / changes" },
  { keys: ["1"], does: "overview" },
  { keys: ["2"], does: "changes" },
  { keys: ["c"], does: "calls / callers" },
  { keys: ["drag"], does: "orbit" },
  { keys: ["scroll"], does: "zoom" },
  { keys: ["click"], does: "enters" },
  { keys: ["enter"], does: "opens a function" },
  { keys: ["o", "i"], does: "jump back / forward" },
  { keys: ["esc"], does: "back" },
  { keys: ["space"], does: "tour play / pause" },
  { keys: [",", "."], does: "tour previous / next" },
  { keys: ["?"], does: "legend" },
];

const TOUR = { " ": "toggle", ",": "prev", ".": "next", t: "sidebar", T: "sidebar" };

// tourAction maps a keydown to a tour action, or null. A key typed into a
// field, held with a modifier, or repeated by the keyboard is not an action;
// space may repeat-fire, so it never acts on a repeat either.
export function tourAction(event) {
  if (!event || event.metaKey || event.ctrlKey || event.altKey || event.repeat) return null;
  const target = event.target;
  if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable)) return null;
  return TOUR[event.key] || null;
}
