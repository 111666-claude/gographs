package cache

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"

	"github.com/siggy/gographs/internal/fixtures"
)

const (
	themeDefault = ""
	themeLight   = "light"
	themeDark    = "dark"
)

// memStore is an in-memory store for tests.
type memStore struct {
	mu     sync.Mutex
	hashes map[string]map[string]string
	zsets  map[string]map[string]float64
	fail   error
}

func newMemStore() *memStore {
	return &memStore{
		hashes: map[string]map[string]string{},
		zsets:  map[string]map[string]float64{},
	}
}

func (s *memStore) Ping(context.Context) error { return s.fail }

func (s *memStore) HGet(_ context.Context, key, field string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return "", s.fail
	}
	v, ok := s.hashes[key][field]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

func (s *memStore) HSet(_ context.Context, key, field, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	if s.hashes[key] == nil {
		s.hashes[key] = map[string]string{}
	}
	s.hashes[key][field] = value
	return nil
}

func (s *memStore) HDel(_ context.Context, key, field string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return 0, s.fail
	}
	if _, ok := s.hashes[key][field]; !ok {
		return 0, nil
	}
	delete(s.hashes[key], field)
	return 1, nil
}

func (s *memStore) HLen(_ context.Context, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return int64(len(s.hashes[key])), nil
}

func (s *memStore) ZIncrBy(_ context.Context, key, member string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	if s.zsets[key] == nil {
		s.zsets[key] = map[string]float64{}
	}
	s.zsets[key][member]++
	return nil
}

func (s *memStore) ZRevRangeByScore(_ context.Context, key string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for member := range s.zsets[key] {
		out = append(out, member)
	}
	sort.Strings(out)
	return out, nil
}

func (s *memStore) ZCount(_ context.Context, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return int64(len(s.zsets[key])), nil
}

func newTestCache(t *testing.T) (*Cache, *memStore) {
	t.Helper()
	s := newMemStore()
	c, err := newCache(s, "mem")
	if err != nil {
		t.Fatalf("newCache: %v", err)
	}
	return c, s
}

func TestCacheThemeIsolation(t *testing.T) {
	c, _ := newTestCache(t)
	repo := fixtures.Repo

	cases := []struct {
		cluster bool
		theme   string
		value   string
	}{
		{false, themeDefault, "default-plain"},
		{true, themeDefault, "default-clustered"},
		{false, themeLight, "light-plain"},
		{true, themeLight, "light-clustered"},
		{false, themeDark, "dark-plain"},
		{true, themeDark, "dark-clustered"},
	}

	for _, tc := range cases {
		c.SetDOT(repo, tc.cluster, tc.theme, "dot-"+tc.value)
		c.SetSVG(repo, tc.cluster, tc.theme, "svg-"+tc.value)
	}

	for _, tc := range cases {
		gotDOT, err := c.GetDOT(repo, tc.cluster, tc.theme)
		if err != nil {
			t.Fatalf("GetDOT(%t, %q): %v", tc.cluster, tc.theme, err)
		}
		if want := "dot-" + tc.value; gotDOT != want {
			t.Errorf("GetDOT(%t, %q) = %q, want %q", tc.cluster, tc.theme, gotDOT, want)
		}

		gotSVG, err := c.GetSVG(repo, tc.cluster, tc.theme)
		if err != nil {
			t.Fatalf("GetSVG(%t, %q): %v", tc.cluster, tc.theme, err)
		}
		if want := "svg-" + tc.value; gotSVG != want {
			t.Errorf("GetSVG(%t, %q) = %q, want %q", tc.cluster, tc.theme, gotSVG, want)
		}
	}
}

func TestCacheDefaultKeyKeepsHistoricalShape(t *testing.T) {
	c, s := newTestCache(t)
	c.SetDOT(fixtures.Repo, true, "", "x")
	if _, ok := s.hashes[dotHash][fixtures.Repo+"+true"]; !ok {
		t.Errorf("default theme key changed shape: %v", s.hashes[dotHash])
	}

	c.SetSVG(fixtures.Repo, false, "dark", "y")
	if _, ok := s.hashes[svgHash][fixtures.Repo+"+false+dark"]; !ok {
		t.Errorf("themed key = %v, want repo+cluster+theme", s.hashes[svgHash])
	}
}

func TestCacheClearRemovesAllThemeCombinations(t *testing.T) {
	c, s := newTestCache(t)
	repo := fixtures.Repo
	other := "example.com/other"

	for _, cluster := range []bool{false, true} {
		for _, theme := range []string{"", "light", "dark"} {
			c.SetDOT(repo, cluster, theme, "dot")
			c.SetSVG(repo, cluster, theme, "svg")
		}
	}
	// A repo whose name shares a prefix must not be cleared.
	c.SetDOT(other, false, "", "keep")
	c.SetSVG(other, false, "", "keep")

	if err := c.Clear(repo); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	for _, hash := range []string{dotHash, svgHash} {
		if n := len(s.hashes[hash]); n != 1 {
			t.Errorf("%s has %d entries after Clear, want 1", hash, n)
		}
		if _, ok := s.hashes[hash][other+"+false"]; !ok {
			t.Errorf("%s lost the unrelated repo", hash)
		}
	}
}

func TestCacheClearSurfacesErrors(t *testing.T) {
	c, s := newTestCache(t)
	s.fail = errors.New("boom")
	if err := c.Clear(fixtures.Repo); err == nil {
		t.Fatal("Clear with a failing store returned nil")
	}
}

func TestRepoScoreIncr(t *testing.T) {
	c, _ := newTestCache(t)
	c.RepoScoreIncr(fixtures.Repo)
	c.RepoScoreIncr(fixtures.Repo)

	scores, err := c.RepoScores()
	if err != nil {
		t.Fatalf("RepoScores: %v", err)
	}
	if len(scores) != 1 || scores[0] != fixtures.Repo {
		t.Errorf("RepoScores = %v, want [%s]", scores, fixtures.Repo)
	}
}
