package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/siggy/gographs/internal/fixtures"
	"github.com/siggy/gographs/pkg/graph"
	"github.com/siggy/gographs/pkg/render"
	"github.com/sirupsen/logrus"
)

// fakeStore implements graphDocStore in memory.
type fakeStore struct {
	svg   map[string]string
	dot   map[string]string
	clear []string
}

func newFakeStore() *fakeStore {
	return &fakeStore{svg: map[string]string{}, dot: map[string]string{}}
}

func fkey(repo string, cluster bool, theme string) string {
	k := repo
	if cluster {
		k += "+c"
	}
	if theme != "" {
		k += "+" + theme
	}
	return k
}

func (s *fakeStore) GetSVG(repo string, cluster bool, theme string) (string, error) {
	v, ok := s.svg[fkey(repo, cluster, theme)]
	if !ok {
		return "", errors.New("miss")
	}
	return v, nil
}
func (s *fakeStore) SetSVG(repo string, cluster bool, theme, svg string) {
	s.svg[fkey(repo, cluster, theme)] = svg
}
func (s *fakeStore) DeleteSVG(repo string, cluster bool, theme string) {
	delete(s.svg, fkey(repo, cluster, theme))
}
func (s *fakeStore) GetDOT(repo string, cluster bool, theme string) (string, error) {
	v, ok := s.dot[fkey(repo, cluster, theme)]
	if !ok {
		return "", errors.New("miss")
	}
	return v, nil
}
func (s *fakeStore) SetDOT(repo string, cluster bool, theme, dot string) {
	s.dot[fkey(repo, cluster, theme)] = dot
}
func (s *fakeStore) DeleteDOT(repo string, cluster bool, theme string) {
	delete(s.dot, fkey(repo, cluster, theme))
}
func (s *fakeStore) Clear(repo string) error {
	s.clear = append(s.clear, repo)
	return nil
}
func (s *fakeStore) RepoScoreIncr(string) {}

// fakeSource returns fixed DOT and records the theme it was asked for.
type fakeSource struct {
	dot      string
	err      error
	themes   []string
	clusters []bool
}

func (f *fakeSource) Get(repo string, cluster bool, theme string) (string, error) {
	f.themes = append(f.themes, theme)
	f.clusters = append(f.clusters, cluster)
	return f.dot, f.err
}

func serve(t *testing.T, src render.DOTSource, store graphDocStore, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	handleGraph(rec, req, "/graph", src, store, logrus.NewEntry(logrus.New()))
	return rec
}

func TestHandleGraphThemeParameter(t *testing.T) {
	const (
		darkMarker  = "// gographs-theme: dark"
		lightMarker = "// gographs-theme: light"
	)

	tests := []struct {
		target     string
		wantStatus int
		wantTheme  string
		wantBody   string
	}{
		{"/graph/example.com/demo.dot", http.StatusOK, "", fixtures.DOT},
		{"/graph/example.com/demo.dot?theme=dark", http.StatusOK, "dark", darkMarker},
		{"/graph/example.com/demo.dot?theme=light", http.StatusOK, "light", lightMarker},
		{"/graph/example.com/demo.dot?theme=DARK", http.StatusOK, "dark", darkMarker},
		{"/graph/example.com/demo.dot?theme=neon", http.StatusBadRequest, "", ""},
		{"/graph/example.com/demo.dot?theme=dark%22%5D%3B+graph%5Bbgcolor%3Dred", http.StatusBadRequest, "", ""},
		{"/graph/example.com/demo.svg?theme=neon", http.StatusBadRequest, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			src := &fakeSource{dot: fixtures.DOT}
			store := newFakeStore()
			rec := serve(t, src, store, tt.target)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus != http.StatusOK {
				if len(src.themes) != 0 {
					t.Errorf("invalid theme reached the graph source: %v", src.themes)
				}
				return
			}
			if got := rec.Header().Get("X-Gographs-Theme"); got != tt.wantTheme {
				t.Errorf("X-Gographs-Theme = %q, want %q", got, tt.wantTheme)
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("body missing %q:\n%s", tt.wantBody, rec.Body.String())
			}
			if len(src.themes) != 1 || src.themes[0] != tt.wantTheme {
				t.Errorf("graph source themes = %v, want [%q]", src.themes, tt.wantTheme)
			}
		})
	}
}

func TestHandleGraphCacheIsolation(t *testing.T) {
	src := &fakeSource{dot: fixtures.DOT}
	store := newFakeStore()

	paths := []string{
		"/graph/example.com/demo.dot",
		"/graph/example.com/demo.dot?theme=light",
		"/graph/example.com/demo.dot?theme=dark",
		"/graph/example.com/demo.dot?cluster=true&theme=dark",
	}
	for _, p := range paths {
		if rec := serve(t, src, store, p); rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", p, rec.Code)
		}
	}

	// Each theme/cluster combination is cached under its own key, with exactly
	// the document that combination serves.
	want := map[string]string{
		"example.com/demo":        "", // default: unthemed
		"example.com/demo+light":  "// gographs-theme: light",
		"example.com/demo+dark":   "// gographs-theme: dark",
		"example.com/demo+c+dark": "// gographs-theme: dark",
	}
	if len(store.dot) != len(want) {
		t.Errorf("cache has %d DOT entries, want %d: %v", len(store.dot), len(want), store.dot)
	}
	for k, marker := range want {
		doc, ok := store.dot[k]
		if !ok {
			t.Errorf("missing cache entry %q", k)
			continue
		}
		if marker == "" {
			if doc != fixtures.DOT {
				t.Errorf("%q was not stored unthemed", k)
			}
			continue
		}
		if !strings.Contains(doc, marker) {
			t.Errorf("%q missing marker %q", k, marker)
		}
	}

	// Repeating a request is a cache hit, not a regeneration.
	before := len(src.themes)
	if rec := serve(t, src, store, "/graph/example.com/demo.dot?theme=dark"); rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
	if len(src.themes) != before {
		t.Errorf("repeated dark request called the graph source again")
	}

	// The default request must still serve the unthemed document.
	rec := serve(t, src, store, "/graph/example.com/demo.dot")
	if strings.Contains(rec.Body.String(), "gographs-theme") {
		t.Errorf("default request served a themed document:\n%s", rec.Body.String())
	}
}

func TestHandleGraphRefreshClearsRepo(t *testing.T) {
	src := &fakeSource{dot: fixtures.DOT}
	store := newFakeStore()

	req := httptest.NewRequest(http.MethodPost, "/graph/example.com/demo.dot?theme=dark", nil)
	rec := httptest.NewRecorder()
	handleGraph(rec, req, "/graph", src, store, logrus.NewEntry(logrus.New()))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(store.clear) != 1 || store.clear[0] != "example.com/demo" {
		t.Errorf("Clear calls = %v, want [example.com/demo]", store.clear)
	}
}

func TestHandleGraphEmptyGraph(t *testing.T) {
	src := &fakeSource{dot: fixtures.EmptyDOT}
	rec := serve(t, src, newFakeStore(), "/graph/example.com/demo.dot?theme=dark")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestHandleGraphGraphvizFailure(t *testing.T) {
	// DOT that Graphviz will reject: an unterminated node statement.
	src := &fakeSource{dot: "digraph G {\n    \"a\" [label=\"a\";\n}\n"}
	rec := serve(t, src, newFakeStore(), "/graph/example.com/demo.svg")

	if _, err := render.ToDOT(src, newFakeStore(), fixtures.Repo, false, render.ThemeDefault); err == nil {
		t.Skip("graphviz accepted malformed input")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(rec.Body.String(), "Failed to render") {
		t.Errorf("body %q does not report the failure", rec.Body.String())
	}
}

func TestHandleGraphRequiresSuffixAndRepo(t *testing.T) {
	src := &fakeSource{dot: fixtures.DOT}
	store := newFakeStore()

	for _, target := range []string{
		"/graph/example.com/demo",
		"/graph/.dot",
	} {
		rec := serve(t, src, store, target)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want %d", target, rec.Code, http.StatusBadRequest)
		}
	}
	if len(src.themes) != 0 {
		t.Errorf("invalid requests reached the graph source: %v", src.themes)
	}
}

// Ensure graph.Client still satisfies the render.DOTSource interface.
var _ render.DOTSource = (*graph.Client)(nil)
