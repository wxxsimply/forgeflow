package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionAndCSRFCookieSecurityAttributes(t *testing.T) {
	recorder := httptest.NewRecorder()
	server := &Server{options: Options{CookieSecure: true, CookieDomain: "example.test"}}
	server.setCookies(recorder, "session-secret", "csrf-secret", 3600)
	cookies := recorder.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("cookies=%d", len(cookies))
	}
	byName := map[string]*http.Cookie{}
	for _, cookie := range cookies {
		byName[cookie.Name] = cookie
	}
	session := byName[SessionCookie]
	csrf := byName[CSRFCookie]
	if session == nil || !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteLaxMode || session.Path != "/" {
		t.Fatalf("unsafe session cookie: %+v", session)
	}
	if csrf == nil || csrf.HttpOnly || !csrf.Secure || csrf.SameSite != http.SameSiteLaxMode || csrf.Path != "/" {
		t.Fatalf("unsafe CSRF cookie: %+v", csrf)
	}
}
