package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"citydiff/lib/git"
	"citydiff/lib/tour"
	"citydiff/view"
)

// viewer is the state behind the default serve mode: the scene it serves and
// the tour the page plays. Loading a tour for another range rebuilds the scene
// for that range, so the city and the left sidebar show the tour's changes.
type viewer struct {
	mu      sync.Mutex
	path    string
	snap    *snapshot
	payload []byte
	// packed is payload gzipped, made by the first request that accepts it.
	packed []byte
	tour   []byte
	build  func(path, commitRange string) (*snapshot, error)
	// editor is the command E opens a node in; nvim when empty.
	editor []string
	// about is this build; update is a newer release, once the startup
	// check has found one.
	about  buildInfo
	update atomic.Pointer[release]
}

func newViewer(snap *snapshot, tourRaw []byte) (*viewer, error) {
	v := &viewer{path: snap.path, build: buildSnapshot, about: readBuild()}
	if err := v.setSnapshot(snap); err != nil {
		return nil, err
	}
	if tourRaw == nil {
		return v, nil
	}
	script, err := tour.Parse(bytes.NewReader(tourRaw))
	if err != nil {
		return nil, err
	}
	if _, err := v.useRange(script.Range); err != nil {
		return nil, err
	}
	v.tour = tourRaw
	return v, nil
}

func (v *viewer) setSnapshot(snap *snapshot) error {
	payload, err := json.Marshal(snap.scene)
	if err != nil {
		return err
	}
	v.snap, v.payload, v.packed = snap, payload, nil
	return nil
}

// useRange makes the scene the one for commitRange. It is a no-op for an
// empty range or the range already shown, compared by commit. Callers hold mu
// or own v alone.
func (v *viewer) useRange(commitRange string) (bool, error) {
	if commitRange == "" {
		return false, nil
	}
	if v.snap.commitRange != "" && v.snap.sameRange(commitRange) == nil {
		return false, nil
	}
	if _, _, err := git.Revs(v.path, commitRange); err != nil {
		return false, fmt.Errorf("cannot load the tour's range %q in %s: %w", commitRange, v.path, err)
	}
	snap, err := v.build(v.path, commitRange)
	if err != nil {
		return false, fmt.Errorf("cannot build the scene for %q: %w", commitRange, err)
	}
	fmt.Fprintf(os.Stderr, "citydiff: scene switched to the tour's range %s\n", commitRange)
	return true, v.setSnapshot(snap)
}

// sceneInfo tells the page which range it is looking at and whether the
// tour just moved it, in which case the page reloads the scene.
type sceneInfo struct {
	Range    string `json:"range"`
	Switched bool   `json:"switched"`
}

type tourReply struct {
	tour.Resolved
	Scene sceneInfo `json:"scene"`
}

func (v *viewer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /scene.json", func(w http.ResponseWriter, r *http.Request) {
		// A scene is JSON with a lot of repetition: gzip makes a large one about
		// a thirteenth of its size, which is most of a phone's wait for it.
		gz := strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
		v.mu.Lock()
		payload := v.payload
		if gz && v.packed == nil {
			v.packed = gzipped(payload)
		}
		body := payload
		if gz {
			body = v.packed
		}
		v.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		// The size lets the viewer measure the download instead of guessing:
		// the page reads the body in chunks and moves its loading bar by bytes.
		// It counts the bytes after decoding, so it is told that size.
		w.Header().Set("X-Scene-Bytes", strconv.Itoa(len(payload)))
		w.Header().Set("Vary", "Accept-Encoding")
		if gz {
			w.Header().Set("Content-Encoding", "gzip")
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	})
	mux.HandleFunc("GET /tour.json", func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		raw := v.tour
		v.mu.Unlock()
		if raw == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	})
	mux.HandleFunc("POST /api/tour", func(w http.ResponseWriter, r *http.Request) {
		var body bytes.Buffer
		if _, err := body.ReadFrom(http.MaxBytesReader(w, r.Body, 4<<20)); err != nil {
			problem(w, "script", err)
			return
		}
		script, err := tour.Parse(bytes.NewReader(body.Bytes()))
		if err != nil {
			problem(w, "script", err)
			return
		}
		v.mu.Lock()
		defer v.mu.Unlock()
		switched, err := v.useRange(script.Range)
		if err != nil {
			problem(w, "range", err)
			return
		}
		resolved, probs := v.snap.check(script)
		if len(probs) > 0 {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"problems": probs})
			return
		}
		// The page reloads after a switch and asks for this script again.
		v.tour = append([]byte(nil), body.Bytes()...)
		writeJSON(w, http.StatusOK, tourReply{Resolved: resolved, Scene: sceneInfo{Range: v.snap.commitRange, Switched: switched}})
	})
	mountSkins(mux, skinsDir())
	mux.HandleFunc("GET /api/edit", v.serveEditor)
	mux.HandleFunc("GET /api/diff", v.serveDiff)
	mux.HandleFunc("GET /api/version", v.serveVersion)
	mux.Handle("/", noStore(http.FileServer(http.FS(view.FS))))
	return mux
}

// gzipped compresses at the fastest level: a scene is compressed once and
// served many times, but the first page waits for it.
func gzipped(raw []byte) []byte {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	return buf.Bytes()
}

func problem(w http.ResponseWriter, field string, err error) {
	writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"problems": []tour.Problem{{Field: field, Message: err.Error()}}})
}
