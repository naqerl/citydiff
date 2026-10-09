import { test } from "node:test";
import assert from "node:assert/strict";
import { renderMarkdown, lineDiff } from "./markdown.js";

test("markup in a note is escaped", () => {
  assert.equal(renderMarkdown("<img src=x onerror=alert(1)>"), "<p>&lt;img src=x onerror=alert(1)&gt;</p>");
});

test("inline constructs", () => {
  assert.equal(renderMarkdown("**b** *i* `a<b` [x](https://e.com)"),
    '<p><strong>b</strong> <em>i</em> <code>a&lt;b</code> <a href="https://e.com" target="_blank" rel="noopener noreferrer">x</a></p>');
});

test("unsafe link schemes keep only the label", () => {
  assert.equal(renderMarkdown("[x](javascript:alert`1`)"), "<p>x</p>");
});

test("paragraphs, lists, headings and fences", () => {
  const html = renderMarkdown("# Head\n\none\ntwo\n\n- a\n- b\n\n1. c\n\n```go\nif a < b {}\n```");
  assert.equal(html, "<h3>Head</h3><p>one two</p><ul><li>a</li><li>b</li></ul><ol><li>c</li></ol>" +
    '<pre><code class="lang-go">if a &lt; b {}</code></pre>');
});

test("lineDiff keeps common lines and marks the rest", () => {
  assert.deepEqual(lineDiff("a\nb\nc", "a\nx\nc").map((l) => l.op + l.text), [" a", "+x", "-b", " c"]);
  assert.deepEqual(lineDiff("", "a").map((l) => l.op + l.text), ["+a"]);
});
