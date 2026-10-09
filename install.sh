#!/usr/bin/env sh
# Install betterdiff (citydiff) from the latest GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/naqerl/citydiff/main/install.sh | sh
#
# Options (env or flags):
#   BIN_DIR=~/.local/bin        where the binary is installed
#   SKILLS_DIR=~/.agents/skills where the agent skill is written
#   AGENTS_MD=<file>            agent instructions file to point at the skill
#   TAG=v0.0.1                  install a specific tag instead of the latest
#   NO_PATH=1                   do not touch PATH
#   NO_SKILLS=1                 do not write the skill / AGENTS.md pointer
#   UNINSTALL=1                 remove binary, skill and AGENTS.md pointer
set -eu

REPO="${REPO:-naqerl/citydiff}"
BIN_NAME="${BIN_NAME:-betterdiff}"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
SKILL_NAME="${SKILL_NAME:-citydiff}"
SKILLS_DIR="${SKILLS_DIR:-$HOME/.agents/skills}"
AGENTS_MD="${AGENTS_MD:-}"
NO_PATH="${NO_PATH:-}"
NO_SKILLS="${NO_SKILLS:-}"
UNINSTALL="${UNINSTALL:-}"
TAG="${TAG:-}"
RELEASES_API="https://api.github.com/repos/$REPO/releases"

usage() {
  sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'
}

for arg in "$@"; do
  case "$arg" in
    -h|--help) usage; exit 0 ;;
    --uninstall) UNINSTALL=1 ;;
    --no-skills) NO_SKILLS=1 ;;
    --no-path) NO_PATH=1 ;;
    --tag=*) TAG="${arg#--tag=}" ;;
    --bin-dir=*) BIN_DIR="${arg#--bin-dir=}" ;;
    --skills-dir=*) SKILLS_DIR="${arg#--skills-dir=}" ;;
    --agents-md=*) AGENTS_MD="${arg#--agents-md=}" ;;
    *) echo "unknown option: $arg" >&2; usage >&2; exit 2 ;;
  esac
done

say()  { printf '%s\n' "$*"; }
warn() { printf '%s\n' "$*" >&2; }
die()  { printf 'install.sh: %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"; }
need curl

# ---------------------------------------------------------------- platform ---

platform() {
  os=$(uname -s) || die "cannot detect OS"
  arch=$(uname -m) || die "cannot detect arch"
  case "$os" in
    Linux)  os=linux ;;
    Darwin) os=darwin ;;
    *) die "unsupported OS: $os (build from source: git clone $REPO && cd citydiff && make vet && go build ./cmd/cli)" ;;
  esac
  case "$arch" in
    x86_64|amd64)  arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) die "unsupported arch: $arch" ;;
  esac
  printf '%s-%s' "$os" "$arch"
}

PLATFORM="$(platform)"

# ------------------------------------------------------------------ fetch ----

fetch() { curl -fsSL "$1"; }

# Latest release payload; falls back to the newest tag when no release exists yet.
release_json() {
  if [ -n "$TAG" ]; then
    fetch "$RELEASES_API/tags/$TAG" 2>/dev/null || {
      fetch "https://api.github.com/repos/$REPO/git/refs/tags/$TAG" >/dev/null
      die "no release published for tag $TAG yet — wait for the ci workflow to finish, or try without --tag"
    }
  else
    if ! fetch "$RELEASES_API/latest" >/dev/null 2>&1; then
      latest_tag=$(fetch "https://api.github.com/repos/$REPO/tags" \
        | sed -n 's/.*"name": *"\([^"]*\)".*/\1/p' | head -1)
      if [ -n "$latest_tag" ]; then
        die "no GitHub release published yet (newest tag is $latest_tag).
Create one with:
  git -C <clone of $REPO> tag $latest_tag && git push origin $latest_tag"
      fi
      die "cannot reach $RELEASES_API (offline?)"
    fi
    fetch "$RELEASES_API/latest"
  fi
}

json_field() {
  # json_field <field> ; prints the first value of a simple string field.
  sed -n "s/.*\"$1\": *\"\([^\"]*\)\".*/\1/p" | head -1
}

resolve_asset() {
  # The asset name embeds the version, which we only learn after parsing.
  json="$1"; suffix="$2"
  printf '%s' "$json" | tr ',' '\n' \
    | sed -n 's/.*"browser_download_url": *"\([^"]*\)".*/\1/p' \
    | grep -- "-${suffix}\.tar\.gz$" | head -1
}

# --------------------------------------------------------------- uninstall ---

uninstall() {
  say "uninstalling $BIN_NAME"
  if [ -e "$BIN_DIR/$BIN_NAME" ]; then rm -f "$BIN_DIR/$BIN_NAME"; say "  removed $BIN_DIR/$BIN_NAME"; fi
  if [ -d "$SKILLS_DIR/$SKILL_NAME" ]; then rm -rf "$SKILLS_DIR/$SKILL_NAME"; say "  removed $SKILLS_DIR/$SKILL_NAME"; fi
  for f in "$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.profile"; do
    if [ -f "$f" ] && grep -q "^# citydiff/bin\|$BIN_DIR$" "$f" 2>/dev/null; then
      say "  note: $f may still export PATH containing $BIN_DIR — edit by hand if wanted"
    fi
  done
  drop_pointer
  say "done"
}

drop_pointer() {
  [ -n "$AGENTS_MD" ] && [ -f "$AGENTS_MD" ] || return 0
  tmp="$AGENTS_MD.citydiff.$$"
  awk '/^<!-- citydiff:begin -->$/ {skip=1} !skip {print} /^<!-- citydiff:end -->$/ {skip=0}' \
    "$AGENTS_MD" > "$tmp" || return 0
  # drop the blank line the block left behind, then replace atomically
  cat "$tmp" > "$AGENTS_MD" && rm -f "$tmp"
}

if [ -n "$UNINSTALL" ]; then uninstall; exit 0; fi

# -------------------------------------------------------------- install ------

say "installing $BIN_NAME ($REPO) for $PLATFORM"
say "  bin dir:    $BIN_DIR"
say "  skills dir: $SKILLS_DIR"

json="$(release_json)"
version="$(printf '%s' "$json" | json_field tag_name)"
tag="${TAG:-${version}}"
asset_url="$(resolve_asset "$json" "$PLATFORM")"
[ -n "$asset_url" ] || die "release $version has no asset for $PLATFORM (expected *-$PLATFORM.tar.gz)"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT INT TERM

say "  release: $version"
say "  fetching $asset_url"
curl -fsSL -o "$tmpdir/pkg.tar.gz" "$asset_url"

# Verify the sha256 the release publishes, when it does.
checksums_url="$(printf '%s' "$json" | tr ',' '\n' \
  | sed -n 's/.*"browser_download_url": *"\([^"]*\)".*/\1/p' \
  | grep '/checksums\.txt$' | head -1)"
if [ -n "$checksums_url" ] && curl -fsSL -o "$tmpdir/checksums.txt" "$checksums_url" 2>/dev/null; then
  pkg="$(basename "$asset_url")"
  want="$(awk -v p="$pkg" '$2 == p || $2 == "*"p {print $1}' "$tmpdir/checksums.txt" | head -1)"
  if [ -n "$want" ]; then
    got="$(sha256sum "$tmpdir/pkg.tar.gz" 2>/dev/null | awk '{print $1}' \
          || shasum -a 256 "$tmpdir/pkg.tar.gz" | awk '{print $1}')"
    [ "$want" = "$got" ] || die "checksum mismatch for $pkg"
    say "  checksum ok"
  fi
fi

tar xzf "$tmpdir/pkg.tar.gz" -C "$tmpdir"
[ -f "$tmpdir/$BIN_NAME" ] || die "archive did not contain $BIN_NAME"
chmod +x "$tmpdir/$BIN_NAME"
"$tmpdir/$BIN_NAME" -h >/dev/null 2>&1 || warn "warning: $BIN_NAME -h did not succeed"

mkdir -p "$BIN_DIR"
if mv "$tmpdir/$BIN_NAME" "$BIN_DIR/$BIN_NAME"; then
  say "  installed $BIN_DIR/$BIN_NAME"
else
  cp "$tmpdir/$BIN_NAME" "$BIN_DIR/$BIN_NAME" 2>/dev/null \
    || die "cannot write to $BIN_DIR (permissions?) — retry with --bin-dir=<writable path>"
  chmod +x "$BIN_DIR/$BIN_NAME" 2>/dev/null || true
  say "  installed $BIN_DIR/$BIN_NAME (copied)"
fi

# ------------------------------------------------------------------ PATH -----

in_path() {
  case ":$PATH:" in *":$BIN_DIR:"*) return 0 ;; esac
  # also accept a PATH entry that is literally this dir without trailing slash
  [ "$(printf '%s' "$PATH" | tr ':' '\n' | sed 's#/$##')" = "$(printf '%s' "$BIN_DIR" | sed 's#/$##')" ] && return 0
  return 1
}

on_path=0
in_path && on_path=1

if [ -z "$NO_PATH" ] && [ "$on_path" -eq 0 ]; then
  # $HOME/bin is on the default PATH of most shells, so prefer it when present.
  target="$HOME/.local/bin"
  rc=""
  for f in "$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.profile"; do [ -f "$f" ] && rc="$f" && break; done
  if [ -n "$rc" ] && [ -w "$rc" ]; then
    printf '\n# citydiff/bin\n[ -d "%s" ] && PATH="%s:$PATH"\n' "$target" "$target" >> "$rc"
    say "  added $target to PATH in $rc"
  else
    warn "  $BIN_DIR is not on PATH; add it yourself:"
    warn "    export PATH=\"$BIN_DIR:\$PATH\""
  fi
elif [ -n "$NO_PATH" ]; then
  say "  PATH untouched (--no-path)"
else
  say "  $BIN_DIR already on PATH"
fi

# ---------------------------------------------------------------- skills -----

write_skill() {
  skill_dir="$SKILLS_DIR/$SKILL_NAME"
  mkdir -p "$skill_dir"
  cat > "$skill_dir/SKILL.md" <<EOF
---
name: $SKILL_NAME
description: 3D structural diff of Go code ("betterdiff") — cross-module dependency changes, declaration/entity edges and call-path changes across a commit range. Use when asked what a commit or range changed structurally, who calls what, or how a call path changed.
---

# $SKILL_NAME — betterdiff, a 3D code diff

A diff viewed from the outside inward, at three levels:

1. **Cross-module dependencies** — which modules gained/lost a dependency on which.
2. **Entity relationships** — which declarations depend on which, and how those edges changed.
3. **Call paths** — inside a function/method, which functions it calls, in order, and how that changed.

The three levels are views of one diff.

## Source of truth

| | |
| --- | --- |
| Repository | https://github.com/$REPO |
| Release | https://github.com/$REPO/releases (tags \`v*\`) |
| Installer | https://raw.githubusercontent.com/$REPO/main/install.sh |
| Module | \`betterdiff\`, Go 1.27, **requires CGO** (tree-sitter C bindings) |
| Installed as | \`$BIN_DIR/$BIN_NAME\` (release installed here on $(date -u +%Y-%m-%d)) |
| Release used | $version |

For any question about behaviour, internals or bugs: **the answer lives in the repository
above, not in this file.** Read \`AGENTS.md\` in the repo for the design intent, \`lib/parser\`
for how declarations and calls are recorded, \`lib/diff\` for how two parses are compared,
\`cmd/cli/main.go\` for flags, and \`view/\` for the browser UI. When this file and the code
disagree, the code wins — and this file should be updated.

## Install / update

\`\`\`sh
curl -fsSL https://raw.githubusercontent.com/$REPO/main/install.sh | sh          # latest release
curl -fsSL https://raw.githubusercontent.com/$REPO/main/install.sh | sh -s -- --tag=v0.0.1
TAG=v0.0.2 curl -fsSL https://raw.githubusercontent.com/$REPO/main/install.sh | sh
UNINSTALL=1 curl -fsSL https://raw.githubusercontent.com/$REPO/main/install.sh | sh   # remove
\`\`\`

Re-running the installer upgrades in place: it resolves the newest release, verifies the
published sha256, replaces the binary, and rewrites this skill file.

## Flags

\`\`\`
$BIN_NAME -path FILE|DIR              parse a file or directory tree
           -range A..B | A...B         diff two refs; works with git repos (reads the tree at a ref)
           -json                       machine-readable entries (pipe into jq)
           -scene                      print the 3D scene as JSON (input for the viewer)
           -view                       serve the 3D viewer over HTTP
           -addr 127.0.0.1:8787        listen address for -view (default)
           -p, -r                      short forms of -path, -range
\`\`\`

Note: \`-view\` is **long-running** — always launch it in the background (see below).
Note: for a commit range the path must be a **git repository**, not a plain directory;
go-git reads the trees at the two refs, so the working tree does not need to be checked out.

## Recipes

Parse a single file or a project tree (one-shot, prints JSON):

\`\`\`sh
$BIN_NAME -path /home/user/src/barse -json | jq '.[] | {path, entries: (.entries | length)}'
\`\`\`

Diff a commit range — the main use:

\`\`\`sh
cd /home/user/src/barse
$BIN_NAME -path . -range 6ca8b06..3aff57d -json > /tmp/diff.json
jq '.[] | {path, entries: [.entries[] | {name, kind, calls}]}' /tmp/diff.json
\`\`\`

Diff the last N commits of the current branch:

\`\`\`sh
cd "\$REPO_DIR"
base=\$(git rev-parse HEAD~20)
$BIN_NAME -path . -range "\$base..HEAD" -json > /tmp/range.json
\`\`\`

### Launch the 3D viewer in the background

It serves on 127.0.0.1:8787 by default and never exits, so never call it in the foreground.

\`\`\`sh
# long-lived viewer over a whole project
nohup $BIN_NAME -path /home/user/src/barse -view -addr 127.0.0.1:8787 \\
  > /tmp/betterdiff-view.log 2>&1 &
echo \$! > /tmp/betterdiff-view.pid

# viewer over a commit range (the diff scene)
nohup $BIN_NAME -path /home/user/src/barse -range A..B -view -addr 127.0.0.1:8788 \\
  > /tmp/betterdiff-range.log 2>&1 &
echo \$! > /tmp/betterdiff-range.pid

# check / stop
curl -fsS http://127.0.0.1:8787/scene.json > /dev/null && echo up
kill "\$(cat /tmp/betterdiff-view.pid)"
\`\`\`

The viewer exposes \`GET /scene.json\` (the scene graph) and \`/\` (static assets, embedded in
the binary). Bind to \`127.0.0.1\` unless remote access is intended — it has no auth.

## Building from source (no release available)

\`\`\`sh
git clone git@github.com:$REPO && cd citydiff
make vet
go build -o $BIN_NAME ./cmd/cli
\`\`\`

CGO is mandatory: \`CGO_ENABLED=0\` fails with *"build constraints exclude all Go files"* in
\`tree-sitter-go/bindings/go\`. Release CI therefore builds natively per runner
(linux/amd64 on ubuntu, darwin/arm64 on macos) instead of cross-compiling.

## Reading the output

- **\`entries[].kind\`** — the declaration kind (func, method, type, …).
- **\`entries[].calls\`** — direct calls recorded on that declaration, in source order,
  including calls inside nested function literals; a call whose target is outside the
  snapshot stays **unresolved** and is kept.
- **body hashes** — the signal that a body changed beyond its call list.
- Unresolved calls are not errors: the snapshot is the closed world for that parse, and the
  diff decides which calls matter.

## Repo map (for questions)

| Path | What |
| --- | --- |
| \`AGENTS.md\` | design intent and invariants of the tool |
| \`cmd/cli/main.go\` | flags, JSON shapes, the \`-view\` HTTP server |
| \`lib/parser.go\`, \`lib/parser/\` | Go source → Entry values (tree-sitter) |
| \`lib/diff/\` | comparing two parses at all three levels |
| \`lib/git/\` | git source: reads trees at refs via go-git |
| \`lib/files/\` | filesystem source |
| \`view/\` | embedded browser viewer (main.js, layout.js, fly.js, search.js) |
| \`.github/workflows/release.yml\` | CI: vet+build+smoke, release on \`v*\` tags |
EOF
  say "  wrote skill $skill_dir/SKILL.md"
}

agents_md_target() {
  if [ -n "$AGENTS_MD" ]; then printf '%s' "$AGENTS_MD"; return 0; fi
  for f in "$HOME/AGENTS.md" "$HOME/.agents/AGENTS.md" "$HOME/.config/agent/AGENTS.md"; do
    [ -f "$f" ] && { printf '%s' "$f"; return 0; }
  done
  printf '%s/.agents/AGENTS.md' "$HOME"
}

add_pointer() {
  f="$1"
  mkdir -p "$(dirname "$f")"
  [ -f "$f" ] || : > "$f"
  drop_pointer
  {
    printf '\n<!-- citydiff:begin -->\n'
    printf '## Skill: %s (betterdiff)\n\n' "$SKILL_NAME"
    printf 'The 3D Go code diff binary (`%s`) is installed at `%s`; its skill lives in\n' "$BIN_NAME" "$BIN_DIR"
    printf '`%s/%s/SKILL.md`. Read that skill before using it: it carries the source of truth\n' "$SKILLS_DIR" "$SKILL_NAME"
    printf '(https://github.com/%s), how the app works, and how to launch it for a project\n' "$REPO"
    printf 'or a commit range **in the background**. Re-run install.sh to upgrade both.\n'
    printf '<!-- citydiff:end -->\n'
  } >> "$f"
  say "  pointed $f at the skill"
}

if [ -n "$NO_SKILLS" ]; then
  say "  skills skipped (--no-skills)"
else
  write_skill
  add_pointer "$(agents_md_target)"
fi

say ""
say "$BIN_NAME $version installed."
"$BIN_DIR/$BIN_NAME" -h 2>&1 | head -3 || true
say ""
say "next:"
say "  $BIN_NAME -path /path/to/go/repo -range A..B -json"
say "  $BIN_NAME -path /path/to/go/repo -view &   # then open http://127.0.0.1:8787"