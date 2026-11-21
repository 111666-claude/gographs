package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/siggy/gographs/pkg/cache"
	"github.com/siggy/gographs/pkg/graph"
	"github.com/siggy/gographs/pkg/prom"
	"github.com/siggy/gographs/pkg/render"
	log "github.com/sirupsen/logrus"
)

const webServer = "web"

// Start initializes the web server and starts listening.
func Start(c *cache.Cache, addr string, graph *graph.Client) error {
	router := mux.NewRouter()
	router.Use(prom.Middleware(webServer))

	getRouter := router.Methods(http.MethodGet).Subrouter()
	postRouter := router.Methods(http.MethodPost).Subrouter()

	log := log.WithFields(
		log.Fields{
			webServer: addr,
		},
	)

	// web views
	getRouter.PathPrefix("/repo").HandlerFunc(repoHandler)
	getRouter.HandleFunc("/svg", repoHandler)
	getRouter.HandleFunc("/", repoHandler)

	// apis
	graphHandler := mkGraphHandler(graph, c, log)
	getRouter.PathPrefix("/graph").HandlerFunc(graphHandler)
	postRouter.PathPrefix("/graph").HandlerFunc(graphHandler)
	getRouter.HandleFunc("/top-repos", mkTopReposHandler(c))

	// assets
	getRouter.PathPrefix("/").Handler(http.FileServer(http.Dir("./public/")))

	log.Infof("%s server listening on %s", webServer, addr)

	return http.ListenAndServe(addr, router)
}

func repoHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "public/index.html")
}

// graphDocStore is the cache surface the graph handler needs.
type graphDocStore interface {
	render.Cache
	Clear(repo string) error
	RepoScoreIncr(repo string)
}

func mkGraphHandler(graphClient render.DOTSource, cache graphDocStore, log *log.Entry) http.HandlerFunc {
	// GET  /graph/github.com/siggy/gographs.svg?cluster=true&theme=dark
	// POST /graph/github.com/siggy/gographs.svg (for refresh)
	return func(rw http.ResponseWriter, r *http.Request) {
		path, err := mux.CurrentRoute(r).GetPathTemplate()
		if err != nil {
			writeError(rw, r, http.StatusInternalServerError, err.Error(), err)
			return
		}
		handleGraph(rw, r, path, graphClient, cache, log)
	}
}

// handleGraph serves .svg/.dot documents. It is separate from the mux wiring so
// it can be tested with a path template directly.
func handleGraph(rw http.ResponseWriter, r *http.Request, pathTemplate string, graphClient render.DOTSource, cache graphDocStore, log *log.Entry) {
	vars := r.URL.Query()
	cluster := vars.Get("cluster") == "true"

	theme, err := render.ParseTheme(vars.Get("theme"))
	if err != nil {
		writeError(rw, r, http.StatusBadRequest, err.Error(), err)
		return
	}

	refresh := r.Method == http.MethodPost

	suffix := ""
	contentType := ""
	if strings.HasSuffix(r.URL.Path, ".svg") {
		suffix = ".svg"
		contentType = "image/svg+xml; charset=utf-8"
	} else if strings.HasSuffix(r.URL.Path, ".dot") {
		suffix = ".dot"
		contentType = "text/plain; charset=utf-8"
	} else {
		writeError(rw, r, http.StatusBadRequest, "svg or dot suffix required", nil)
		return
	}

	goRepo := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, pathTemplate+"/"), suffix)
	if goRepo == "" {
		writeError(rw, r, http.StatusBadRequest, "repo required", nil)
		return
	}

	if refresh {
		log.Debugf("Clearing cache for %s", goRepo)
		err = cache.Clear(goRepo)
		if err != nil {
			log.Errorf("Failed to clear cache for repo %s: %s", goRepo, err)
		}
	}

	log.Debugf("Processing %s (cluster=%t theme=%q)", goRepo, cluster, theme)

	out := ""
	if suffix == ".svg" {
		out, err = render.ToSVG(graphClient, cache, goRepo, cluster, theme)
	} else if suffix == ".dot" {
		out, err = render.ToDOT(graphClient, cache, goRepo, cluster, theme)
	}
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, graph.ErrEmptyGraph):
			status = http.StatusUnprocessableEntity
		case graph.IsStatus(err, http.StatusUnprocessableEntity):
			status = http.StatusUnprocessableEntity
		case graph.IsStatus(err, http.StatusBadRequest):
			status = http.StatusBadRequest
		}
		message := fmt.Sprintf("Failed to render %s to %s", goRepo, suffix)
		writeError(rw, r, status, message, err)
		return
	}

	go cache.RepoScoreIncr(goRepo)

	rw.Header().Set("Content-Type", contentType)
	// Theme and cluster change the document, so caches and CDNs must not
	// share one response across combinations.
	rw.Header().Set("Cache-Control", "public, max-age=300")
	rw.Header().Set("Vary", "Accept-Encoding")
	if !theme.IsZero() {
		rw.Header().Set("X-Gographs-Theme", theme.String())
	}
	rw.WriteHeader(http.StatusOK)
	rw.Write([]byte(out))
}

// TODO: poll for this every interval, hold result in local mem
func mkTopReposHandler(cache *cache.Cache) http.HandlerFunc {
	// /top-repos
	return func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(rw, r, http.StatusMethodNotAllowed, "", nil)
			return
		}

		scores, err := cache.RepoScores()
		if err != nil {
			writeError(rw, r, http.StatusInternalServerError, "", err)
			return
		}

		j, err := json.Marshal(scores)
		if err != nil {
			writeError(rw, r, http.StatusInternalServerError, "", err)
			return
		}

		rw.Header().Set("Content-Type", "application/json; charset=utf-8")
		rw.WriteHeader(http.StatusOK)
		rw.Write(j)
	}
}

// writeError handles all errors returned by the web server. It writes an error
// header, an optional error message, counts the error in metrics, and logs it.
func writeError(rw http.ResponseWriter, r *http.Request, status int, message string, err error) {
	rw.WriteHeader(status)
	if message != "" {
		rw.Write([]byte(message))
	}

	route := mux.CurrentRoute(r)
	path := ""
	if route != nil {
		path, _ = route.GetPathTemplate()
	}

	log.Errorf("Failed request for [%s]: [%d] Message: [%s] Error: [%s]", path, status, message, err)
	prom.CountError(webServer, r, status, message, err)
}
