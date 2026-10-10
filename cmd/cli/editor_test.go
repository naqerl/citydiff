package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func editorServer(t *testing.T, root string, editor ...string) *httptest.Server {
	t.Helper()
	v := &viewer{path: root, editor: editor}
	srv := httptest.NewServer(v.handler())
	t.Cleanup(srv.Close)
	return srv
}

func dial(t *testing.T, srv *httptest.Server, query string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/edit?"+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

// readUntil collects output frames until want shows up, and fails on a close first.
func readUntil(t *testing.T, conn *websocket.Conn, out *strings.Builder, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for !strings.Contains(out.String(), want) {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %q in %q: %v", want, out.String(), err)
		}
		out.Write(data)
	}
}

func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "a.go"), []byte("package pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestEditorGetsArgsResizesAndClosesOnExit(t *testing.T) {
	root := repo(t)
	srv := editorServer(t, root, "sh", "-c", `printf 'ARGS[%s]' "$*"; printf 'SIZE[%s]' "$(stty size)"; read x; printf 'SIZE[%s]' "$(stty size)"`, "sh")
	conn := dial(t, srv, "file=pkg/a.go&line=12&col=3&cols=100&rows=30")
	defer conn.CloseNow()
	var out strings.Builder
	readUntil(t, conn, &out, "SIZE[30 100]")
	if want := "ARGS[+call cursor(12,3) " + filepath.Join(root, "pkg", "a.go") + "]"; !strings.Contains(out.String(), want) {
		t.Fatalf("args: %q lacks %q", out.String(), want)
	}
	ctx := context.Background()
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":120,"rows":40}`)); err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, []byte("\r")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, conn, &out, "SIZE[40 120]")
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		if _, _, err := conn.Read(rctx); err != nil {
			if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				t.Fatalf("want a normal close when the editor exits, got %v", err)
			}
			return
		}
	}
}

func TestEditorIsKilledWhenTheSocketDrops(t *testing.T) {
	root := repo(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	srv := editorServer(t, root, "sh", "-c", `echo $$ > `+pidFile+`; echo up; exec sleep 60`, "sh")
	conn := dial(t, srv, "file=pkg/a.go")
	var out strings.Builder
	readUntil(t, conn, &out, "up")
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	conn.CloseNow()
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("editor %d still running after the socket dropped", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestEditorReportsAMissingFile(t *testing.T) {
	srv := editorServer(t, repo(t), "sh", "-c", "true")
	conn := dial(t, srv, "file=gone.go&line=3")
	defer conn.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	kind, data, err := conn.Read(ctx)
	if err != nil || kind != websocket.MessageText || !strings.Contains(string(data), `"type":"error"`) || !strings.Contains(string(data), "not in the working tree") {
		t.Fatalf("got %v %q %v", kind, data, err)
	}
}

func TestResolveEditStaysInTheRepository(t *testing.T) {
	root := repo(t)
	for _, rel := range []string{"../x.go", "pkg/../../x.go", "/etc/passwd", ""} {
		if _, err := resolveEdit(root, rel, "", 1, 1); err == nil {
			t.Errorf("%q: want an error", rel)
		}
	}
	if _, err := resolveEdit(root, "pkg/a.go", "../..", 0, 0); err == nil {
		t.Error("a package directory outside the root must be refused")
	}
	got, err := resolveEdit(root, "pkg/a.go", "pkg", 0, 0)
	if err != nil || !got.dir || got.path != filepath.Join(root, "pkg") {
		t.Fatalf("package: %+v %v", got, err)
	}
	got, err = resolveEdit(root, "pkg/a.go", "nothere", 0, 0)
	if err != nil || got.dir || got.path != filepath.Join(root, "pkg", "a.go") {
		t.Fatalf("package without its directory falls back to its file: %+v %v", got, err)
	}
	if _, err := resolveEdit(root, "pkg/deleted.go", "", 4, 0); err == nil || !strings.Contains(err.Error(), "deleted") {
		t.Fatalf("deleted file: %v", err)
	}
}

func TestEditorArgs(t *testing.T) {
	cases := []struct {
		t    editTarget
		want string
	}{
		{editTarget{path: "/r/a.go", line: 7, col: 2}, "+call cursor(7,2) /r/a.go"},
		{editTarget{path: "/r/a.go", line: 7}, "+7 /r/a.go"},
		{editTarget{path: "/r/a.go"}, "/r/a.go"},
		{editTarget{path: "/r/pkg", dir: true, line: 3}, "/r/pkg"},
	}
	for _, c := range cases {
		if got := strings.Join(editorArgs(c.t), " "); got != c.want {
			t.Errorf("%+v: got %q want %q", c.t, got, c.want)
		}
	}
}
