package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"html"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"citydiff/lib/git"
	"citydiff/lib/observe"
	"citydiff/lib/scene"
	"citydiff/lib/tour"
	"citydiff/view"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Card size is the pull-request image GitHub publishes: 1200×600. The
// crawlers that read og:image take a PNG of that size and do not render SVG.
const (
	cardWidth  = 1200
	cardHeight = 600
	cardPad    = 72
)

// The dark skin. Background, type, and the change colours are the ones in
// skins/dark/skin.json, so a thumb is the same night as the city it opens.
var (
	cardBG       = color.RGBA{0x09, 0x09, 0x0b, 0xff}
	cardFG       = color.RGBA{0xfa, 0xfa, 0xfa, 0xff}
	cardMuted    = color.RGBA{0xa1, 0xa1, 0xaa, 0xff}
	cardFaint    = color.RGBA{0x71, 0x71, 0x7a, 0xff}
	cardLine     = color.RGBA{0x3f, 0x3f, 0x46, 0xff}
	cardAdded    = color.RGBA{0x71, 0xd0, 0x83, 0xff}
	cardRemoved  = color.RGBA{0xe5, 0x48, 0x4d, 0xff}
	cardModified = color.RGBA{0xff, 0xc5, 0x3d, 0xff}
)

const cardFormat = "v2"

var longHash = regexp.MustCompile(`[0-9a-fA-F]{12,}`)

var (
	fontOnce    sync.Once
	fontRegular *opentype.Font
	fontMedium  *opentype.Font
	fontMono    *opentype.Font
	fontErr     error
)

func cardFonts() error {
	fontOnce.Do(func() {
		fontRegular, fontErr = opentype.Parse(goregular.TTF)
		if fontErr != nil {
			return
		}
		fontMedium, fontErr = opentype.Parse(gomedium.TTF)
		if fontErr != nil {
			return
		}
		fontMono, fontErr = opentype.Parse(gomono.TTF)
	})
	return fontErr
}

// cardView is one link's card: the lines the image draws, and the commits
// those lines were counted from. Note is the description crawlers read; it
// is not drawn.
type cardView struct {
	Kind     string
	Eyebrow  string
	Title    string
	Subtitle string
	Note     string
	Groups   []cardGroup
	Quiet    string
	Head     string
	Left     string
	Right    string
	Range    string
}

// cardGroup is one noun on the stat line. Count is the overview total.
// Parts are the diff, in the viewer's order: added, changed, deleted.
type cardGroup struct {
	Label string
	Count int
	Parts []cardPart
}

type cardPart struct {
	Text  string
	Color color.RGBA
}

// cardDir is the cache beside the clone. The clone stays a git checkout;
// a card is a sibling directory, the clone path plus ".cards".
func cardDir(repo string) string {
	return strings.TrimRight(repo, "/") + ".cards"
}

func (v cardView) key() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		cardFormat, v.Kind, v.Head, v.Left, v.Right, v.Eyebrow, v.Title, v.Subtitle,
	}, "\n")))
	return hex.EncodeToString(sum[:])
}

func (v cardView) sceneRange() string {
	if v.Kind == "overview" {
		return ""
	}
	return v.Range
}

func (v cardView) description() string {
	if v.Note != "" {
		return v.Note
	}
	if v.Kind == "range" {
		return v.Eyebrow
	}
	if v.Subtitle != "" {
		return v.Subtitle
	}
	return v.Title
}

func (h *hub) cardView(obs observe.Obs, q url.Values) (cardView, error) {
	name := q.Get("tour")
	if name != "" && !remoteTour(name) {
		return h.tourCard(obs, name)
	}
	if commitRange := strings.TrimSpace(q.Get("range")); commitRange != "" {
		return h.rangeCard(obs, commitRange)
	}
	return h.overviewCard(obs)
}

func (h *hub) overviewCard(obs observe.Obs) (cardView, error) {
	hash, branch, err := observe.Head(obs.Repo)
	if err != nil {
		return cardView{}, err
	}
	label := branch
	if label == "" {
		label = short(hash)
	}
	return cardView{
		Kind:     "overview",
		Eyebrow:  "citydiff",
		Title:    repoLabel(obs.Remote),
		Subtitle: label,
		Head:     hash,
	}, nil
}

func (h *hub) rangeCard(obs observe.Obs, commitRange string) (cardView, error) {
	left, right, err := git.Revs(obs.Repo, commitRange)
	if err != nil {
		return cardView{}, err
	}
	return cardView{
		Kind:    "range",
		Eyebrow: repoLabel(obs.Remote),
		Title:   shortSpan(commitRange),
		Left:    left,
		Right:   right,
		Range:   commitRange,
	}, nil
}

func (h *hub) tourCard(obs observe.Obs, name string) (cardView, error) {
	raw, err := h.store.Tour(obs.ID, name)
	if err != nil {
		return cardView{}, cardMiss(err.Error())
	}
	script, err := tour.Parse(bytes.NewReader(raw))
	if err != nil {
		return cardView{}, err
	}
	if strings.TrimSpace(script.Range) == "" {
		return cardView{}, errorsCard("tour has no range")
	}
	left, right, err := git.Revs(obs.Repo, script.Range)
	if err != nil {
		return cardView{}, err
	}
	title := strings.TrimSpace(script.Title)
	if title == "" {
		title = "Tour"
	}
	n := len(script.Steps)
	sub := shortSpan(script.Range)
	switch n {
	case 1:
		sub += " · 1 step"
	default:
		sub += " · " + strconv.Itoa(n) + " steps"
	}
	return cardView{
		Kind:     "tour",
		Eyebrow:  repoLabel(obs.Remote),
		Title:    title,
		Subtitle: sub,
		Note:     tourNote(script),
		Left:     left,
		Right:    right,
		Range:    script.Range,
	}, nil
}

type cardMiss string

func (e cardMiss) Error() string { return string(e) }

func errorsCard(msg string) error { return cardMiss(msg) }

// repoLabel is owner/repository, the sidebar title. The host is not part of it.
func repoLabel(remote string) string {
	u, err := url.Parse(strings.TrimSpace(remote))
	if err != nil {
		return "repository"
	}
	parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	if len(parts) < 2 {
		return "repository"
	}
	repo := parts[len(parts)-1]
	if strings.HasSuffix(strings.ToLower(repo), ".git") {
		repo = repo[:len(repo)-4]
	}
	owner := parts[len(parts)-2]
	if owner == "" || repo == "" {
		return "repository"
	}
	return owner + "/" + repo
}

// shortSpan shortens each full hash in a range. A branch, a tag, and the
// dots stay as they were typed.
func shortSpan(s string) string {
	return longHash.ReplaceAllStringFunc(s, func(h string) string { return h[:10] })
}

func tourNote(script tour.Tour) string {
	for _, step := range script.Steps {
		if s := firstSentence(step.Note); s != "" {
			return s
		}
	}
	return ""
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.Index(s, "\n\n"); i >= 0 {
		s = s[:i]
	}
	s = strings.ReplaceAll(s, "\n", " ")
	for _, mark := range []string{"**", "__", "`", "*"} {
		s = strings.ReplaceAll(s, mark, "")
	}
	s = strings.Join(strings.Fields(s), " ")
	const max = 180
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max])) + "…"
}

func cardStats(sc scene.Scene) (groups []cardGroup, quiet string) {
	var pkgs, deps, decls, calls tally
	for _, pkg := range sc.Packages {
		if pkg.External || pkg.Synthetic {
			continue
		}
		pkgs.add(pkg.Change)
		for _, dep := range pkg.Deps {
			deps.add(dep.Change)
		}
		for _, entity := range pkg.Entities {
			if !declaredKind(entity.Kind) {
				continue
			}
			decls.add(entity.Change)
			for _, step := range entity.Calls {
				if step.Resolved {
					calls.add(step.Change)
				}
			}
		}
	}
	for _, item := range []struct {
		label string
		n     tally
	}{
		{"packages", pkgs},
		{"dependencies", deps},
		{"declarations", decls},
		{"calls", calls},
	} {
		if g, ok := item.n.group(item.label, sc.Diff); ok {
			groups = append(groups, g)
		}
	}
	if sc.Diff && len(groups) == 0 {
		quiet = "low level code change"
	}
	return groups, quiet
}

func declaredKind(kind string) bool {
	switch kind {
	case "type", "variable", "function", "method":
		return true
	default:
		return false
	}
}

type tally struct {
	added, removed, modified, total int
}

func (t *tally) add(change string) {
	t.total++
	switch change {
	case "added":
		t.added++
	case "removed":
		t.removed++
	case "modified":
		t.modified++
	}
}

func (t tally) group(label string, diff bool) (cardGroup, bool) {
	if !diff {
		if t.total == 0 {
			return cardGroup{}, false
		}
		return cardGroup{Label: label, Count: t.total}, true
	}
	g := cardGroup{Label: label}
	if t.added > 0 {
		g.Parts = append(g.Parts, cardPart{Text: "+" + strconv.Itoa(t.added), Color: cardAdded})
	}
	if t.modified > 0 {
		g.Parts = append(g.Parts, cardPart{Text: "~" + strconv.Itoa(t.modified), Color: cardModified})
	}
	if t.removed > 0 {
		g.Parts = append(g.Parts, cardPart{Text: "−" + strconv.Itoa(t.removed), Color: cardRemoved})
	}
	if len(g.Parts) == 0 {
		return cardGroup{}, false
	}
	return g, true
}

// cardCache runs one build per card file. A second request for the same
// file waits for that build instead of drawing again.
type cardCache struct {
	mu      sync.Mutex
	flights map[string]*cardFlight
}

type cardFlight struct {
	done chan struct{}
	png  []byte
	err  error
}

func (c *cardCache) do(key string, build func() ([]byte, error)) (png []byte, err error) {
	c.mu.Lock()
	if c.flights == nil {
		c.flights = map[string]*cardFlight{}
	}
	if f := c.flights[key]; f != nil {
		c.mu.Unlock()
		<-f.done
		return f.png, f.err
	}
	f := &cardFlight{done: make(chan struct{})}
	c.flights[key] = f
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		f.png, f.err = png, err
		delete(c.flights, key)
		close(f.done)
		c.mu.Unlock()
	}()
	return build()
}

func (h *hub) cardPNG(ctx context.Context, obs observe.Obs, q url.Values) ([]byte, error) {
	v, err := h.cardView(obs, q)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(cardDir(obs.Repo), v.key()+".png")
	if b, err := os.ReadFile(path); err == nil {
		return b, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return h.cards.do(path, func() ([]byte, error) {
		if b, err := os.ReadFile(path); err == nil {
			return b, nil
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		// A crawler that gives up still leaves the file for the next fetch.
		held, err := h.scenes.get(context.WithoutCancel(ctx), obs.Repo, v.sceneRange())
		if err != nil {
			return nil, err
		}
		v.Groups, v.Quiet = cardStats(held.snap.scene)
		png, err := renderCard(v)
		if err != nil {
			return nil, err
		}
		if err := writeCard(path, png); err != nil {
			return nil, err
		}
		return png, nil
	})
}

func writeCard(path string, png []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".card-*.png")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(png); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return os.Chmod(path, 0o644)
}

func (h *hub) card(w http.ResponseWriter, r *http.Request, obs observe.Obs) {
	png, err := h.cardPNG(r.Context(), obs, r.URL.Query())
	if err != nil {
		if _, miss := err.(cardMiss); miss {
			http.NotFound(w, r)
			return
		}
		problem(w, "range", err)
		return
	}
	writeBody(w, r, "image/png", "public, max-age=86400", png)
}

func (h *hub) page(w http.ResponseWriter, r *http.Request, obs observe.Obs) {
	raw, err := view.FS.ReadFile("index.html")
	if err != nil {
		http.Error(w, "page unavailable", http.StatusInternalServerError)
		return
	}
	if v, err := h.cardView(obs, r.URL.Query()); err == nil {
		raw = injectCard(raw, cardHead{
			Title:       v.Title,
			Description: v.description(),
			Page:        pageURL(r, obs.ID, pageQuery(r.URL.Query(), v.Kind)),
			Image:       cardImageURL(r, obs.ID, v),
		})
	}
	writeBody(w, r, "text/html; charset=utf-8", "no-store", raw)
}

type cardHead struct {
	Title, Description, Page, Image string
}

func pageQuery(q url.Values, kind string) string {
	out := url.Values{}
	switch kind {
	case "tour":
		if name := q.Get("tour"); name != "" {
			out.Set("tour", name)
		}
	case "range":
		if commitRange := q.Get("range"); commitRange != "" {
			out.Set("range", commitRange)
		}
	}
	return out.Encode()
}

func cardImageURL(r *http.Request, id string, v cardView) string {
	q := url.Values{}
	switch v.Kind {
	case "tour":
		if name := r.URL.Query().Get("tour"); name != "" {
			q.Set("tour", name)
		}
	case "range":
		if commitRange := r.URL.Query().Get("range"); commitRange != "" {
			q.Set("range", commitRange)
		}
	}
	q.Set("v", v.key()[:16])
	base := strings.TrimRight(pageURL(r, id, ""), "/")
	return base + "/card.png?" + q.Encode()
}

func injectCard(page []byte, meta cardHead) []byte {
	const marker = "<title>citydiff</title>"
	if !bytes.Contains(page, []byte(marker)) {
		return page
	}
	title := meta.Title
	if title == "" {
		title = "citydiff"
	}
	var b strings.Builder
	b.WriteString("<title>")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</title>\n")
	writeMeta := func(attr, key, val string) {
		if val == "" {
			return
		}
		b.WriteString("  <meta ")
		b.WriteString(attr)
		b.WriteString(`="`)
		b.WriteString(key)
		b.WriteString(`" content="`)
		b.WriteString(html.EscapeString(val))
		b.WriteString("\">\n")
	}
	writeMeta("name", "description", meta.Description)
	writeMeta("property", "og:type", "website")
	writeMeta("property", "og:site_name", "citydiff")
	writeMeta("property", "og:title", title)
	writeMeta("property", "og:description", meta.Description)
	writeMeta("property", "og:url", meta.Page)
	writeMeta("property", "og:image", meta.Image)
	writeMeta("property", "og:image:width", strconv.Itoa(cardWidth))
	writeMeta("property", "og:image:height", strconv.Itoa(cardHeight))
	writeMeta("property", "og:image:alt", meta.Description)
	writeMeta("name", "twitter:card", "summary_large_image")
	writeMeta("name", "twitter:title", title)
	writeMeta("name", "twitter:description", meta.Description)
	writeMeta("name", "twitter:image", meta.Image)
	return bytes.Replace(page, []byte(marker), []byte(strings.TrimRight(b.String(), "\n")), 1)
}

func writeBody(w http.ResponseWriter, r *http.Request, contentType, cache string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", cache)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

func renderCard(v cardView) ([]byte, error) {
	if err := cardFonts(); err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, cardWidth, cardHeight))
	draw.Draw(img, img.Bounds(), image.NewUniform(cardBG), image.Point{}, draw.Src)
	// A hairline in the skin's strong line, so the card keeps an edge.
	border := cardLine
	for x := 0; x < cardWidth; x++ {
		img.SetRGBA(x, 0, border)
		img.SetRGBA(x, cardHeight-1, border)
	}
	for y := 1; y < cardHeight-1; y++ {
		img.SetRGBA(0, y, border)
		img.SetRGBA(cardWidth-1, y, border)
	}

	eyebrow := newFace(fontRegular, 22)
	defer eyebrow.close()
	title := newFace(fontMedium, 52)
	defer title.close()
	titleSm := newFace(fontMedium, 36)
	defer titleSm.close()
	mono := newFace(fontMono, 44)
	defer mono.close()
	monoSm := newFace(fontMono, 32)
	defer monoSm.close()
	sub := newFace(fontRegular, 24)
	defer sub.close()
	label := newFace(fontRegular, 26)
	defer label.close()
	num := newFace(fontMedium, 26)
	defer num.close()

	width := cardWidth - 2*cardPad
	y := cardPad + eyebrow.ascent()
	eyebrowWidth := width
	if v.Kind != "overview" {
		const markText = "citydiff"
		markW := textWidth(eyebrow, markText)
		drawAt(img, eyebrow, cardWidth-cardPad-markW, y, cardFaint, markText)
		eyebrowWidth = width - markW - 28
	}
	drawAt(img, eyebrow, cardPad, y, cardMuted, fit(eyebrow, v.Eyebrow, eyebrowWidth))

	face := title
	lines := wrapLines(title, v.Title, width, 2)
	if len(lines) == 1 && !strings.Contains(v.Title, " ") {
		face = fittingFace(v.Title, width, title, titleSm, mono, monoSm)
		lines = []string{v.Title}
		if textWidth(face, v.Title) > width {
			lines = []string{fit(face, v.Title, width)}
		}
	}
	y += eyebrow.descent() + 36 + face.ascent()
	for i, line := range lines {
		if i > 0 {
			y += face.height()
		}
		drawAt(img, face, cardPad, y, cardFG, line)
	}
	if v.Subtitle != "" {
		y += face.descent() + 18 + sub.ascent()
		drawAt(img, sub, cardPad, y, cardMuted, fit(sub, v.Subtitle, width))
	}

	base := cardHeight - cardPad
	rows := statRows(label, num, v.Groups, width)
	if len(rows) == 0 && v.Quiet != "" {
		drawAt(img, label, cardPad, base, cardMuted, fit(label, v.Quiet, width))
	}
	rowGap := label.height() + 12
	top := base - (len(rows)-1)*rowGap
	for i, row := range rows {
		drawStatRow(img, label, num, cardPad, top+i*rowGap, row)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fittingFace picks the largest face that keeps a single token on one line.
// A hash range is monospaced. A branch range stays in the text face.
func fittingFace(text string, width int, title, titleSm, mono, monoSm *cardFace) *cardFace {
	faces := []*cardFace{title, titleSm}
	if hashRange(text) {
		faces = []*cardFace{mono, monoSm}
	}
	for _, face := range faces {
		if textWidth(face, text) <= width {
			return face
		}
	}
	return faces[len(faces)-1]
}

func hashRange(s string) bool {
	parts := strings.Split(s, "..")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	return hashSide(parts[0]) && hashSide(parts[1])
}

func hashSide(s string) bool {
	s = strings.TrimPrefix(s, ".")
	if len(s) < 7 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

type cardFace struct {
	font.Face
}

func newFace(f *opentype.Font, size float64) *cardFace {
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		panic(err)
	}
	return &cardFace{Face: face}
}

func (f *cardFace) close() {
	if c, ok := f.Face.(interface{ Close() error }); ok {
		_ = c.Close()
	}
}

func (f *cardFace) ascent() int  { return f.Metrics().Ascent.Ceil() }
func (f *cardFace) descent() int { return f.Metrics().Descent.Ceil() }
func (f *cardFace) height() int  { return f.Metrics().Height.Ceil() }

func textWidth(f *cardFace, s string) int {
	return font.MeasureString(f.Face, s).Round()
}

func drawAt(img *image.RGBA, f *cardFace, x, y int, col color.Color, text string) {
	d := font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: f.Face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(text)
}

func fit(f *cardFace, text string, width int) string {
	if text == "" || textWidth(f, text) <= width {
		return text
	}
	r := []rune(text)
	for len(r) > 0 && textWidth(f, string(r)+"…") > width {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

func wrapLines(f *cardFace, text string, width, max int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	cur := ""
	for _, word := range words {
		next := word
		if cur != "" {
			next = cur + " " + word
		}
		if textWidth(f, next) <= width {
			cur = next
			continue
		}
		if cur != "" {
			lines = append(lines, cur)
		}
		if textWidth(f, word) > width {
			word = fit(f, word, width)
		}
		cur = word
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if max > 0 && len(lines) > max {
		lines = lines[:max]
		lines[max-1] = fit(f, lines[max-1], width)
	}
	return lines
}

func statRows(label, num *cardFace, groups []cardGroup, width int) [][]cardGroup {
	if len(groups) == 0 {
		return nil
	}
	if rowWidth(label, num, groups) <= width {
		return [][]cardGroup{groups}
	}
	// Two rows of similar width. A greedy fill left one noun alone on the
	// second line, which read as a leftover rather than a line of stats.
	best := 1
	bestGap := math.MaxInt
	for i := 1; i < len(groups); i++ {
		gap := rowWidth(label, num, groups[:i]) - rowWidth(label, num, groups[i:])
		if gap < 0 {
			gap = -gap
		}
		if gap < bestGap {
			bestGap = gap
			best = i
		}
	}
	return [][]cardGroup{groups[:best], groups[best:]}
}

func rowWidth(label, num *cardFace, groups []cardGroup) int {
	w := 0
	for i, g := range groups {
		if i > 0 {
			w += 36
		}
		w += groupWidth(label, num, g)
	}
	return w
}

func groupWidth(label, num *cardFace, g cardGroup) int {
	w := textWidth(label, g.Label)
	if len(g.Parts) == 0 {
		return w + 12 + textWidth(num, strconv.Itoa(g.Count))
	}
	for _, part := range g.Parts {
		w += 14 + textWidth(num, part.Text)
	}
	return w
}

func drawStatRow(img *image.RGBA, label, num *cardFace, x, y int, groups []cardGroup) {
	for i, g := range groups {
		if i > 0 {
			x += 36
		}
		drawAt(img, label, x, y, cardMuted, g.Label)
		x += textWidth(label, g.Label)
		if len(g.Parts) == 0 {
			x += 12
			drawAt(img, num, x, y, cardFG, strconv.Itoa(g.Count))
			x += textWidth(num, strconv.Itoa(g.Count))
			continue
		}
		for _, part := range g.Parts {
			x += 14
			drawAt(img, num, x, y, part.Color, part.Text)
			x += textWidth(num, part.Text)
		}
	}
}
