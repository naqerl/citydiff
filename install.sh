#!/usr/bin/env sh
# Install citydiff from the latest GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/naqerl/citydiff/main/install.sh | sh
#
# Options (env or flags):
#   BIN_DIR=~/.local/bin        where the binary is installed
#   SKILLS_DIR=~/.agents/skills where the agent skill is written
#   AGENTS_MD=<file>            agent instructions file to point at the skill
#   TAG=v0.0.1                  install a specific tag instead of the latest
#   NO_PATH=1                   do not touch PATH
#   NO_SKILLS=1                 do not write the skills / AGENTS.md pointer
#   SKILL_TARGETS="d1 d2"       skill dirs for the citydiff-tour skill
#                               (default: ~/.agents/skills, plus ~/.claude/skills
#                               and ~/.cursor/skills when ~/.claude, ~/.cursor exist)
#   SKILLS_ONLY=1               install the skills only, not the binary
#   FROM_SOURCE=1               build the binary with git + go instead of a release
#   UNINSTALL=1                 remove binary, skills and AGENTS.md pointer
set -eu

REPO="${REPO:-naqerl/citydiff}"
BIN_NAME="${BIN_NAME:-citydiff}"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
SKILL_NAME="${SKILL_NAME:-citydiff}"
SKILLS_DIR="${SKILLS_DIR:-$HOME/.agents/skills}"
AGENTS_MD="${AGENTS_MD:-}"
NO_PATH="${NO_PATH:-}"
NO_SKILLS="${NO_SKILLS:-}"
UNINSTALL="${UNINSTALL:-}"
TAG="${TAG:-}"
SKILL_TARGETS="${SKILL_TARGETS:-}"
SKILLS_ONLY="${SKILLS_ONLY:-}"
FROM_SOURCE="${FROM_SOURCE:-}"
TOUR_SKILL="citydiff-tour"
RELEASES_API="https://api.github.com/repos/$REPO/releases"

usage() {
  sed -n '2,23p' "$0" | sed 's/^# \{0,1\}//'
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
    --skill-targets=*) SKILL_TARGETS="${arg#--skill-targets=}" ;;
    --skills-only) SKILLS_ONLY=1 ;;
    --from-source) FROM_SOURCE=1 ;;
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

# ------------------------------------------------------------ tour skill ---

# The skill dirs agents read. ~/.agents/skills always; Claude's and Cursor's
# only when that agent is installed, so no stray dot-directories appear.
skill_targets() {
  if [ -n "$SKILL_TARGETS" ]; then printf '%s\n' $SKILL_TARGETS; return 0; fi
  printf '%s\n' "$SKILLS_DIR"
  [ -d "$HOME/.claude" ] && printf '%s\n' "$HOME/.claude/skills"
  [ -d "$HOME/.cursor" ] && printf '%s\n' "$HOME/.cursor/skills"
  return 0
}

# The skill comes from this checkout when install.sh runs from one, else
# from the repository at the installed tag (or main).
tour_skill_source() {
  here=$(dirname "$0" 2>/dev/null || printf .)
  if [ -f "$here/skills/$TOUR_SKILL/SKILL.md" ]; then
    cat "$here/skills/$TOUR_SKILL/SKILL.md"
    return 0
  fi
  ref="${1:-main}"
  fetch "https://raw.githubusercontent.com/$REPO/$ref/skills/$TOUR_SKILL/SKILL.md" 2>/dev/null \
    || fetch "https://raw.githubusercontent.com/$REPO/main/skills/$TOUR_SKILL/SKILL.md"
}

install_tour_skill() {
  body_file="$(mktemp)"
  if ! tour_skill_source "${1:-}" > "$body_file" || ! grep -q "^name: $TOUR_SKILL\$" "$body_file"; then
    rm -f "$body_file"
    warn "  could not fetch the $TOUR_SKILL skill; skipped"
    return 0
  fi
  for dir in $(skill_targets); do
    target="$dir/$TOUR_SKILL"
    if [ -e "$target" ] && [ ! -d "$target" ]; then
      warn "  $target exists and is not a directory; skipped"
      continue
    fi
    if [ -f "$target/SKILL.md" ] && ! grep -q "^name: $TOUR_SKILL\$" "$target/SKILL.md"; then
      warn "  $target/SKILL.md is not ours; left alone"
      continue
    fi
    mkdir -p "$target" || { warn "  cannot create $target; skipped"; continue; }
    if [ -f "$target/SKILL.md" ] && cmp -s "$body_file" "$target/SKILL.md"; then
      say "  $target/SKILL.md up to date"
      continue
    fi
    cp "$body_file" "$target/SKILL.md.tmp.$$" && mv "$target/SKILL.md.tmp.$$" "$target/SKILL.md"
    say "  wrote skill $target/SKILL.md"
  done
  rm -f "$body_file"
}

# ------------------------------------------------------------- base skill -----

# The base skill ships with the release: the copy in the repository at the
# installed tag, so what lands beside the binary is the text that release
# carried. A checkout wins when there is one, and a tag older than the file
# falls back to main with a warning, which is the best it can do.
base_skill_source() {
  here=$(dirname "$0" 2>/dev/null || printf .)
  if [ -f "$here/skills/$SKILL_NAME/SKILL.md" ]; then
    cat "$here/skills/$SKILL_NAME/SKILL.md"
    return 0
  fi
  ref="${1:-main}"
  if fetch "https://raw.githubusercontent.com/$REPO/$ref/skills/$SKILL_NAME/SKILL.md" 2>/dev/null; then
    return 0
  fi
  warn "  no skills/$SKILL_NAME/SKILL.md at $ref; taking the skill from main"
  fetch "https://raw.githubusercontent.com/$REPO/main/skills/$SKILL_NAME/SKILL.md"
}

write_skill() {
  skill_dir="$SKILLS_DIR/$SKILL_NAME"
  src="$(mktemp)"
  if ! base_skill_source "${1:-}" > "$src"; then
    rm -f "$src"
    warn "  could not fetch the $SKILL_NAME skill; skipped"
    return 0
  fi
  # The file carries placeholders, so fill them in before checking that what we
  # fetched is the skill we think it is.
  filled="$src.filled.$$"
  if ! sed -e "s|__REPO_DIR__|${REPO##*/}|g" \
           -e "s|__REPO__|$REPO|g" \
           -e "s|__BIN_DIR__|$BIN_DIR|g" \
           -e "s|__BIN_NAME__|$BIN_NAME|g" \
           -e "s|__SKILL_NAME__|$SKILL_NAME|g" \
           -e "s|__VERSION__|${version:-${TAG:-main}}|g" \
           -e "s|__DATE__|$(date -u +%Y-%m-%d)|g" \
           "$src" > "$filled" || ! grep -q "^name: $SKILL_NAME$" "$filled"; then
    rm -f "$src" "$filled"
    warn "  the $SKILL_NAME skill we fetched is not ours; skipped"
    return 0
  fi
  if mkdir -p "$skill_dir" && mv "$filled" "$skill_dir/SKILL.md"; then
    rm -f "$src"
    say "  wrote skill $skill_dir/SKILL.md"
    return 0
  fi
  rm -f "$src" "$filled"
  warn "  cannot write $skill_dir/SKILL.md; skipped"
}

# --------------------------------------------------------------- uninstall ---

uninstall() {
  say "uninstalling $BIN_NAME"
  if [ -e "$BIN_DIR/$BIN_NAME" ]; then rm -f "$BIN_DIR/$BIN_NAME"; say "  removed $BIN_DIR/$BIN_NAME"; fi
  if [ -d "$SKILLS_DIR/$SKILL_NAME" ]; then rm -rf "$SKILLS_DIR/$SKILL_NAME"; say "  removed $SKILLS_DIR/$SKILL_NAME"; fi
  for dir in $(skill_targets); do
    # Only a directory this installer wrote: it holds our SKILL.md and nothing else.
    if [ -f "$dir/$TOUR_SKILL/SKILL.md" ] && grep -q "^name: $TOUR_SKILL\$" "$dir/$TOUR_SKILL/SKILL.md"; then
      rm -f "$dir/$TOUR_SKILL/SKILL.md"
      rmdir "$dir/$TOUR_SKILL" 2>/dev/null || true
      say "  removed $dir/$TOUR_SKILL"
    fi
  done
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

if [ -n "$SKILLS_ONLY" ]; then
  write_skill "$TAG"
  install_tour_skill "$TAG"
  say "skills installed."
  exit 0
fi

build_from_source() {
  need git
  command -v go >/dev/null 2>&1 || die "go is needed to build from source (https://go.dev/dl/)"
  src="$(mktemp -d)"
  trap 'rm -rf "$src"' EXIT INT TERM
  say "  building from source (${TAG:-main})"
  if [ -n "$TAG" ]; then
    git clone -q --depth 1 --branch "$TAG" "https://github.com/$REPO.git" "$src/citydiff"
  else
    git clone -q --depth 1 "https://github.com/$REPO.git" "$src/citydiff"
  fi
  (cd "$src/citydiff" && CGO_ENABLED=1 go build -o "$src/$BIN_NAME" ./cmd/cli) || die "go build failed (CGO and a C compiler are required)"
  mkdir -p "$BIN_DIR"
  cp "$src/$BIN_NAME" "$BIN_DIR/$BIN_NAME.tmp.$$" && chmod +x "$BIN_DIR/$BIN_NAME.tmp.$$" && mv "$BIN_DIR/$BIN_NAME.tmp.$$" "$BIN_DIR/$BIN_NAME"
  say "  installed $BIN_DIR/$BIN_NAME (built from source)"
}

if [ -n "$FROM_SOURCE" ]; then
  build_from_source
  version="${TAG:-main}"
  if [ -z "$NO_SKILLS" ]; then write_skill "$TAG"; install_tour_skill "$TAG"; fi
  say ""
  say "$BIN_NAME $version installed."
  exit 0
fi

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
[ -f "$tmpdir/$BIN_NAME" ] || die "archive did not contain $BIN_NAME (it may predate the rename: releases up to v0.0.1 shipped a 'betterdiff' binary; install a newer release or build from source)"
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
    printf '## Skill: %s (citydiff)\n\n' "$SKILL_NAME"
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
  write_skill "$version"
  install_tour_skill "$version"
  add_pointer "$(agents_md_target)"
fi

say ""
say "$BIN_NAME $version installed."
"$BIN_DIR/$BIN_NAME" -h 2>&1 | head -3 || true
say ""
say "next:"
say "  $BIN_NAME -path /path/to/go/repo -range A..B -json"
say "  $BIN_NAME -path /path/to/go/repo -view &   # then open http://127.0.0.1:8787"
say "  $BIN_NAME nodes -path /path/to/repo -range A..B -changed   # names for a tour (skill: $TOUR_SKILL)"