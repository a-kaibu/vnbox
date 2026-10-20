package portal

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"vnbox/internal/session"
)

func TestUsers(t *testing.T) {
	s := NewUsers(map[string]string{"alice-token": "alice", "bob-token-1": "bob"}, session.Options{})
	h := s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("user=" + user(r))) }))
	get := func(cookie, query string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/"+query, nil)
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "vnbox", Value: cookie})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := get("alice-token", ""); w.Body.String() != "user=alice" {
		t.Errorf("alice: %d %q", w.Code, w.Body.String())
	}
	if w := get("bob-token-1", ""); w.Body.String() != "user=bob" {
		t.Errorf("bob: %d %q", w.Code, w.Body.String())
	}
	if w := get("mallory", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("unknown cookie: %d", w.Code)
	}
	if w := get("", "?token=mallory"); w.Code != http.StatusUnauthorized {
		t.Errorf("unknown token: %d", w.Code)
	}
	if w := get("", "?token=bob-token-1"); w.Code != http.StatusSeeOther || w.Result().Cookies()[0].Value != "bob-token-1" {
		t.Errorf("login: %d", w.Code)
	}
	if session.Key("bob", "demo") == session.Key("alice", "demo") || session.Key("", "demo") != "demo" {
		t.Error("session keys")
	}
}
