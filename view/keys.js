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
  { keys: ["0"], does: "reset camera" },
  { keys: ["y"], does: "bird view" },
  { keys: ["m"], does: "overview / changes" },
  { keys: ["1"], does: "overview" },
  { keys: ["2"], does: "changes" },
  { keys: ["c"], does: "calls / callers" },
  { keys: ["drag"], does: "orbit" },
  { keys: ["scroll"], does: "zoom" },
  { keys: ["click"], does: "enters" },
  { keys: ["right hold"], does: "action ring" },
  { keys: ["enter"], does: "opens a function" },
  { keys: ["E"], does: "open in nvim" },
  { keys: ["D"], does: "diff of the change" },
  { keys: ["o", "i"], does: "jump back / forward" },
  { keys: ["esc"], does: "deselect, back to the city" },
  { keys: ["space"], does: "tour play / pause" },
  { keys: [",", "."], does: "tour previous / next" },
  { keys: ["?"], does: "legend" },
];

// The action ring holds the keys nothing on screen stands in for: no button,
// no gesture. The right button held on the city opens it, and on a touch
// screen a held sheet grip does, so a mouse or a finger reaches every one.
// The order is clockwise from the top, and pairs face each other across the
// ring: the two that open the node, back and forward, the bird view and
// Escape.
export const RING = [
  { id: "reset", key: "0", label: "reset" },
  { id: "diff", key: "D", label: "diff" },
  { id: "forward", key: "i", label: "forward" },
  { id: "bird", key: "y", label: "bird" },
  { id: "deselect", key: "esc", label: "deselect" },
  { id: "back", key: "o", label: "back" },
  { id: "edit", key: "E", label: "nvim" },
];

// On a touch screen the top of the ring is Cancel, the way out without
// running anything, and the rest keep their pairs across the ring with reset
// at the bottom, opposite Cancel.
export const RING_TOUCH = ["cancel", "diff", "forward", "bird", "reset", "deselect", "back", "edit"];

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

// birdAction is true for the bird-view toggle. Typing, modifiers and
// repeats are not it.
export function birdAction(event) {
  if (!event || event.metaKey || event.ctrlKey || event.altKey || event.repeat) return false;
  const target = event.target;
  if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable)) return false;
  return event.key === "y" || event.key === "Y";
}
