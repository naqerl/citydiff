package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"
)

// editTarget is what the editor opens: a file at a position, or a directory.
type editTarget struct {
	path      string
	line, col int
	dir       bool
}

// repoRoot is the directory the scene's file paths are relative to: the
// served path, or the directory of a single served file.
func repoRoot(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	return abs, nil
}

// within joins rel to root and refuses anything that lands outside root,
// through .. or an absolute path.
func within(root, rel string) (string, error) {
	if rel == "" {
		return "", errors.New("no file to open")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s is outside the repository", rel)
	}
	full := filepath.Join(root, filepath.FromSlash(rel))
	back, err := filepath.Rel(root, full)
	if err != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the repository", rel)
	}
	return full, nil
}

// errNotInWorkingTree marks a file the working tree does not have, so the
// caller can fall back to the snapshot's own bytes. The working tree need not
// be checked out at either side of the range the scene came from.
var errNotInWorkingTree = errors.New("not in the working tree; it may be deleted in this range")

// resolveEdit maps the page's request to what is on disk. A package names
// its directory and, as a fallback, its first file. A file that is not in the
// working tree is errNotInWorkingTree, for the caller to answer from the
// snapshot.
func resolveEdit(root, file, dir string, line, col int) (editTarget, error) {
	if dir != "" {
		full, err := within(root, dir)
		if err != nil {
			return editTarget{}, err
		}
		if info, err := os.Stat(full); err == nil && info.IsDir() {
			return editTarget{path: full, dir: true}, nil
		}
	}
	full, err := within(root, file)
	if err != nil {
		return editTarget{}, err
	}
	info, err := os.Stat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return editTarget{}, fmt.Errorf("%s is %w", file, errNotInWorkingTree)
	}
	if err != nil {
		return editTarget{}, err
	}
	if info.IsDir() {
		return editTarget{path: full, dir: true}, nil
	}
	return editTarget{path: full, line: max(line, 0), col: max(col, 0)}, nil
}

// snapshotEdit opens the snapshot's copy of a file the working tree does not
// have. A range's sources are already in memory: the checkout does not have to
// be at either side of the range, so E must open the bytes the city was drawn
// from. The right side wins because that is what the city draws; the left side
// answers for a declaration removed in the range. The copy is read-only — it is
// a copy, and an edit must not be mistaken for an edit of the repo file — and
// it lives only as long as the editor's socket.
func (v *viewer) snapshotEdit(file string, line, col int) (editTarget, func(), error) {
	v.mu.Lock()
	snap := v.snap
	v.mu.Unlock()
	src, found := []byte(nil), false
	if snap != nil {
		if b, ok := snap.after[file]; ok {
			src, found = b, true
		} else if b, ok := snap.before[file]; ok {
			src, found = b, true
		}
	}
	if !found {
		if snap != nil && snap.commitRange != "" {
			return editTarget{}, nil, fmt.Errorf("%s is not in the working tree nor at %s", file, snap.commitRange)
		}
		return editTarget{}, nil, fmt.Errorf("%s is not in the working tree", file)
	}
	dir, err := os.MkdirTemp("", "citydiff-edit-")
	if err != nil {
		return editTarget{}, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	// Keep the base name so the editor still reads the syntax from the extension.
	path := filepath.Join(dir, filepath.Base(filepath.FromSlash(file)))
	if err := os.WriteFile(path, src, 0o444); err != nil {
		cleanup()
		return editTarget{}, nil, err
	}
	return editTarget{path: path, line: max(line, 0), col: max(col, 0)}, cleanup, nil
}

// editorArgs are the arguments after the editor command: the cursor goes to
// the line, and the column when one is known.
func editorArgs(t editTarget) []string {
	switch {
	case t.dir || t.line <= 0:
		return []string{t.path}
	case t.col > 0:
		return []string{fmt.Sprintf("+call cursor(%d,%d)", t.line, t.col), t.path}
	}
	return []string{"+" + strconv.Itoa(t.line), t.path}
}

// control is a text frame from the page. Keystrokes come as binary frames.
type control struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// serveEditor runs the editor on a PTY behind a WebSocket. Output goes out
// as binary frames; binary frames in are keystrokes and text frames are
// resize messages. The socket closes when the editor exits, and a dropped
// socket kills the editor. A target that cannot be opened is reported in an
// "error" text frame before the close.
func (v *viewer) serveEditor(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	q := r.URL.Query()
	fail := func(err error) {
		msg, _ := json.Marshal(map[string]string{"type": "error", "message": err.Error()})
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		_ = conn.Write(ctx, websocket.MessageText, msg)
		conn.Close(websocket.StatusNormalClosure, "")
	}
	root, err := repoRoot(v.path)
	if err != nil {
		fail(err)
		return
	}
	target, err := resolveEdit(root, q.Get("file"), q.Get("dir"), atoi(q.Get("line")), atoi(q.Get("col")))
	if errors.Is(err, errNotInWorkingTree) {
		var cleanup func()
		target, cleanup, err = v.snapshotEdit(q.Get("file"), atoi(q.Get("line")), atoi(q.Get("col")))
		if cleanup != nil {
			defer cleanup()
		}
	}
	if err != nil {
		fail(err)
		return
	}
	cols, rows := uint16(atoi(q.Get("cols"))), uint16(atoi(q.Get("rows")))
	if cols == 0 || rows == 0 {
		cols, rows = 80, 24
	}
	editor := v.editor
	if len(editor) == 0 {
		editor = []string{"nvim"}
	}
	cmd := exec.Command(editor[0], append(append([]string{}, editor[1:]...), editorArgs(target)...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		fail(fmt.Errorf("cannot start %s: %w", editor[0], err))
		return
	}
	defer tty.Close()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := tty.Read(buf)
			if n > 0 && conn.Write(ctx, websocket.MessageBinary, buf[:n]) != nil {
				return
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		for {
			kind, data, err := conn.Read(ctx)
			if err != nil {
				_ = cmd.Process.Kill()
				return
			}
			if kind == websocket.MessageBinary {
				_, _ = tty.Write(data)
				continue
			}
			var c control
			if json.Unmarshal(data, &c) == nil && c.Type == "resize" && c.Cols > 0 && c.Rows > 0 {
				_ = pty.Setsize(tty, &pty.Winsize{Cols: c.Cols, Rows: c.Rows})
			}
		}
	}()
	select {
	case <-exited:
		// Let the last of the output reach the page before the close.
		time.Sleep(50 * time.Millisecond)
		conn.Close(websocket.StatusNormalClosure, "editor exited")
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-exited
	}
}
