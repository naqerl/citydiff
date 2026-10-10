// highlight turns one line of source into HTML with the parts that are
// keywords, strings, comments, numbers, types and calls wrapped in spans, so
// a diff reads the way an editor draws it.
//
// It is a reader's highlighter, not a compiler's: one scan per line, carrying
// only the two things that outlive a line, a block comment and a raw string.
// The colours are not here. Every part is a class, and the classes are painted
// by the skin, so the diff belongs to the theme like everything else.

const KEYWORDS = {
  go: "break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var",
  rust: "as async await break const continue crate dyn else enum extern false fn for if impl in let loop match mod move mut pub ref return self static struct super trait true type unsafe use where while",
  swift: "actor as associatedtype async await break case catch class continue default defer deinit do else enum extension fallthrough false fileprivate for func guard if import in indirect init inout internal is lazy let nil open operator private protocol public repeat rethrows return self static struct subscript super switch throw throws true try typealias var where while",
};

const BUILTINS = {
  go: "any bool byte comparable complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr",
  rust: "bool char f32 f64 i8 i16 i32 i64 i128 isize str u8 u16 u32 u64 u128 usize",
  swift: "Any AnyObject Array Bool Character Dictionary Double Float Int Int8 Int16 Int32 Int64 Optional Set String UInt UInt8 UInt16 UInt32 UInt64 Void",
};

const sets = new Map();
function words(lang, table) {
  const key = lang + ":" + table;
  let set = sets.get(key);
  if (!set) {
    set = new Set((table[lang] || "").split(" "));
    sets.set(key, set);
  }
  return set;
}

function escape(text) {
  return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function span(kind, text) {
  return '<span class="t-' + kind + '">' + escape(text) + "</span>";
}

const IDENT = /[A-Za-z_][A-Za-z0-9_]*/y;

// highlighter returns a function that highlights lines of one language in
// order, so a block comment opened on one line stays a comment on the next.
export function highlighter(lang) {
  const keywords = words(lang, KEYWORDS);
  const builtins = words(lang, BUILTINS);
  const state = { block: false, raw: false };
  return (text) => highlight(text, lang, keywords, builtins, state);
}

function highlight(text, lang, keywords, builtins, state) {
  let out = "";
  let i = 0;
  while (i < text.length) {
    if (state.block) {
      const end = text.indexOf("*/", i);
      if (end < 0) return out + span("comment", text.slice(i));
      out += span("comment", text.slice(i, end + 2));
      state.block = false;
      i = end + 2;
      continue;
    }
    if (state.raw) {
      const end = text.indexOf("`", i);
      if (end < 0) return out + span("string", text.slice(i));
      out += span("string", text.slice(i, end + 1));
      state.raw = false;
      i = end + 1;
      continue;
    }
    const rest = text.slice(i);
    if (rest.startsWith("//")) return out + span("comment", rest);
    if (rest.startsWith("/*")) {
      const end = text.indexOf("*/", i + 2);
      if (end < 0) {
        state.block = true;
        return out + span("comment", rest);
      }
      out += span("comment", text.slice(i, end + 2));
      i = end + 2;
      continue;
    }
    const ch = text[i];
    if (ch === "`" && lang === "go") {
      const end = text.indexOf("`", i + 1);
      if (end < 0) {
        state.raw = true;
        return out + span("string", rest);
      }
      out += span("string", text.slice(i, end + 1));
      i = end + 1;
      continue;
    }
    // Rust raw strings: r"…", r#"…"# and friends, so the hash count matches.
    if (lang === "rust" && ch === "r" && /^r#*"/.test(rest)) {
      const hashes = rest.match(/^r(#*)"/)[1];
      const close = '"' + hashes;
      const end = text.indexOf(close, i + 2 + hashes.length);
      if (end < 0) {
        state.raw = true;
        return out + span("string", rest);
      }
      out += span("string", text.slice(i, end + close.length));
      i = end + close.length;
      continue;
    }
    if (ch === '"') {
      let j = i + 1;
      while (j < text.length) {
        if (text[j] === "\\") j += 2;
        else if (text[j] === '"') break;
        else j++;
      }
      const end = j < text.length ? j + 1 : text.length;
      out += span("string", text.slice(i, end));
      i = end;
      continue;
    }
    if (ch === "'") {
      // A rune in Go, a character in Rust and Swift — but a Rust lifetime is
      // an identifier, and one letter followed by a quote is not.
      const close = text.indexOf("'", i + 1);
      if (close > i && close - i <= 4 && lang !== "rust") {
        out += span("string", text.slice(i, close + 1));
        i = close + 1;
        continue;
      }
      if (close === i + 2 || (close > i && close - i === 3 && text[i + 1] === "\\")) {
        out += span("string", text.slice(i, close + 1));
        i = close + 1;
        continue;
      }
      out += escape(ch);
      i++;
      continue;
    }
    if (/[0-9]/.test(ch)) {
      const number = rest.match(/^(0[xXbBoO][0-9a-fA-F_]+|[0-9][0-9_]*(?:\.[0-9_]+)?(?:[eE][+-]?[0-9]+)?)[a-zA-Z0-9]*/);
      out += span("number", number[0]);
      i += number[0].length;
      continue;
    }
    IDENT.lastIndex = i;
    const word = IDENT.exec(text);
    if (word && word.index === i) {
      const name = word[0];
      const after = text.slice(i + name.length);
      if (keywords.has(name)) out += span("keyword", name);
      else if (builtins.has(name) || /^[A-Z]/.test(name)) out += span("type", name);
      else if (/^\s*\(/.test(after)) out += span("func", name);
      else out += escape(name);
      i += name.length;
      continue;
    }
    out += escape(ch);
    i++;
  }
  return out;
}
