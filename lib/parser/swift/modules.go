package swift

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// manifest is one Package.swift: its directory and its targets.
type manifest struct {
	dir     string
	name    string
	targets []target
}

// target is one SwiftPM target: its module name and source directory,
// relative to the package.
type target struct {
	name string
	dir  string
}

var (
	targetCall = regexp.MustCompile(`\.(target|executableTarget|testTarget|macro|plugin|systemLibrary|binaryTarget)\s*\(`)
	nameArg    = regexp.MustCompile(`\bname\s*:\s*"([^"]+)"`)
	pathArg    = regexp.MustCompile(`\bpath\s*:\s*"([^"]*)"`)
	pkgName    = regexp.MustCompile(`\bPackage\s*\(\s*name\s*:\s*"([^"]+)"`)
)

// readManifest reads the targets of a Package.swift without running it:
// each .target(...) call's name and path arguments. A target without a path
// lives in Sources/<name>, a test target in Tests/<name> and a plugin in
// Plugins/<name>, as SwiftPM lays them out.
func readManifest(dir string, src []byte) manifest {
	m := manifest{dir: dir}
	text := stripComments(string(src))
	if n := pkgName.FindStringSubmatch(text); n != nil {
		m.name = n[1]
	}
	for _, loc := range targetCall.FindAllStringSubmatchIndex(text, -1) {
		kind := text[loc[2]:loc[3]]
		args := balanced(text[loc[1]:])
		name := nameArg.FindStringSubmatch(args)
		if name == nil {
			continue
		}
		t := target{name: name[1]}
		if p := topLevelPath(args); p != "" {
			t.dir = path.Clean(p)
		} else {
			switch kind {
			case "testTarget":
				t.dir = "Tests/" + t.name
			case "plugin":
				t.dir = "Plugins/" + t.name
			default:
				t.dir = "Sources/" + t.name
			}
		}
		m.targets = append(m.targets, t)
	}
	return m
}

// topLevelPath is the path argument of the target itself, not one nested
// in a dependency or resource list.
func topLevelPath(args string) string {
	depth := 0
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case 'p':
			if depth == 0 {
				if m := pathArg.FindStringSubmatchIndex(args[i:]); m != nil && m[0] == 0 && (i == 0 || !isWord(args[i-1])) {
					return args[i+m[2] : i+m[3]]
				}
			}
		case '"':
			for i++; i < len(args) && args[i] != '"'; i++ {
			}
		}
	}
	return ""
}

func isWord(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// balanced is the text up to the parenthesis that closes the one just opened.
func balanced(s string) string {
	depth := 1
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[:i]
			}
		case '"':
			for i++; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' {
					i++
				}
			}
		}
	}
	return s
}

func stripComments(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '"':
			j := i + 1
			for j < len(s) && s[j] != '"' && s[j] != '\n' {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(s) {
				j = len(s) - 1
			}
			b.WriteString(s[i : j+1])
			i = j
		case strings.HasPrefix(s[i:], "//"):
			for i < len(s) && s[i] != '\n' {
				i++
			}
			b.WriteByte('\n')
		case strings.HasPrefix(s[i:], "/*"):
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return b.String()
			}
			i += end + 3
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// assignModules puts each file in a module. A file under a target's
// directory of the nearest Package.swift belongs to that target. Any other
// file falls back to Xcode-style folders: Sources/<X>/ and Tests/<X>/ name
// module X, otherwise the top folder does, and a file at the root is in
// module "main". ns tells apart same-named modules of different packages.
func assignModules(drafts []*draft, manifests []manifest) {
	sort.SliceStable(manifests, func(i, j int) bool { return len(manifests[i].dir) > len(manifests[j].dir) })
	for _, d := range drafts {
		dir := path.Dir(d.path)
		var pkg *manifest
		for i := range manifests {
			m := &manifests[i]
			if m.dir == "." || dir == m.dir || strings.HasPrefix(dir, m.dir+"/") {
				pkg = m
				break
			}
		}
		root := "."
		rel := d.path
		if pkg != nil {
			root = pkg.dir
			d.pkgName = pkg.name
			if root != "." {
				rel = strings.TrimPrefix(d.path, root+"/")
			}
			best := -1
			for i, t := range pkg.targets {
				if t.dir == "." || strings.HasPrefix(rel, t.dir+"/") {
					if best < 0 || len(t.dir) > len(pkg.targets[best].dir) {
						best = i
					}
				}
			}
			if best >= 0 {
				d.module = pkg.targets[best].name
			}
		}
		if d.module == "" {
			d.module = folderModule(rel)
		}
		d.ns = root + "\x00" + d.module
		d.importPath = d.module
		if root != "." {
			d.importPath = root + "/" + d.module
		}
	}
}

func folderModule(rel string) string {
	segs := strings.Split(rel, "/")
	if len(segs) >= 3 && (segs[0] == "Sources" || segs[0] == "Tests") {
		return segs[1]
	}
	if len(segs) >= 2 {
		return segs[0]
	}
	return "main"
}
