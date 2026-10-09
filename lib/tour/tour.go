// Package tour reads, checks and resolves a tour: an ordered script of
// steps over one scene that the viewer plays.
package tour

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"citydiff/lib/scene"
)

// Version is the only script version this build reads.
const Version = 1

// Schema is the JSON Schema of a version 1 script.
//
//go:embed tour.schema.json
var Schema []byte

// Tour is one script. Range is the base..head the script was written for.
type Tour struct {
	Version int    `json:"version"`
	Title   string `json:"title,omitempty"`
	Range   string `json:"range,omitempty"`
	Steps   []Step `json:"steps"`
}

// Step is one stop. Every field but Title is optional, and a step starts
// from a clean view: nothing selected, nothing highlighted.
type Step struct {
	Title     string   `json:"title"`
	Note      string   `json:"note,omitempty"`
	Duration  float64  `json:"duration,omitempty"`
	Mode      string   `json:"mode,omitempty"`
	Select    string   `json:"select,omitempty"`
	Focus     string   `json:"focus,omitempty"`
	Highlight []string `json:"highlight,omitempty"`
	Path      *Path    `json:"path,omitempty"`
	Dim       *bool    `json:"dim,omitempty"`
	Camera    string   `json:"camera,omitempty"`
	Zoom      float64  `json:"zoom,omitempty"`
	Code      string   `json:"code,omitempty"`
}

// Path asks for the call path from one function or method to another.
type Path struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Modes and cameras a step can ask for.
var (
	Modes   = []string{"changes", "full"}
	Cameras = []string{"overview", "top", "fit", "close"}
)

// DefaultDuration is how long a step without a duration plays, in seconds.
const DefaultDuration = 8

// Parse reads one script. Unknown fields are an error, so a typo in a
// field name is caught rather than ignored.
func Parse(r io.Reader) (Tour, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Tour{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var t Tour
	if err := dec.Decode(&t); err != nil {
		return Tour{}, fmt.Errorf("tour: %w", err)
	}
	if dec.More() {
		return Tour{}, errors.New("tour: trailing data after the script")
	}
	return t, nil
}

// Problem is one thing wrong with a script. Step is 1-based; 0 is the script itself.
type Problem struct {
	Step    int    `json:"step"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (p Problem) String() string {
	if p.Step == 0 {
		return fmt.Sprintf("%s: %s", p.Field, p.Message)
	}
	return fmt.Sprintf("step %d %s: %s", p.Step, p.Field, p.Message)
}

// Resolved is a script with every name replaced by the node it names.
type Resolved struct {
	Version int            `json:"version"`
	Title   string         `json:"title,omitempty"`
	Range   string         `json:"range,omitempty"`
	Steps   []ResolvedStep `json:"steps"`
}

// ResolvedStep carries the step as written and the nodes it points at.
// Path is every function on the call path, first to last.
type ResolvedStep struct {
	Step
	Duration float64 `json:"duration"`
	Dim      bool    `json:"dim"`
	Targets  Targets `json:"targets"`
}

// Targets are scene ids.
type Targets struct {
	Select    *Node  `json:"select,omitempty"`
	Focus     *Node  `json:"focus,omitempty"`
	Highlight []Node `json:"highlight,omitempty"`
	Path      []Node `json:"path,omitempty"`
	Code      *Node  `json:"code,omitempty"`
}

var (
	selectKinds = []string{KindPackage, KindExternal}
	callKinds   = []string{KindFunction, KindMethod}
	codeKinds   = []string{KindType, KindFunction, KindMethod, KindVariable}
)

// Check resolves every name in t against sc. sameRange, when not nil,
// reports whether the script's range is the range sc was built from; a
// script written for another range is a problem. The result is usable only
// when there are no problems.
func Check(t Tour, sc scene.Scene, sameRange func(string) error) (Resolved, []Problem) {
	ix := NewIndex(sc)
	calls := newCallGraph(sc, ix)
	var probs []Problem
	add := func(step int, field string, err error) {
		probs = append(probs, Problem{Step: step, Field: field, Message: err.Error()})
	}
	if t.Version != Version {
		add(0, "version", fmt.Errorf("is %d; this build reads version %d", t.Version, Version))
	}
	if sameRange != nil && strings.TrimSpace(t.Range) != "" {
		if err := sameRange(t.Range); err != nil {
			add(0, "range", err)
		}
	}
	if len(t.Steps) == 0 {
		add(0, "steps", errors.New("a tour needs at least one step"))
	}
	out := Resolved{Version: t.Version, Title: t.Title, Range: t.Range, Steps: make([]ResolvedStep, len(t.Steps))}
	for i, s := range t.Steps {
		n := i + 1
		r := ResolvedStep{Step: s, Duration: s.Duration, Dim: true}
		if s.Duration == 0 {
			r.Duration = DefaultDuration
		}
		if s.Dim != nil {
			r.Dim = *s.Dim
		}
		if strings.TrimSpace(s.Title) == "" {
			add(n, "title", errors.New("is required"))
		}
		if s.Duration < 0 {
			add(n, "duration", errors.New("must not be negative"))
		}
		if s.Mode != "" && !oneOf(s.Mode, Modes) {
			add(n, "mode", fmt.Errorf("is %q; use %s", s.Mode, strings.Join(Modes, " or ")))
		}
		if s.Camera != "" && !oneOf(s.Camera, Cameras) {
			add(n, "camera", fmt.Errorf("is %q; use one of %s", s.Camera, strings.Join(Cameras, ", ")))
		}
		if s.Zoom < 0 {
			add(n, "zoom", errors.New("must be positive"))
		}
		if s.Select != "" && s.Focus != "" {
			add(n, "focus", errors.New("select and focus both move the camera; use one per step"))
		}
		one := func(field, name string, want []string) *Node {
			if name == "" {
				return nil
			}
			node, err := ix.Resolve(name, want...)
			if err != nil {
				add(n, field, err)
				return nil
			}
			return &node
		}
		r.Targets.Select = one("select", s.Select, selectKinds)
		r.Targets.Focus = one("focus", s.Focus, nil)
		r.Targets.Code = one("code", s.Code, codeKinds)
		for j, name := range s.Highlight {
			if node := one(fmt.Sprintf("highlight[%d]", j), name, nil); node != nil {
				r.Targets.Highlight = append(r.Targets.Highlight, *node)
			}
		}
		if s.Path != nil {
			from := one("path.from", s.Path.From, callKinds)
			to := one("path.to", s.Path.To, callKinds)
			if from != nil && to != nil {
				ids := calls.path(from.ID, to.ID)
				if ids == nil {
					add(n, "path", fmt.Errorf("no resolved call path from %s to %s", from.Name, to.Name))
				}
				for _, id := range ids {
					node, _ := ix.Node(id)
					r.Targets.Path = append(r.Targets.Path, node)
				}
			}
		}
		out.Steps[i] = r
	}
	return out, probs
}

func oneOf(v string, list []string) bool {
	for _, item := range list {
		if v == item {
			return true
		}
	}
	return false
}
