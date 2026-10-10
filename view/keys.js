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
  { keys: ["E"], does: "open in nvim" },
  { keys: ["D"], does: "diff of the change" },
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

// editAction is true for the key that opens the selection in nvim: a capital
// E, so the e of orbit keeps its meaning. Typing, modifiers and repeats are
// not it.
export function editAction(event) {
  return capital(event, "E");
}

// diffAction is true for the key that shows the selection's git diff: a
// capital D, for the same reason E is a capital.
export function diffAction(event) {
  return capital(event, "D");
}

// capital is true when the event is that capital letter and nothing else: not
// a modifier combination, not a repeat, and not typed into a field.
function capital(event, letter) {
  if (!event || event.metaKey || event.ctrlKey || event.altKey || event.repeat) return false;
  const target = event.target;
  if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable)) return false;
  return event.key === letter;
}
