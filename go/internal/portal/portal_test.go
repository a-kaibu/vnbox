package portal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vnbox/internal/session"
)

func TestAuth(t *testing.T) {
	t.Setenv("VNBOX", t.TempDir())
	h := New("s3cret", session.Options{}).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/?token=wrong", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/?token=s3cret", nil))
	if rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), "token") {
		t.Fatalf("token login: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	cookie := rec.Result().Cookies()[0]
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "vnbox") {
		t.Fatalf("with cookie: %d", rec.Code)
	}
	// a stream of a game that is not running redirects to the index instead of exposing anything
	req = httptest.NewRequest("GET", "/g/none/video", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("stopped game: %d", rec.Code)
	}
}
