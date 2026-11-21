package cache

import (
	"context"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// registerOnce keeps gauge registration idempotent: promauto panics when the
// same metric is registered twice, and tests may build several caches.
var registerOnce sync.Once

func registerGauges(s store) {
	registerOnce.Do(func() {
		registerHashGauge(s, dotHash)
		registerHashGauge(s, svgHash)
		registerSetGauge(s, repoScores)
	})
}

func registerHashGauge(s store, key string) {
	registerGauge(
		func() float64 {
			size, _ := s.HLen(context.Background(), key)
			return float64(size)
		},
		key,
	)
}

func registerSetGauge(s store, key string) {
	registerGauge(
		func() float64 {
			size, _ := s.ZCount(context.Background(), key)
			return float64(size)
		},
		key,
	)
}

func registerGauge(function func() float64, key string) {
	promauto.NewGaugeFunc(
		prometheus.GaugeOpts{
			Namespace:   "gographs",
			Subsystem:   "cache",
			Name:        "size",
			Help:        "Size of the cache",
			ConstLabels: prometheus.Labels{"key": key},
		},
		function,
	)
}
