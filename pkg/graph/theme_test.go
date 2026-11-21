package graph

import (
	"errors"
	"strings"
	"testing"

	"github.com/siggy/gographs/internal/fixtures"
)

func TestParseTheme(t *testing.T) {
	tests := []struct {
		in      string
		want    Theme
		wantErr bool
	}{
		{in: "", want: ThemeDefault},
		{in: string(ThemeLight), want: ThemeLight},
		{in: string(ThemeDark), want: ThemeDark},
		{in: "DARK", want: ThemeDark},
		{in: " dark ", want: ThemeDark},
		{in: "solarized", wantErr: true},
		{in: "default", wantErr: true},
		{in: `dark"];graph[bgcolor="red`, wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParseTheme(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseTheme(%q) = %q, want error", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseTheme(%q) unexpected error: %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("ParseTheme(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestApplyDOTDefaultIsByteForByte(t *testing.T) {
	for _, theme := range []Theme{ThemeDefault} {
		got, err := ApplyDOT(fixtures.DOT, theme)
		if err != nil {
			t.Fatalf("ApplyDOT(%q): %v", theme, err)
		}
		if got != fixtures.DOT {
			t.Errorf("default theme changed the document:\n--- got ---\n%s\n--- want ---\n%s", got, fixtures.DOT)
		}
	}
}

func TestApplyDOTDark(t *testing.T) {
	got, err := ApplyDOT(fixtures.DOT, ThemeDark)
	if err != nil {
		t.Fatalf("ApplyDOT: %v", err)
	}

	want := []string{
		"// gographs-theme: dark\n",
		`graph [bgcolor="#12161b" fontcolor="#e6edf3" pencolor="#8b949e"];`,
		`node [fontcolor="#e6edf3" color="#e6edf3"];`,
		`edge [fontcolor="#e6edf3" color="#8b949e"];`,
		// goda's dark colors are blended toward white but keep their hue.
		`color="#ae8fceb2"`,
		`color="#96ce8fb2"`,
		// Labels and tooltips are untouched, even when they contain -> or [.
		`label="a\l12 / 0.4KB\l"`,
		`tooltip="example.com/demo/a -> example.com/demo/b"`,
		// The original defaults and cluster structure remain.
		`node [penwidth=2 fontsize=10 shape=rectangle target="_graphviz"];`,
		`subgraph "cluster_example.com/demo" {`,
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("dark DOT missing %q\n--- got ---\n%s", w, got)
		}
	}

	// The injected defaults appear only once, at the top-level graph. Cluster
	// subgraphs inherit them; repeating them would repaint cluster borders.
	if n := strings.Count(got, "graph [bgcolor="); n != 1 {
		t.Errorf("graph defaults inserted %d times, want 1", n)
	}
	if n := strings.Count(got, "node [fontcolor="); n != 1 {
		t.Errorf("node defaults inserted %d times, want 1", n)
	}
	if n := strings.Count(got, "edge [fontcolor="); n != 1 {
		t.Errorf("edge defaults inserted %d times, want 1", n)
	}
	// The source graph's own defaults must still follow the injected ones so
	// they merge rather than replace them.
	if strings.Index(got, "graph [bgcolor=") > strings.Index(got, "node [penwidth=2") {
		t.Errorf("themed defaults must precede goda's own defaults")
	}
}

func TestApplyDOTLight(t *testing.T) {
	got, err := ApplyDOT(fixtures.DOT, ThemeLight)
	if err != nil {
		t.Fatalf("ApplyDOT: %v", err)
	}
	for _, w := range []string{
		"// gographs-theme: light\n",
		`bgcolor="#ffffff"`,
		`fontcolor="#1b1f23"`,
	} {
		if !strings.Contains(got, w) {
			t.Errorf("light DOT missing %q", w)
		}
	}
	// goda's colors already have enough contrast on white, so light does not
	// rewrite them.
	if strings.Contains(got, "#ae8fce") {
		t.Errorf("light theme should not lighten node colors:\n%s", got)
	}
}

func TestApplyDOTEmptyGraph(t *testing.T) {
	if _, err := ApplyDOT(fixtures.EmptyDOT, ThemeDark); !errors.Is(err, ErrEmptyGraph) {
		t.Fatalf("ApplyDOT(empty) error = %v, want ErrEmptyGraph", err)
	}
	// The default theme also refuses to pass through an empty graph so callers
	// never cache a blank diagram.
	if _, err := ApplyDOT(fixtures.EmptyDOT, ThemeDefault); !errors.Is(err, ErrEmptyGraph) {
		t.Fatalf("ApplyDOT(empty, default) error = %v, want ErrEmptyGraph", err)
	}
}

func TestApplyDOTIdempotent(t *testing.T) {
	once, err := ApplyDOT(fixtures.DOT, ThemeDark)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := ApplyDOT(once, ThemeDark)
	if err != nil {
		t.Fatalf("re-applying the same theme: %v", err)
	}
	if once != twice {
		t.Errorf("applying the same theme twice changed the document")
	}

	if _, err := ApplyDOT(once, ThemeLight); err == nil {
		t.Errorf("applying a different theme to themed DOT should fail")
	}
}

func TestApplyDOTMaliciousValuesAreQuoted(t *testing.T) {
	// A DOT document whose attribute values look like an injection attempt.
	in := `digraph G {
    "x" [label="x" color="#4a0791b2\n"];graph[bgcolor="red"];
    "y" -> "z" [color="a\"b"];
}
`
	out, err := ApplyDOT(in, ThemeDark)
	if err != nil {
		t.Fatalf("ApplyDOT: %v", err)
	}
	// The attribute value is only rewritten when it is a clean hex literal;
	// anything else passes through inside its quotes, and raw newlines inside
	// strings are escaped so they cannot end the statement early.
	if strings.Contains(out, "#4a0791b2\n\"];graph[bgcolor=\"red\"]") {
		t.Errorf("string newline was not escaped:\n%s", out)
	}
	if !strings.Contains(out, "#4a0791b2\\n") {
		t.Errorf("expected the newline inside the value to be escaped:\n%s", out)
	}
}

func TestHasNodes(t *testing.T) {
	if !HasNodes(fixtures.DOT) {
		t.Error("HasNodes(fixture) = false, want true")
	}
	if HasNodes(fixtures.EmptyDOT) {
		t.Error("HasNodes(empty) = true, want false")
	}
}

func TestNormalizeDOT(t *testing.T) {
	dark, err := ApplyDOT(fixtures.DOT, ThemeDark)
	if err != nil {
		t.Fatal(err)
	}

	// errThemeMismatch matches the "cached dot has theme ..." error without
	// pinning its exact wording.
	var errThemeMismatch = errors.New("theme mismatch sentinel")

	tests := []struct {
		name    string
		dot     string
		theme   Theme
		wantOK  bool
		wantErr error
	}{
		{"unthemed for default", fixtures.DOT, ThemeDefault, true, nil},
		{"unthemed for dark", fixtures.DOT, ThemeDark, true, nil},
		{"dark for dark", dark, ThemeDark, true, nil},
		{"dark for light", dark, ThemeLight, false, errThemeMismatch},
		{"empty", fixtures.EmptyDOT, ThemeDark, false, ErrEmptyGraph},
		{"blank", "", ThemeDark, false, ErrEmptyGraph},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := NormalizeDOT(tt.dot, tt.theme)
			switch tt.wantErr {
			case nil:
				if err != nil {
					t.Fatalf("NormalizeDOT error = %v", err)
				}
			case errThemeMismatch:
				if err == nil || errors.Is(err, ErrEmptyGraph) {
					t.Fatalf("NormalizeDOT error = %v, want theme mismatch", err)
				}
			default:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("NormalizeDOT error = %v, want %v", err, tt.wantErr)
				}
			}
			if err == nil && ok != tt.wantOK {
				t.Errorf("NormalizeDOT ok = %t, want %t", ok, tt.wantOK)
			}
		})
	}
}

func TestRecolorDOT(t *testing.T) {
	// Unthemed historical cache entries are recolored on read.
	got, err := RecolorDOT(fixtures.DOT, ThemeDark)
	if err != nil {
		t.Fatalf("RecolorDOT: %v", err)
	}
	if DOTTheme(got) != ThemeDark {
		t.Errorf("recolored DOT theme = %q, want dark", DOTTheme(got))
	}

	// Already-themed entries are returned unchanged.
	again, err := RecolorDOT(got, ThemeDark)
	if err != nil {
		t.Fatalf("RecolorDOT: %v", err)
	}
	if again != got {
		t.Error("RecolorDOT changed an already-themed document")
	}
}

func TestLightenHex(t *testing.T) {
	tests := map[string]string{
		"#000000":   "#8c8c8c",
		"#ffffff":   "#ffffff",
		"#4a0791b2": "#ae8fceb2",
		"#4a0791":   "#ae8fce",
		"red":       "red",
		"#12345":    "#12345",
		"#zzzzzz":   "#zzzzzz",
	}
	for in, want := range tests {
		if got := lightenHex(in); got != want {
			t.Errorf("lightenHex(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestThemeString(t *testing.T) {
	if got := ThemeDefault.String(); got != "" {
		t.Errorf("default String() = %q, want empty", got)
	}
	if got := ThemeDark.String(); got != "dark" {
		t.Errorf("dark String() = %q, want dark", got)
	}
}

// TestApplyDOTRealGodaSample exercises the rewriter against DOT captured from a
// real goda run, so an upstream format change that breaks attribute injection
// fails here rather than in production.
func TestApplyDOTRealGodaSample(t *testing.T) {
	if !HasNodes(fixtures.GodaSample) {
		t.Fatal("real goda sample has no nodes")
	}

	dark, err := ApplyDOT(fixtures.GodaSample, ThemeDark)
	if err != nil {
		t.Fatalf("ApplyDOT(real sample): %v", err)
	}
	for _, want := range []string{
		`graph [bgcolor="#12161b"`,
		`node [fontcolor="#e6edf3"`,
		`edge [fontcolor="#e6edf3"`,
		`subgraph "cluster_`,
	} {
		if !strings.Contains(dark, want) {
			t.Errorf("themed real sample missing %q", want)
		}
	}
	// Every goda node color must have been lightened.
	for _, stale := range []string{"#4a0791", "#159107", "#914707", "#749107", "#8d9107"} {
		if strings.Contains(dark, stale+"b2") {
			t.Errorf("real sample still has unlightened color %s", stale)
		}
	}
	// Labels and tooltips that contain graph punctuation survive intact.
	if !strings.Contains(dark, `tooltip="github.com/siggy/gographs/pkg/render -> github.com/siggy/gographs/pkg/graph"`) {
		t.Error("edge tooltip was mangled")
	}
	// The default theme stays byte-identical on real input too.
	plain, err := ApplyDOT(fixtures.GodaSample, ThemeDefault)
	if err != nil {
		t.Fatal(err)
	}
	if plain != fixtures.GodaSample {
		t.Error("default theme altered the real goda sample")
	}
}
