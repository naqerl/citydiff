package observe

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// idRE is the id Create writes. Anything else is not an observation, which
// keeps a path out of the directory walk.
var idRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// nameRE is a tour's name in a URL: one segment, no dots, no slashes.
var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// ValidID reports whether id is an observation id this package wrote.
func ValidID(id string) bool { return idRE.MatchString(id) }

// ValidName reports whether name can be a tour's name in the address bar.
func ValidName(name string) bool { return nameRE.MatchString(name) }

// Store is the observation directory.
type Store struct {
	root string
}

// OpenStore creates root if it is missing.
func OpenStore(root string) (*Store, error) {
	for _, sub := range []string{"repos", "obs"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return nil, err
		}
	}
	return &Store{root: root}, nil
}

// RepoDir is where the clone of canonical is kept.
func (s *Store) RepoDir(canonical string) string {
	return filepath.Join(s.root, "repos", RepoKey(canonical))
}

// Obs is one submission. Writes are authorised by the hub's API key, not by
// anything stored here.
type Obs struct {
	ID      string    `json:"id"`
	Remote  string    `json:"remote"`
	Repo    string    `json:"repo"`
	Created time.Time `json:"created"`
}

// Create records a new observation.
func (s *Store) Create(remote, repo string) (Obs, error) {
	obs := Obs{
		ID:      newID(),
		Remote:  remote,
		Repo:    repo,
		Created: time.Now().UTC(),
	}
	dir := filepath.Join(s.root, "obs", obs.ID)
	if err := os.MkdirAll(filepath.Join(dir, "tours"), 0o755); err != nil {
		return Obs{}, err
	}
	raw, err := json.MarshalIndent(obs, "", "  ")
	if err != nil {
		return Obs{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), append(raw, '\n'), 0o600); err != nil {
		return Obs{}, err
	}
	return obs, nil
}

// Load reads one observation. A missing id is an error.
func (s *Store) Load(id string) (Obs, error) {
	if !ValidID(id) {
		return Obs{}, errors.New("unknown observation")
	}
	raw, err := os.ReadFile(filepath.Join(s.root, "obs", id, "meta.json"))
	if err != nil {
		return Obs{}, errors.New("unknown observation")
	}
	var obs Obs
	if err := json.Unmarshal(raw, &obs); err != nil {
		return Obs{}, err
	}
	if obs.ID != id {
		return Obs{}, errors.New("unknown observation")
	}
	return obs, nil
}

// TourMeta is one stored tour, without its steps.
type TourMeta struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Range string `json:"range"`
}

// Tours lists the tours stored on id, by name.
func (s *Store) Tours(id string) ([]TourMeta, error) {
	if _, err := s.Load(id); err != nil {
		return nil, err
	}
	dir := filepath.Join(s.root, "obs", id, "tours")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []TourMeta
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".json")
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || !ValidName(name) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var head struct {
			Title string `json:"title"`
			Range string `json:"range"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			return nil, err
		}
		out = append(out, TourMeta{Name: name, Title: head.Title, Range: head.Range})
	}
	return out, nil
}

// Tour reads one stored script.
func (s *Store) Tour(id, name string) ([]byte, error) {
	if !ValidName(name) {
		return nil, errors.New("unknown tour")
	}
	if _, err := s.Load(id); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(s.root, "obs", id, "tours", name+".json"))
	if err != nil {
		return nil, errors.New("unknown tour")
	}
	return raw, nil
}

// PutTour stores raw under name. The caller has already checked the script.
func (s *Store) PutTour(id, name string, raw []byte) error {
	if !ValidName(name) {
		return errors.New("tour name must be letters, digits, _ and -")
	}
	if _, err := s.Load(id); err != nil {
		return err
	}
	path := filepath.Join(s.root, "obs", id, "tours", name+".json")
	return os.WriteFile(path, raw, 0o644)
}

// DeleteTour removes one stored script. The name is validated before it is
// joined into a path, so a name from a URL cannot leave the tours directory.
func (s *Store) DeleteTour(id, name string) error {
	if !ValidName(name) {
		return errors.New("unknown tour")
	}
	if _, err := s.Load(id); err != nil {
		return err
	}
	path := filepath.Join(s.root, "obs", id, "tours", name+".json")
	if err := os.Remove(path); err != nil {
		return errors.New("unknown tour")
	}
	return nil
}

// HasTour reports whether name is already stored.
func (s *Store) HasTour(id, name string) bool {
	_, err := s.Tour(id, name)
	return err == nil
}

// UniqueName is base, or base-2, base-3, when base is taken.
func (s *Store) UniqueName(id, base string) (string, error) {
	base = Slug(base)
	for n := 1; n < 1000; n++ {
		name := base
		if n > 1 {
			name = withSuffix(base, n)
		}
		if !s.HasTour(id, name) {
			return name, nil
		}
	}
	return "", errors.New("too many tours with that name")
}

// Slug is a tour name made from a title.
func Slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		s = "tour"
	}
	if len(s) > 64 {
		s = strings.Trim(s[:64], "-")
	}
	return s
}

func withSuffix(base string, n int) string {
	suf := "-" + strconv.Itoa(n)
	room := 64 - len(suf)
	if room < 1 {
		room = 1
	}
	if len(base) > room {
		base = strings.TrimRight(base[:room], "-")
	}
	if base == "" {
		base = "t"
	}
	return base + suf
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// RefCommit is one recent commit the range panel can pick.
type RefCommit struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
}

// Refs is what /range lists.
type Refs struct {
	Head     string      `json:"head"`
	Branches []string    `json:"branches"`
	Tags     []string    `json:"tags"`
	Commits  []RefCommit `json:"commits"`
}

// Head is the clone's current commit and, when HEAD is a branch, its name.
func Head(repo string) (hash, branch string, err error) {
	r, err := gogit.PlainOpen(repo)
	if err != nil {
		return "", "", err
	}
	ref, err := r.Head()
	if err != nil {
		return "", "", err
	}
	if ref.Name().IsBranch() {
		branch = ref.Name().Short()
	}
	return ref.Hash().String(), branch, nil
}

// ListRefs reads branches, tags and the twenty commits from HEAD.
func ListRefs(repo string) (Refs, error) {
	r, err := gogit.PlainOpen(repo)
	if err != nil {
		return Refs{}, err
	}
	head, err := r.Head()
	if err != nil {
		return Refs{}, err
	}
	out := Refs{Branches: []string{}, Tags: []string{}, Commits: []RefCommit{}}
	if head.Name().IsBranch() {
		out.Head = head.Name().Short()
	} else {
		out.Head = head.Hash().String()[:12]
	}
	iter, err := r.References()
	if err != nil {
		return Refs{}, err
	}
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		switch {
		case ref.Name().IsBranch():
			out.Branches = append(out.Branches, ref.Name().Short())
		case ref.Name().IsTag():
			out.Tags = append(out.Tags, ref.Name().Short())
		}
		return nil
	})
	if err != nil {
		return Refs{}, err
	}
	sortStrings(out.Branches)
	sortStrings(out.Tags)
	log, err := r.Log(&gogit.LogOptions{From: head.Hash()})
	if err != nil {
		return Refs{}, err
	}
	err = log.ForEach(func(c *object.Commit) error {
		if len(out.Commits) >= 20 {
			return errStop
		}
		subject := c.Message
		if i := strings.IndexByte(subject, '\n'); i >= 0 {
			subject = subject[:i]
		}
		out.Commits = append(out.Commits, RefCommit{Hash: c.Hash.String(), Subject: strings.TrimSpace(subject)})
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return Refs{}, err
	}
	return out, nil
}

// errStop ends a go-git log walk. ForEach treats any error as the end, and
// a sentinel keeps a real read error distinguishable at the call.
var errStop = errors.New("stop")

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
