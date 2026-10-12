package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"citydiff/lib/git"
	"citydiff/lib/observe"
	"citydiff/lib/tour"
	"citydiff/view"
)

func (h *hub) create(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	var body openBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	if err := dec.Decode(&body); err != nil {
		problem(w, "remote", err)
		return
	}
	canonical, err := observe.Canonical(body.Remote)
	if err != nil {
		problem(w, "remote", err)
		return
	}
	if err := h.public(r.Context(), canonical); err != nil {
		problem(w, "remote", err)
		return
	}
	repo, err := h.ensure(r.Context(), canonical)
	if err != nil {
		problem(w, "remote", err)
		return
	}
	commitRange := strings.TrimSpace(body.Range)
	var script tour.Tour
	var tourRaw []byte
	if tourPresent(body.Tour) {
		tourRaw = append([]byte(nil), body.Tour...)
		script, err = tour.Parse(bytes.NewReader(tourRaw))
		if err != nil {
			problem(w, "script", err)
			return
		}
		if strings.TrimSpace(script.Range) == "" {
			problem(w, "range", errors.New("a tour needs a range"))
			return
		}
		if commitRange != "" {
			if err := sameCommits(repo, commitRange, script.Range); err != nil {
				problem(w, "range", err)
				return
			}
		}
	}
	if _, err := h.scenes.get(r.Context(), repo, ""); err != nil {
		problem(w, "remote", err)
		return
	}
	sceneRange := commitRange
	if script.Range != "" {
		sceneRange = script.Range
	}
	var snap *snapshot
	if sceneRange != "" {
		held, err := h.scenes.get(r.Context(), repo, sceneRange)
		if err != nil {
			problem(w, "range", err)
			return
		}
		snap = held.snap
	}
	if tourRaw != nil {
		if _, probs := snap.check(script); len(probs) > 0 {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"problems": probs})
			return
		}
	}
	obs, err := h.store.Create(canonical, repo)
	if err != nil {
		problem(w, "remote", err)
		return
	}
	name := ""
	if tourRaw != nil {
		name, err = h.store.UniqueName(obs.ID, script.Title)
		if err != nil {
			problem(w, "script", err)
			return
		}
		if err := h.store.PutTour(obs.ID, name, tourRaw); err != nil {
			problem(w, "script", err)
			return
		}
	}
	query := ""
	if name != "" {
		query = url.Values{"tour": {name}}.Encode()
	} else if commitRange != "" {
		query = url.Values{"range": {commitRange}}.Encode()
	}
	writeJSON(w, http.StatusCreated, openReply{
		ID:     obs.ID,
		URL:    pageURL(r, obs.ID, query),
		Remote: canonical,
	})
}

func (h *hub) observation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	obs, err := h.store.Load(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/o/"+id+"/")
	switch {
	case r.Method == http.MethodGet && rest == "scene.json":
		h.scene(w, r, obs)
	case r.Method == http.MethodGet && rest == "tour.json":
		h.tourFile(w, r, obs)
	case r.Method == http.MethodGet && rest == "api/observe":
		h.about(w, r, obs)
	case r.Method == http.MethodGet && rest == "api/tours":
		h.listTours(w, obs)
	case r.Method == http.MethodPost && rest == "api/tours":
		h.postTour(w, r, obs)
	case r.Method == http.MethodPost && rest == "api/tour":
		h.checkPosted(w, r, obs)
	case r.Method == http.MethodGet && rest == "api/refs":
		h.refs(w, obs)
	case r.Method == http.MethodGet && rest == "api/diff":
		h.diff(w, r, obs)
	case (r.Method == http.MethodGet || r.Method == http.MethodHead) && (rest == "" || rest == "index.html"):
		h.page(w, r, obs)
	case (r.Method == http.MethodGet || r.Method == http.MethodHead) && rest == "card.png":
		h.card(w, r, obs)
	case r.Method == http.MethodGet:
		h.files(w, r, id)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *hub) scene(w http.ResponseWriter, r *http.Request, obs observe.Obs) {
	commitRange, err := h.queryRange(obs, r.URL.Query())
	if err != nil {
		http.NotFound(w, r)
		return
	}
	held, err := h.scenes.get(r.Context(), obs.Repo, commitRange)
	if err != nil {
		problem(w, "range", err)
		return
	}
	writeScene(w, r, held.payload, held.packed)
}

func (h *hub) tourFile(w http.ResponseWriter, r *http.Request, obs observe.Obs) {
	name := r.URL.Query().Get("tour")
	if name == "" || remoteTour(name) {
		http.NotFound(w, r)
		return
	}
	raw, err := h.store.Tour(obs.ID, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

func (h *hub) about(w http.ResponseWriter, r *http.Request, obs observe.Obs) {
	_, branch, err := observe.Head(obs.Repo)
	if err != nil {
		problem(w, "remote", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": obs.ID, "remote": obs.Remote, "head": branch})
}

func (h *hub) listTours(w http.ResponseWriter, obs observe.Obs) {
	list, err := h.store.Tours(obs.ID)
	if err != nil {
		problem(w, "script", err)
		return
	}
	if list == nil {
		list = []observe.TourMeta{}
	}
	writeJSON(w, http.StatusOK, list)
}

// refreshClone brings the observation's clone up to date before a tour is
// resolved against it, then drops the scenes cached for that clone. A failed
// fetch does not fail the upload: the tour is judged against what the clone
// still holds, and the caller reports the error. The remote is checked as
// public again before the fetch.
func (h *hub) refreshClone(ctx context.Context, obs observe.Obs) (bool, error) {
	if h.public != nil {
		if err := h.public(ctx, obs.Remote); err != nil {
			return false, err
		}
	}
	err := h.refresh(ctx, obs.Remote, obs.Repo)
	h.scenes.invalidate(obs.Repo)
	return err == nil, err
}

func (h *hub) postTour(w http.ResponseWriter, r *http.Request, obs observe.Obs) {
	if !h.authorize(w, r) {
		return
	}
	raw, err := readBody(w, r)
	if err != nil {
		problem(w, "script", err)
		return
	}
	script, err := tour.Parse(bytes.NewReader(raw))
	if err != nil {
		problem(w, "script", err)
		return
	}
	if strings.TrimSpace(script.Range) == "" {
		problem(w, "range", errors.New("a tour needs a range"))
		return
	}
	refreshed, refreshErr := h.refreshClone(r.Context(), obs)
	held, err := h.scenes.get(r.Context(), obs.Repo, script.Range)
	if err != nil {
		problem(w, "range", err)
		return
	}
	if _, probs := held.snap.check(script); len(probs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"problems": probs})
		return
	}
	name := r.URL.Query().Get("name")
	if name != "" {
		if !observe.ValidName(name) {
			problem(w, "script", errors.New("tour name must be letters, digits, _ and -"))
			return
		}
		if h.store.HasTour(obs.ID, name) {
			problem(w, "script", errors.New("a tour with that name is already stored"))
			return
		}
	} else {
		name, err = h.store.UniqueName(obs.ID, script.Title)
		if err != nil {
			problem(w, "script", err)
			return
		}
	}
	if err := h.store.PutTour(obs.ID, name, raw); err != nil {
		problem(w, "script", err)
		return
	}
	reply := map[string]any{
		"name":      name,
		"title":     script.Title,
		"range":     script.Range,
		"url":       pageURL(r, obs.ID, url.Values{"tour": {name}}.Encode()),
		"refreshed": refreshed,
	}
	if refreshErr != nil {
		reply["refresh_error"] = refreshErr.Error()
	}
	writeJSON(w, http.StatusCreated, reply)
}

// checkPosted resolves a script the page already has, against the scene that
// page is showing. It does not store the script and it does not change the
// scene another request is looking at.
func (h *hub) checkPosted(w http.ResponseWriter, r *http.Request, obs observe.Obs) {
	raw, err := readBody(w, r)
	if err != nil {
		problem(w, "script", err)
		return
	}
	script, err := tour.Parse(bytes.NewReader(raw))
	if err != nil {
		problem(w, "script", err)
		return
	}
	commitRange, err := h.queryRange(obs, r.URL.Query())
	if err != nil {
		problem(w, "range", err)
		return
	}
	h.refreshClone(r.Context(), obs)
	held, err := h.scenes.get(r.Context(), obs.Repo, commitRange)
	if err != nil {
		problem(w, "range", err)
		return
	}
	resolved, probs := held.snap.check(script)
	if len(probs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"problems": probs})
		return
	}
	writeJSON(w, http.StatusOK, tourReply{
		Resolved: resolved,
		Scene:    sceneInfo{Range: held.snap.commitRange, Switched: false},
	})
}

func (h *hub) refs(w http.ResponseWriter, obs observe.Obs) {
	refs, err := observe.ListRefs(obs.Repo)
	if err != nil {
		problem(w, "range", err)
		return
	}
	writeJSON(w, http.StatusOK, refs)
}

func (h *hub) diff(w http.ResponseWriter, r *http.Request, obs observe.Obs) {
	commitRange, err := h.queryRange(obs, r.URL.Query())
	if err != nil {
		problem(w, "range", err)
		return
	}
	writePatch(w, r, obs.Repo, commitRange)
}

func (h *hub) files(w http.ResponseWriter, r *http.Request, id string) {
	noStore(http.StripPrefix("/o/"+id, http.FileServer(http.FS(view.FS)))).ServeHTTP(w, r)
}

// queryRange is the commit range this request is showing. A stored tour name
// wins, and the scene is that script's own range. An http tour URL is a
// script the browser fetches itself, so it does not name a scene.
func (h *hub) queryRange(obs observe.Obs, q url.Values) (string, error) {
	name := q.Get("tour")
	if name != "" && !remoteTour(name) {
		raw, err := h.store.Tour(obs.ID, name)
		if err != nil {
			return "", err
		}
		script, err := tour.Parse(bytes.NewReader(raw))
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(script.Range) == "" {
			return "", errors.New("tour has no range")
		}
		return script.Range, nil
	}
	return q.Get("range"), nil
}

func remoteTour(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func tourPresent(raw json.RawMessage) bool {
	s := bytes.TrimSpace(raw)
	return len(s) > 0 && !bytes.Equal(s, []byte("null"))
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(http.MaxBytesReader(w, r.Body, 4<<20))
	return buf.Bytes(), err
}

// authorize accepts the hub's API key. Reads stay open. A dropped script
// still posts to /api/tour, which only checks the script and stores nothing.
func (h *hub) authorize(w http.ResponseWriter, r *http.Request) bool {
	got := bearer(r)
	if h.key != "" && len(got) == len(h.key) && subtle.ConstantTimeCompare([]byte(got), []byte(h.key)) == 1 {
		return true
	}
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeJSON(w, http.StatusUnauthorized, map[string]any{
		"problems": []tour.Problem{{Field: "key", Message: "API key required"}},
	})
	return false
}

func bearer(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}

func pageURL(r *http.Request, id, query string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	switch strings.ToLower(r.Header.Get("X-Forwarded-Proto")) {
	case "https":
		scheme = "https"
	case "http":
		scheme = "http"
	}
	u := scheme + "://" + r.Host + "/o/" + id + "/"
	if query != "" {
		u += "?" + query
	}
	return u
}

func sameCommits(repo, a, b string) error {
	al, ar, err := git.Revs(repo, a)
	if err != nil {
		return err
	}
	bl, br, err := git.Revs(repo, b)
	if err != nil {
		return err
	}
	if al != bl || ar != br {
		return fmtRange(a, al, ar, b, bl, br)
	}
	return nil
}

func fmtRange(a, al, ar, b, bl, br string) error {
	return errors.New("range " + a + " is " + short(al) + ".." + short(ar) + " but the tour targets " + short(bl) + ".." + short(br))
}
