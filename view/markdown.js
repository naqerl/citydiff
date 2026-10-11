// A small Markdown renderer for tour notes. Every character of the source
// is escaped first, so a note can never inject markup: only the constructs
// below become tags, and links accept http(s) and relative targets only.

const ESC = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };

export function escapeHTML(text) {
  return String(text).replace(/[&<>"']/g, (c) => ESC[c]);
}

function inline(text) {
  const codes = [];
  let out = escapeHTML(text).replace(/`([^`]+)`/g, (_, code) => {
    codes.push(code);
    return "\u0000" + (codes.length - 1) + "\u0000";
  });
  out = out
    .replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (all, label, href) => {
      const raw = href.replace(/&amp;/g, "&");
      if (!/^(https?:\/\/|\/|\.\/|#)/i.test(raw)) return label;
      return `<a href="${href}" target="_blank" rel="noopener noreferrer">${label}</a>`;
    })
    .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
    .replace(/(^|[^*\w])\*([^*\s][^*]*)\*/g, "$1<em>$2</em>")
    .replace(/(^|[^_\w])_([^_\s][^_]*)_(?!\w)/g, "$1<em>$2</em>");
  return out.replace(/\u0000(\d+)\u0000/g, (_, i) => `<code>${codes[Number(i)]}</code>`);
}

export function renderMarkdown(source) {
  const lines = String(source || "").replace(/\r\n?/g, "\n").split("\n");
  const out = [];
  let para = [];
  let list = null;
  const flushPara = () => {
    if (para.length) out.push(`<p>${inline(para.join(" "))}</p>`);
    para = [];
  };
  const flushList = () => {
    if (list) out.push(`<${list.tag}>${list.items.map((item) => `<li>${inline(item)}</li>`).join("")}</${list.tag}>`);
    list = null;
  };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const fence = line.match(/^```\s*([\w-]*)\s*$/);
    if (fence) {
      flushPara();
      flushList();
      const body = [];
      for (i++; i < lines.length && !/^```\s*$/.test(lines[i]); i++) body.push(lines[i]);
      const cls = fence[1] ? ` class="lang-${escapeHTML(fence[1])}"` : "";
      out.push(`<pre><code${cls}>${escapeHTML(body.join("\n"))}</code></pre>`);
      continue;
    }
    const heading = line.match(/^(#{1,4})\s+(.*)$/);
    if (heading) {
      flushPara();
      flushList();
      const level = Math.min(6, heading[1].length + 2);
      out.push(`<h${level}>${inline(heading[2])}</h${level}>`);
      continue;
    }
    const item = line.match(/^\s*([-*+]|\d+[.)])\s+(.*)$/);
    if (item) {
      flushPara();
      const tag = /\d/.test(item[1]) ? "ol" : "ul";
      if (!list || list.tag !== tag) {
        flushList();
        list = { tag, items: [] };
      }
      list.items.push(item[2]);
      continue;
    }
    if (!line.trim()) {
      flushPara();
      flushList();
      continue;
    }
    if (list && /^\s+\S/.test(line)) {
      list.items[list.items.length - 1] += " " + line.trim();
      continue;
    }
    flushList();
    para.push(line.trim());
  }
  flushPara();
  flushList();
  return out.join("");
}

