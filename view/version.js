// The legend's header names the build the server runs, from GET /api/version:
// a release by its version, a development build by its commit. A newer
// release, once the server's startup check has found one, is a link to it.

export function versionLabel(info) {
  if (!info || !info.version) return null;
  let text = info.version;
  if (info.version === "dev") {
    text = info.commit ? "dev " + info.commit : "dev";
    if (info.dirty) text += " (modified)";
  }
  const update = info.update && info.update.version && info.update.url
    ? { text: info.update.version + " available", url: info.update.url }
    : null;
  return { text, update };
}

// showVersion writes the build into el. A server that cannot say leaves it
// empty: the legend is still the legend.
export async function showVersion(el) {
  let label = null;
  try {
    const res = await fetch("/api/version");
    if (res.ok) label = versionLabel(await res.json());
  } catch {
    // offline or an older server: no version to show
  }
  el.replaceChildren();
  if (!label) return;
  el.append(label.text);
  if (!label.update) return;
  const link = document.createElement("a");
  link.href = label.update.url;
  link.target = "_blank";
  link.rel = "noopener";
  link.textContent = label.update.text;
  el.append(" · ", link);
}
