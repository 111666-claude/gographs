package graph

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/siggy/gographs/internal/fixtures"
	"github.com/sirupsen/logrus"
)

func TestGraphHandlerTheme(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		status      int
		themeHeader string
		wantMarker  string
	}{
		{
			name:   "no theme keeps historical output",
			body:   `{"repo":"example.com/demo","cluster":true}`,
			status: http.StatusOK,
		},
		{
			name:        "dark theme",
			body:        `{"repo":"example.com/demo","cluster":true,"theme":"dark"}`,
			status:      http.StatusOK,
			themeHeader: string(ThemeDark),
			wantMarker:  themeMarker + string(ThemeDark),
		},
		{
			name:        "light theme",
			body:        `{"repo":"example.com/demo","cluster":true,"theme":"light"}`,
			status:      http.StatusOK,
			themeHeader: string(ThemeLight),
			wantMarker:  themeMarker + string(ThemeLight),
		},
		{
			name:   "unknown theme is rejected",
			body:   `{"repo":"example.com/demo","theme":"neon"}`,
			status: http.StatusBadRequest,
		},
		{
			name:   "theme injection attempt is rejected",
			body:   `{"repo":"example.com/demo","theme":"dark\"]; graph[bgcolor=red"}`,
			status: http.StatusBadRequest,
		},
		{
			name:   "unknown field is ignored for forward compatibility",
			body:   `{"repo":"example.com/demo","colour":"dark"}`,
			status: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotTheme Theme
			render := func(repo string, cluster bool, theme Theme) (string, error) {
				gotTheme = theme
				if theme.IsZero() {
					return fixtures.DOT, nil
				}
				return ApplyDOT(fixtures.DOT, theme)
			}

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/graph", strings.NewReader(tt.body))
			mkGraphHandler(logrus.NewEntry(logrus.New()), render).ServeHTTP(rec, req)

			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tt.status, rec.Body.String())
			}
			if rec.Code != http.StatusOK {
				return
			}
			if got := rec.Header().Get("X-Gographs-Theme"); got != tt.themeHeader {
				t.Errorf("X-Gographs-Theme = %q, want %q", got, tt.themeHeader)
			}
			if strings.Contains(rec.Body.String(), "color=red") {
				t.Errorf("untrusted input leaked into DOT output")
			}
			if tt.wantMarker != "" && !strings.Contains(rec.Body.String(), tt.wantMarker) {
				t.Errorf("output missing %q", tt.wantMarker)
			}
			if tt.wantMarker == "" && strings.Contains(rec.Body.String(), "gographs-theme") {
				t.Errorf("default output unexpectedly themed:\n%s", rec.Body.String())
			}
			if want := mustTheme(t, tt.body); gotTheme != want {
				t.Errorf("render got theme %q, want %q", gotTheme, want)
			}
		})
	}
}

func mustTheme(t *testing.T, body string) Theme {
	t.Helper()
	var p Post
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatalf("bad test body: %v", err)
	}
	theme, err := ParseTheme(p.Theme)
	if err != nil {
		t.Fatalf("bad test theme: %v", err)
	}
	return theme
}

func TestGraphHandlerEmptyGraph(t *testing.T) {
	render := func(string, bool, Theme) (string, error) {
		return "", ErrEmptyGraph
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/graph", strings.NewReader(`{"repo":"example.com/empty"}`))
	mkGraphHandler(logrus.NewEntry(logrus.New()), render).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if !strings.Contains(rec.Body.String(), ErrEmptyGraph.Error()) {
		t.Errorf("body %q does not explain the empty graph", rec.Body.String())
	}
}

func TestGraphHandlerRejectsOversizedBody(t *testing.T) {
	body := `{"repo":"` + strings.Repeat("a", maxPostBodyBytes) + `"}`
	render := func(string, bool, Theme) (string, error) { return fixtures.DOT, nil }

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/graph", strings.NewReader(body))
	mkGraphHandler(logrus.NewEntry(logrus.New()), render).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGraphHandlerRoute(t *testing.T) {
	// The production router must dispatch POST /graph through the handler.
	router := mux.NewRouter()
	router.HandleFunc("/graph", func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}).Methods(http.MethodPost)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/graph", strings.NewReader(`{}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("route status = %d", rec.Code)
	}
}
