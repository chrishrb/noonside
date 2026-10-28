package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithCORS(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	h := withCORS(next, []string{"http://localhost:8081/"})

	t.Run("preflight from allowed origin", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodOptions, "/api/v1/systems", nil)
		r.Header.Set("Origin", "http://localhost:8081")
		r.Header.Set("Access-Control-Request-Method", "GET")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Equal(t, "http://localhost:8081", w.Header().Get("Access-Control-Allow-Origin"))
		assert.Contains(t, w.Header().Get("Access-Control-Allow-Headers"), "Authorization")
	})

	t.Run("request from allowed origin", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/systems", nil)
		r.Header.Set("Origin", "http://localhost:8081")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		assert.Equal(t, http.StatusTeapot, w.Code)
		assert.Equal(t, "http://localhost:8081", w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("other origin", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodOptions, "/api/v1/systems", nil)
		r.Header.Set("Origin", "http://evil.test")
		r.Header.Set("Access-Control-Request-Method", "GET")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	})
}
