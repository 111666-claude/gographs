package render

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/siggy/gographs/internal/fixtures"
	"github.com/siggy/gographs/pkg/graph"
)

// memCache implements Cache for tests.
type memCache struct {
	svg map[string]string
	dot map[string]string
}

func newMemCache() *memCache {
	return &memCache{svg: map[string]string{}, dot: map[string]string{}}
}

func key(repo string, cluster bool, theme string) string {
	k := repo
	if cluster {
		k += "+c"
	} else {
		k += "+p"
	}
	if theme != "" {
		k += "+" + theme
	}
	return k
}

func (m *memCache) GetSVG(repo string, cluster bool, theme string) (string, error) {
	v, ok := m.svg[key(repo, cluster, theme)]
	if !ok {
		return "", errors.New("miss")
	}
	return v, nil
}

func (m *memCache) SetSVG(repo string, cluster bool, theme, svg string) {
	m.svg[key(repo, cluster, theme)] = svg
}

func (m *memCache) DeleteSVG(repo string, cluster bool, theme string) {
	delete(m.svg, key(repo, cluster, theme))
}

func (m *memCache) GetDOT(repo string, cluster bool, theme string) (string, error) {
	v, ok := m.dot[key(repo, cluster, theme)]
	if !ok {
		return "", errors.New("miss")
	}
	return v, nil
}

func (m *memCache) SetDOT(repo string, cluster bool, theme, dot string) {
	m.dot[key(repo, cluster, theme)] = dot
}

func (m *memCache) DeleteDOT(repo string, cluster bool, theme string) {
	delete(m.dot, key(repo, cluster, theme))
}

// fakeSource records requests and returns a canned DOT document.
type fakeSource struct {
	dot   string
	err   error
	calls []graph.Post
}

func (f *fakeSource) Get(repo string, cluster bool, theme string) (string, error) {
	f.calls = append(f.calls, graph.Post{Repo: repo, Cluster: cluster, Theme: theme})
	return f.dot, f.err
}

func TestToDOTCachesPerTheme(t *testing.T) {
	src := &fakeSource{dot: fixtures.DOT}
	c := newMemCache()

	for _, theme := range []Theme{ThemeDark, ThemeLight, ThemeDefault} {
		got, err := ToDOT(src, c, fixtures.Repo, true, theme)
		if err != nil {
			t.Fatalf("ToDOT(%q): %v", theme, err)
		}
		if theme.IsZero() {
			if got != fixtures.DOT {
				t.Errorf("default theme output differs from goda DOT")
			}
		} else if !strings.Contains(got, "// gographs-theme: "+theme.String()) {
			t.Errorf("ToDOT(%q) missing theme marker:\n%s", theme, got)
		}
		if _, ok := c.dot[key(fixtures.Repo, true, theme.String())]; !ok {
			t.Errorf("ToDOT(%q) did not cache under its own key", theme)
		}
	}

	if len(src.calls) != 3 {
		t.Fatalf("graph source called %d times, want 3 (one per theme)", len(src.calls))
	}
	for i, want := range []string{"dark", "light", ""} {
		if src.calls[i].Theme != want {
			t.Errorf("call %d sent theme %q, want %q", i, src.calls[i].Theme, want)
		}
	}

	// A second dark request must be a cache hit with no new source call.
	before := len(src.calls)
	got, err := ToDOT(src, c, fixtures.Repo, true, ThemeDark)
	if err != nil {
		t.Fatal(err)
	}
	if len(src.calls) != before {
		t.Errorf("cache hit still called the graph source")
	}
	if !strings.Contains(got, "// gographs-theme: dark") {
		t.Errorf("cached dark DOT lost its theme")
	}

	// The plain (non-cluster) graph is a distinct entry from the clustered one.
	if _, err := ToDOT(src, c, fixtures.Repo, false, ThemeDark); err != nil {
		t.Fatal(err)
	}
	if len(src.calls) != before+1 {
		t.Errorf("non-cluster theme request was served from the cluster cache entry")
	}
}

func TestToDOTRecolorsHistoricalEntry(t *testing.T) {
	src := &fakeSource{dot: fixtures.DOT}
	c := newMemCache()
	// An entry written before themes existed sits under the theme-less key.
	c.SetDOT(fixtures.Repo, true, "", fixtures.DOT)

	// A default request is still an exact historical hit.
	plain, err := ToDOT(src, c, fixtures.Repo, true, ThemeDefault)
	if err != nil {
		t.Fatalf("ToDOT: %v", err)
	}
	if plain != fixtures.DOT {
		t.Errorf("default request did not return the historical entry unchanged")
	}
	if len(src.calls) != 0 {
		t.Errorf("default request regenerated the graph")
	}

	// A themed request uses the historical entry as its source and caches the
	// recolored result under the themed key, instead of regenerating.
	got, err := ToDOT(src, c, fixtures.Repo, true, ThemeDark)
	if err != nil {
		t.Fatalf("ToDOT: %v", err)
	}
	if len(src.calls) != 0 {
		t.Errorf("historical entry should have been recolored, not regenerated")
	}
	if !strings.Contains(got, "// gographs-theme: dark") {
		t.Errorf("historical entry was not recolored:\n%s", got)
	}
	if cached, _ := c.GetDOT(fixtures.Repo, true, "dark"); cached != got {
		t.Errorf("recolored entry was not cached under the themed key")
	}
}

func TestToDOTEvictsInvalidEntries(t *testing.T) {
	tests := []struct {
		name string
		seed string
	}{
		{"empty", ""},
		{"empty graph", fixtures.EmptyDOT},
		{"other theme", "// gographs-theme: light\n" + fixtures.DOT},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &fakeSource{dot: fixtures.DOT}
			c := newMemCache()
			c.SetDOT(fixtures.Repo, true, "dark", tt.seed)

			got, err := ToDOT(src, c, fixtures.Repo, true, ThemeDark)
			if err != nil {
				t.Fatalf("ToDOT: %v", err)
			}
			if len(src.calls) != 1 {
				t.Errorf("source called %d times, want 1", len(src.calls))
			}
			if !strings.Contains(got, "// gographs-theme: dark") {
				t.Errorf("output is not dark themed:\n%s", got)
			}
			if cached, _ := c.GetDOT(fixtures.Repo, true, "dark"); cached != got {
				t.Errorf("invalid entry was not replaced in cache")
			}
		})
	}
}

func TestToDOTEmptyGraph(t *testing.T) {
	src := &fakeSource{dot: fixtures.EmptyDOT}
	c := newMemCache()

	_, err := ToDOT(src, c, fixtures.Repo, false, ThemeDark)
	if !errors.Is(err, graph.ErrEmptyGraph) {
		t.Fatalf("ToDOT error = %v, want ErrEmptyGraph", err)
	}
	if len(c.dot) != 0 {
		t.Errorf("empty graph was cached: %v", c.dot)
	}
}

func TestToDOTSourceErrorNotCached(t *testing.T) {
	src := &fakeSource{err: errors.New("clone failed")}
	c := newMemCache()

	if _, err := ToDOT(src, c, fixtures.Repo, false, ThemeDark); err == nil {
		t.Fatal("ToDOT returned nil error")
	}
	if len(c.dot) != 0 {
		t.Errorf("error result was cached: %v", c.dot)
	}
}

func TestToSVGValidatesCachedSVG(t *testing.T) {
	if _, err := exec.LookPath("dot"); err != nil {
		t.Skip("graphviz 'dot' not installed")
	}

	src := &fakeSource{dot: fixtures.DOT}
	c := newMemCache()

	// A truncated entry must not be served.
	c.SetSVG(fixtures.Repo, false, "dark", "<svg><g>")
	got, err := ToSVG(src, c, fixtures.Repo, false, ThemeDark)
	if err != nil {
		t.Fatalf("ToSVG: %v", err)
	}
	if !isSVG(got) {
		t.Errorf("ToSVG returned an incomplete document:\n%s", got)
	}
	if len(src.calls) != 1 {
		t.Errorf("source called %d times, want 1", len(src.calls))
	}
}

func TestToSVGSurfacesGraphvizFailure(t *testing.T) {
	if _, err := exec.LookPath("dot"); err != nil {
		t.Skip("graphviz 'dot' not installed")
	}

	src := &fakeSource{dot: "digraph G {\n    \"a\" [label=\"a\";\n}\n"}
	c := newMemCache()

	if _, err := ToSVG(src, c, fixtures.Repo, false, ThemeDark); err == nil {
		t.Fatal("ToSVG with malformed DOT returned nil error")
	}
	if len(c.svg) != 0 {
		t.Errorf("failed SVG conversion was cached: %v", c.svg)
	}
}

// TestToSVGThemedAttributes renders the fixed graph with Graphviz and checks
// the resulting SVG attributes. It is skipped when Graphviz is unavailable.
func TestToSVGThemedAttributes(t *testing.T) {
	if _, err := exec.LookPath("dot"); err != nil {
		t.Skip("graphviz 'dot' not installed")
	}

	const (
		darkCanvas = `fill="#12161b"`
		darkText   = `fill="#e6edf3"`
	)

	tests := []struct {
		name    string
		theme   Theme
		want    []string
		notWant []string
	}{
		{
			name:  "dark",
			theme: ThemeDark,
			want: []string{
				darkCanvas,         // canvas + cluster background
				darkText,           // node, cluster, and edge-label text
				`stroke="#8b949e"`, // cluster border
			},
			notWant: []string{
				`fill="white"`,
				`fill="black"`,
			},
		},
		{
			name:  "light",
			theme: ThemeLight,
			want: []string{
				`fill="#ffffff"`,
				`fill="#1b1f23"`,
			},
			notWant: []string{darkCanvas},
		},
		{
			name:  "default",
			theme: ThemeDefault,
			want: []string{
				`fill="white"`,   // Graphviz's untouched background
				`stroke="black"`, // Graphviz's untouched borders
			},
			notWant: []string{darkCanvas, darkText},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &fakeSource{dot: fixtures.DOT}
			c := newMemCache()
			svg, err := ToSVG(src, c, fixtures.Repo, true, tt.theme)
			if err != nil {
				t.Fatalf("ToSVG: %v", err)
			}
			for _, w := range tt.want {
				if !strings.Contains(svg, w) {
					t.Errorf("SVG missing %s", w)
				}
			}
			for _, nw := range tt.notWant {
				if strings.Contains(svg, nw) {
					t.Errorf("SVG unexpectedly contains %s", nw)
				}
			}

			// Per-package node colors must remain legible on a dark canvas:
			// goda's l=0.3 colors are lightened but stay saturated.
			if tt.theme == ThemeDark {
				for _, want := range []string{"#ae8fce", "#96ce8f"} {
					if !strings.Contains(svg, want) {
						t.Errorf("dark SVG missing lightened node color %s", want)
					}
				}
				for _, stale := range []string{"#4a0791", "#159107"} {
					if strings.Contains(svg, stale) {
						t.Errorf("dark SVG still contains unlightened color %s", stale)
					}
				}
			}
		})
	}
}
