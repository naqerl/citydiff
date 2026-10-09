package tour

import (
	"regexp"
	"strings"
)

// maxSnippetLines bounds one snippet so a huge function does not flood the note panel.
const maxSnippetLines = 80

// Snippet cuts the declaration of one node out of its file's source: from
// its first line to the brace or semicolon that closes it. Leading doc
// comments come along. It returns "" when the declaration is not found.
func Snippet(src []byte, n Node, recv string) string {
	name := n.Name
	if segs := split(n.Name); len(segs) > 0 {
		name = segs[len(segs)-1]
	}
	re := declPattern(n.Kind, name, recv, strings.HasSuffix(n.File, ".rs"))
	if re == nil {
		return ""
	}
	lines := strings.Split(string(src), "\n")
	start := -1
	for i, line := range lines {
		if re.MatchString(line) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	first := start
	for first > 0 {
		prev := strings.TrimSpace(lines[first-1])
		if !strings.HasPrefix(prev, "//") && !strings.HasPrefix(prev, "#[") {
			break
		}
		first--
	}
	end := closeOf(lines, start)
	truncated := false
	if end-first+1 > maxSnippetLines {
		end = first + maxSnippetLines - 1
		truncated = true
	}
	out := strings.Join(lines[first:end+1], "\n")
	if truncated {
		out += "\n…"
	}
	return out
}

func declPattern(kind, name, recv string, rust bool) *regexp.Regexp {
	q := regexp.QuoteMeta(name)
	if rust {
		switch kind {
		case KindFunction, KindMethod:
			return regexp.MustCompile(`\bfn\s+` + q + `\b`)
		case KindType:
			return regexp.MustCompile(`\b(struct|enum|trait|type|union)\s+` + q + `\b`)
		case KindVariable:
			return regexp.MustCompile(`\b(const|static)\s+` + q + `\b`)
		}
		return nil
	}
	switch kind {
	case KindFunction:
		return regexp.MustCompile(`^func\s+` + q + `\s*[\[(]`)
	case KindMethod:
		r := regexp.QuoteMeta(strings.TrimPrefix(recv, "*"))
		return regexp.MustCompile(`^func\s*\([^)]*\b\*?` + r + `\b[^)]*\)\s*` + q + `\s*[\[(]`)
	case KindType:
		return regexp.MustCompile(`^(type\s+|\s+)` + q + `\b(\s*\[[^\]]*\])?\s+(struct|interface|func|=|[A-Za-z*\[])`)
	case KindVariable:
		return regexp.MustCompile(`^((var|const)\s+|\s+)` + q + `\b`)
	}
	return nil
}

// closeOf is the line where the block opened at start closes. A
// declaration with no block ends on its own line.
func closeOf(lines []string, start int) int {
	depth := 0
	opened := false
	for i := start; i < len(lines); i++ {
		for _, c := range stripLiterals(lines[i]) {
			switch c {
			case '{', '(':
				depth++
				opened = true
			case '}', ')':
				depth--
			}
		}
		if opened && depth <= 0 {
			return i
		}
		if !opened && i > start {
			return i - 1
		}
	}
	return len(lines) - 1
}

// stripLiterals drops strings, runes and line comments so their braces do not count.
func stripLiterals(line string) string {
	var b strings.Builder
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == '\\' && quote != '`' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '`':
			quote = c
		case c == '\'' && i+2 < len(line) && (line[i+2] == '\'' || line[i+1] == '\\'):
			quote = c
		case c == '/' && i+1 < len(line) && line[i+1] == '/':
			return b.String()
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
