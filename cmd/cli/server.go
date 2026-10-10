package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"

	"citydiff/lib/git"
	"citydiff/lib/tour"
	"citydiff/view"
)

// viewer is the state behind -view: the scene it serves and the tour the
// page plays. Loading a tour for another range rebuilds the scene for that
// range, so the city and the left sidebar show the tour's changes.
type viewer struct {
	mu      sync.Mutex
	path    string
	snap    *snapshot
	payload []byte
	tour    []byte
	build   func(path, commitRange string) (*snapshot, error)
	// editor is the command E opens a node in; nvim when empty.
	editor []string
}

func newViewer(snap *snapshot, tourRaw []byte) (*viewer, error) {
	v := &viewer{path: snap.path, build: buildSnapshot}
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
	v.snap, v.payload = snap, payload
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
		v.mu.Lock()
		payload := v.payload
		v.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		// The size lets the viewer measure the download instead of guessing:
		// the page reads the body in chunks and moves its loading bar by bytes.
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
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
	mux.HandleFunc("GET /api/code", func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		snap := v.snap
		v.mu.Unlock()
		view, ok := snap.code(r.URL.Query().Get("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, view)
	})
	mountSkins(mux, skinsDir())
	mux.HandleFunc("GET /api/edit", v.serveEditor)
	mux.HandleFunc("GET /api/diff", v.serveDiff)
	mux.Handle("/", noStore(http.FileServer(http.FS(view.FS))))
	return mux
}

func problem(w http.ResponseWriter, field string, err error) {
	writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"problems": []tour.Problem{{Field: field, Message: err.Error()}}})
}
