package cache

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"
	"github.com/valkey-io/valkey-go"
)

// store is the subset of the Valkey operations the cache needs. Keeping it
// narrow lets tests substitute an in-memory implementation, and keeps the
// theme/cluster key layout in one place.
type store interface {
	Ping(ctx context.Context) error
	HGet(ctx context.Context, key, field string) (string, error)
	HSet(ctx context.Context, key, field, value string) error
	HDel(ctx context.Context, key, field string) (int64, error)
	HLen(ctx context.Context, key string) (int64, error)
	ZIncrBy(ctx context.Context, key, member string) error
	ZRevRangeByScore(ctx context.Context, key string) ([]string, error)
	ZCount(ctx context.Context, key string) (int64, error)
}

// Cache holds the backing store and log state.
type Cache struct {
	store store
	log   *log.Entry
}

const (
	// dot[repo+cluster+theme]
	// github.com/siggy/gographs+false
	// github.com/siggy/gographs+false+dark
	// =>
	// [dot file]
	dotHash = "dot"

	// svg[repo+cluster+theme]
	// github.com/siggy/gographs+false
	// github.com/siggy/gographs+false+dark
	// =>
	// [svg file]
	svgHash = "svg"

	// repoScores[repo]
	// github.com/siggy/gographs
	// =>
	// [numeric popularity score]
	repoScores = "reposcores"
)

// New initializes a new cache client.
func New(addr string) (*Cache, error) {
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{addr},
	})
	if err != nil {
		return nil, err
	}

	return newCache(&valkeyStore{client}, addr)
}

func newCache(s store, addr string) (*Cache, error) {
	ctx := context.Background()

	if err := s.Ping(ctx); err != nil {
		return nil, err
	}

	log := log.WithFields(
		log.Fields{
			"cache": addr,
		},
	)

	registerGauges(s)

	log.Infof("Cache initialized")

	return &Cache{
		store: s,
		log:   log,
	}, nil
}

// repoThemes is every theme value that can appear in a cache key, including
// the empty historical theme. Keep it in sync with graph.Theme.
var repoThemes = []string{"", "light", "dark"}

// Clear deletes all cache entries relevant to a GoLang repo, across every
// cluster flag and theme combination that may exist. Entries written before
// themes existed (theme-less keys) are removed too, so a refresh never leaves
// a stale unthemed entry behind.
//
// The combinations are finite, so this deletes keys directly rather than
// scanning the whole hash (which would list every repo in the cache).
func (c *Cache) Clear(repo string) error {
	ctx := context.Background()
	var rerr error
	for _, hash := range []string{dotHash, svgHash} {
		for _, cluster := range []bool{false, true} {
			for _, theme := range repoThemes {
				if _, err := c.store.HDel(ctx, hash, repoKey(repo, cluster, theme)); err != nil && rerr == nil {
					rerr = err
				}
			}
		}
	}
	return rerr
}

// SetSVG sets an SVG for a repo, cluster flag, and theme.
func (c *Cache) SetSVG(repo string, cluster bool, theme string, svg string) {
	if err := c.store.HSet(context.Background(), svgHash, repoKey(repo, cluster, theme), svg); err != nil {
		c.log.Errorf("SetSVG failed: %s", err)
	}
}

// GetSVG gets an SVG for a repo, cluster flag, and theme.
func (c *Cache) GetSVG(repo string, cluster bool, theme string) (string, error) {
	c.log.Tracef("hget[%s,%s]", svgHash, repoKey(repo, cluster, theme))
	return c.store.HGet(context.Background(), svgHash, repoKey(repo, cluster, theme))
}

// SetDOT sets a DOT for a repo, cluster flag, and theme.
func (c *Cache) SetDOT(repo string, cluster bool, theme string, dot string) {
	if err := c.store.HSet(context.Background(), dotHash, repoKey(repo, cluster, theme), dot); err != nil {
		c.log.Errorf("SetDOT failed: %s", err)
	}
}

// GetDOT gets a DOT for a repo, cluster flag, and theme.
func (c *Cache) GetDOT(repo string, cluster bool, theme string) (string, error) {
	c.log.Tracef("hget[%s,%s]", dotHash, repoKey(repo, cluster, theme))
	return c.store.HGet(context.Background(), dotHash, repoKey(repo, cluster, theme))
}

// DeleteDOT removes one cached DOT entry, e.g. a stale or invalid document.
func (c *Cache) DeleteDOT(repo string, cluster bool, theme string) {
	if _, err := c.store.HDel(context.Background(), dotHash, repoKey(repo, cluster, theme)); err != nil {
		c.log.Errorf("DeleteDOT failed: %s", err)
	}
}

// DeleteSVG removes one cached SVG entry, e.g. a stale or invalid document.
func (c *Cache) DeleteSVG(repo string, cluster bool, theme string) {
	if _, err := c.store.HDel(context.Background(), svgHash, repoKey(repo, cluster, theme)); err != nil {
		c.log.Errorf("DeleteSVG failed: %s", err)
	}
}

// RepoScoreIncr increments the popularity score for a repo.
func (c *Cache) RepoScoreIncr(repo string) {
	if err := c.store.ZIncrBy(context.Background(), repoScores, repo); err != nil {
		c.log.Errorf("RepoScoreIncr failed: %s", err)
	}
}

// RepoScores returns the top-10 most popular repos
func (c *Cache) RepoScores() ([]string, error) {
	return c.store.ZRevRangeByScore(context.Background(), repoScores)
}

// repoKey builds a cache field. theme is "" for the default theme, which keeps
// the historical key shape and leaves existing entries valid.
func repoKey(repo string, cluster bool, theme string) string {
	key := fmt.Sprintf("%s+%t", repo, cluster)
	if theme != "" {
		key += "+" + theme
	}
	return key
}

// valkeyStore adapts a Valkey client to store.
type valkeyStore struct {
	client valkey.Client
}

func (s *valkeyStore) Ping(ctx context.Context) error {
	return s.client.Do(ctx, s.client.B().Ping().Build()).Error()
}

func (s *valkeyStore) HGet(ctx context.Context, key, field string) (string, error) {
	return s.client.Do(ctx, s.client.B().Hget().Key(key).Field(field).Build()).ToString()
}

func (s *valkeyStore) HSet(ctx context.Context, key, field, value string) error {
	return s.client.Do(
		ctx,
		s.client.B().Hset().Key(key).FieldValue().FieldValue(field, value).Build(),
	).Error()
}

func (s *valkeyStore) HDel(ctx context.Context, key, field string) (int64, error) {
	return s.client.Do(ctx, s.client.B().Hdel().Key(key).Field(field).Build()).AsInt64()
}

func (s *valkeyStore) HLen(ctx context.Context, key string) (int64, error) {
	return s.client.Do(ctx, s.client.B().Hlen().Key(key).Build()).AsInt64()
}

func (s *valkeyStore) ZIncrBy(ctx context.Context, key, member string) error {
	return s.client.Do(
		ctx,
		s.client.B().Zincrby().Key(key).Increment(1).Member(member).Build(),
	).Error()
}

func (s *valkeyStore) ZRevRangeByScore(ctx context.Context, key string) ([]string, error) {
	return s.client.Do(
		ctx,
		s.client.B().Zrevrangebyscore().Key(key).Max("+inf").Min("0").Limit(0, 1000).Build(),
	).AsStrSlice()
}

func (s *valkeyStore) ZCount(ctx context.Context, key string) (int64, error) {
	return s.client.Do(
		ctx,
		s.client.B().Zcount().Key(key).Min("-inf").Max("+inf").Build(),
	).AsInt64()
}
