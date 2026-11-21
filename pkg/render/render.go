package render

// This package takes GoLang repos as input and outputs SVG and DOT files:
//
// 1. repo => dot
//    curl --data '{"repo":"github.com/siggy/gographs","cluster":true}' -X POST [graph-addr]/graph
// 2. dot => svg
//    echo "..." | \
//      dot -Tsvg \
//      -Gfontname=Roboto,Arial,sans-serif \
//      -Nfontname=Roboto,Arial,sans-serif \
//      -Efontname=Roboto,Arial,sans-serif \
//      -o graph2.svg
//
// Nested control-flow accommodates caching:
//
// ToSVG(repo) {
//   ToDOT(repo) {} => DOT
//   dotToSVG(DOT) {} => SVG
// } => SVG
//
// Both documents are cached per repo, cluster flag, and theme.

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/siggy/gographs/pkg/graph"
	log "github.com/sirupsen/logrus"
)

// Theme is the palette used for rendering. It is an alias so callers can use
// render.Theme and the graph package interchangeably.
type Theme = graph.Theme

const (
	ThemeDefault = graph.ThemeDefault
	ThemeLight   = graph.ThemeLight
	ThemeDark    = graph.ThemeDark
)

// ParseTheme validates an untrusted theme query parameter.
func ParseTheme(s string) (Theme, error) {
	return graph.ParseTheme(s)
}

// DOTSource produces DOT for a repo. *graph.Client implements it.
type DOTSource interface {
	Get(repo string, cluster bool, theme string) (string, error)
}

// Cache stores rendered documents per repo, cluster flag, and theme.
// *cache.Cache implements it.
type Cache interface {
	GetSVG(repo string, cluster bool, theme string) (string, error)
	SetSVG(repo string, cluster bool, theme string, svg string)
	DeleteSVG(repo string, cluster bool, theme string)

	GetDOT(repo string, cluster bool, theme string) (string, error)
	SetDOT(repo string, cluster bool, theme string, dot string)
	DeleteDOT(repo string, cluster bool, theme string)
}

// ToSVG takes a GoLang repo, cluster flag, and theme, and returns an SVG
// dependency graph.
//
// Cached entries are validated before use: empty or truncated documents are
// evicted and regenerated instead of being served forever.
func ToSVG(graphClient DOTSource, c Cache, repo string, cluster bool, theme Theme) (string, error) {
	cached, err := c.GetSVG(repo, cluster, theme.String())
	if err == nil && isSVG(cached) {
		return cached, nil
	}
	if err == nil {
		log.Warnf("evicting invalid cached svg for %s (cluster=%t theme=%q)", repo, cluster, theme)
		c.DeleteSVG(repo, cluster, theme.String())
	}

	dot, err := ToDOT(graphClient, c, repo, cluster, theme)
	if err != nil {
		log.Errorf("error generating dot: %s", err)
		return "", err
	}

	svg, err := dotToSVG(dot)
	if err != nil {
		log.Errorf("error converting dot to svg: %s", err)
		return "", err
	}

	c.SetSVG(repo, cluster, theme.String(), svg)

	return svg, nil
}

// ToDOT takes a GoLang repo, cluster flag, and theme, and returns a DOT
// dependency graph.
//
// DOT is cached after theming, so each repo/cluster/theme combination is
// stored independently. An entry cached under a theme-less key (from before
// themes existed) is recolored on read; entries that are empty or carry a
// different theme marker are evicted first.
func ToDOT(graphClient DOTSource, c Cache, repo string, cluster bool, theme Theme) (string, error) {
	cached, err := c.GetDOT(repo, cluster, theme.String())
	if err == nil {
		if ok, verr := graph.NormalizeDOT(cached, theme); verr == nil && ok {
			if themed, rerr := graph.RecolorDOT(cached, theme); rerr == nil && themed == cached {
				return cached, nil
			}
		}
		log.Warnf("evicting invalid cached dot for %s (cluster=%t theme=%q)", repo, cluster, theme)
		c.DeleteDOT(repo, cluster, theme.String())
	}

	// A document cached before themes existed still describes this repo, so it
	// can seed a themed entry instead of re-cloning and re-running goda.
	// (Theme-less keys keep their historical shape, so this lookup is safe.)
	if theme != ThemeDefault {
		if legacy, lerr := c.GetDOT(repo, cluster, ""); lerr == nil {
			if ok, verr := graph.NormalizeDOT(legacy, theme); verr == nil && ok {
				if themed, rerr := graph.RecolorDOT(legacy, theme); rerr == nil {
					c.SetDOT(repo, cluster, theme.String(), themed)
					return themed, nil
				}
			}
		}
	}

	// The graph server is asked for themed DOT directly so the cache holds the
	// same document that is served and theming is applied exactly once.
	dot, err := graphClient.Get(repo, cluster, theme.String())
	if err != nil {
		return "", err
	}

	ok, verr := graph.NormalizeDOT(dot, theme)
	if verr != nil || !ok {
		if verr != nil {
			return "", verr
		}
		return "", graph.ErrEmptyGraph
	}

	// A graph server that does not apply themes still yields valid input: recolor
	// it here so every cached themed entry has its theme applied exactly once.
	dot, err = graph.RecolorDOT(dot, theme)
	if err != nil {
		return "", err
	}

	c.SetDOT(repo, cluster, theme.String(), dot)

	return dot, nil
}

// isSVG reports whether s looks like a complete SVG document rather than a
// truncated or empty cache entry.
func isSVG(s string) bool {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "<svg") {
		return strings.HasSuffix(s, "</svg>")
	}
	return strings.HasPrefix(s, "<?xml") &&
		strings.Contains(s, "<svg") &&
		strings.HasSuffix(s, "</svg>")
}

func dotToSVG(dot string) (string, error) {
	command := exec.Command(
		"dot",
		"-Tsvg",
		"-Gfontname=Roboto,Arial,sans-serif",
		"-Nfontname=Roboto,Arial,sans-serif",
		"-Efontname=Roboto,Arial,sans-serif",
	)
	command.Stdin = strings.NewReader(dot)
	var stderr bytes.Buffer
	command.Stderr = &stderr

	log.Debugf("running dot: %s", command)
	svg, err := command.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return "", fmt.Errorf("%w: %s", err, detail)
		}
		return "", err
	}

	if len(bytes.TrimSpace(svg)) == 0 {
		return "", errors.New("dot produced empty output")
	}

	return string(svg), nil
}
