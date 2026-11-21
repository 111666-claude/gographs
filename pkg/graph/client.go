package graph

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
)

// Post defines the input POST body to the `/graph` endpoint.
// curl --data '{"repo":"github.com/siggy/gographs","cluster":true,"theme":"dark"}' -X POST [graph-addr]/graph
type Post struct {
	Repo    string `json:"repo"`
	Cluster bool   `json:"cluster"`
	// Theme is "light", "dark", or empty for the historical default output.
	Theme string `json:"theme,omitempty"`
}

// Client provides a client to the graph server.
type Client struct {
	url string
	log *log.Entry
}

// StatusError reports a non-200 response from the graph server.
type StatusError struct {
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("graph server returned status %d: %s", e.StatusCode, e.Body)
}

// IsStatus reports whether err is a graph server response with the given
// status code.
func IsStatus(err error, status int) bool {
	var se *StatusError
	return errors.As(err, &se) && se.StatusCode == status
}

// DefaultGraphAddr defines the graph server's address when running locally. If
// the caller uses something other than the default it is assumed to be TLS'd.
const DefaultGraphAddr = "localhost:8889"

// NewClient creates a client to the graph server.
func NewClient(addr string) *Client {
	url := fmt.Sprintf("http://%s/graph", addr)
	if addr != DefaultGraphAddr {
		url = fmt.Sprintf("https://%s/graph", addr)
	}

	log := log.WithFields(
		log.Fields{
			"graphclient": url,
		},
	)

	log.Infof("Graph client initialized")

	return &Client{url, log}
}

// Get takes a repo, cluster flag, and theme and returns a DOT representation
// of the repo. theme must be a value accepted by ParseTheme.
func (c *Client) Get(repo string, cluster bool, theme string) (string, error) {
	body, err := json.Marshal(
		Post{
			Repo:    repo,
			Cluster: cluster,
			Theme:   theme,
		},
	)
	if err != nil {
		return "", err
	}

	c.log.Debugf("POST Request: %s", string(body))

	labels := prometheus.Labels{
		repoLabel:    repo,
		clusterLabel: strconv.FormatBool(cluster),
		themeLabel:   theme,
	}
	httpRequests.With(labels).Inc()
	httpErrors, err := httpErrors.CurryWith(labels)
	if err != nil {
		return "", err
	}

	timer := prometheus.NewTimer(httpDuration.With(labels))
	defer timer.ObserveDuration()

	resp, err := http.Post(c.url, "text/plain; charset=utf-8", bytes.NewBuffer(body))
	if err != nil {
		httpErrors.WithLabelValues(err.Error()).Inc()
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		httpErrors.WithLabelValues(err.Error()).Inc()
		return "", err
	}

	debugStr := fmt.Sprintf("POST response[%d] (%d bytes): %s ", resp.StatusCode, len(respBody), string(respBody))
	c.log.Debug(debugStr)

	if resp.StatusCode != http.StatusOK {
		err := &StatusError{StatusCode: resp.StatusCode, Body: string(respBody)}
		httpErrors.WithLabelValues(err.Error()).Inc()
		return "", err
	}

	return string(respBody), nil
}
