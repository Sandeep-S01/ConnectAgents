package gateway

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const sessionCookieName = "paia_session"
const sessionTTL = 12 * time.Hour

type loginFailureState struct {
	Count      int
	FirstSeen  time.Time
	LastFailed time.Time
}

func (s *Server) authorized(r *http.Request) bool {
	return s.authMode(r) != ""
}

func (s *Server) authMode(r *http.Request) string {
	if s.authToken == "" {
		return "none"
	}
	if r.Header.Get("Authorization") == "Bearer "+s.authToken {
		return "bearer"
	}
	if strings.HasSuffix(r.URL.Path, "/events/stream") && r.URL.Query().Get("token") == s.authToken {
		return "stream_token"
	}
	if s.hasValidSessionCookie(r) {
		return "session"
	}
	return ""
}

func requiresCSRFCheck(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func isMutatingAPIRequest(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsedOrigin, err := url.Parse(origin)
	if err != nil || parsedOrigin.Scheme == "" || parsedOrigin.Host == "" {
		return false
	}
	return strings.EqualFold(parsedOrigin.Scheme, requestScheme(r)) && strings.EqualFold(parsedOrigin.Host, r.Host)
}

func requestScheme(r *http.Request) string {
	if forwardedProto := r.Header.Get("X-Forwarded-Proto"); forwardedProto != "" {
		return strings.TrimSpace(strings.Split(forwardedProto, ",")[0])
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func (s *Server) loginThrottled(remoteAddr string, now time.Time) bool {
	s.loginFailuresMu.Lock()
	defer s.loginFailuresMu.Unlock()

	state, ok := s.loginFailures[remoteAddr]
	if !ok {
		return false
	}
	if now.Sub(state.FirstSeen) > s.loginFailureWindow {
		delete(s.loginFailures, remoteAddr)
		return false
	}
	return state.Count >= s.loginFailureLimit
}

func (s *Server) recordFailedLogin(remoteAddr string, now time.Time) {
	s.loginFailuresMu.Lock()
	defer s.loginFailuresMu.Unlock()

	state := s.loginFailures[remoteAddr]
	if state.FirstSeen.IsZero() || now.Sub(state.FirstSeen) > s.loginFailureWindow {
		state = loginFailureState{FirstSeen: now}
	}
	state.Count++
	state.LastFailed = now
	s.loginFailures[remoteAddr] = state
}

func (s *Server) clearFailedLogins(remoteAddr string) {
	s.loginFailuresMu.Lock()
	defer s.loginFailuresMu.Unlock()
	delete(s.loginFailures, remoteAddr)
}

func (s *Server) newSessionCookie(r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    s.signSession(time.Now().Add(sessionTTL)),
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestScheme(r) == "https",
	}
}

func clearSessionCookie(r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestScheme(r) == "https",
	}
}

func (s *Server) hasValidSessionCookie(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return false
	}
	expiresUnix, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().Unix() > expiresUnix {
		return false
	}
	expected := s.sessionSignature(parts[0])
	return subtle.ConstantTimeCompare([]byte(parts[1]), []byte(expected)) == 1
}

func (s *Server) signSession(expiresAt time.Time) string {
	expires := strconv.FormatInt(expiresAt.Unix(), 10)
	return expires + "." + s.sessionSignature(expires)
}

func (s *Server) sessionSignature(expires string) string {
	mac := hmac.New(sha256.New, []byte(s.authToken))
	_, _ = mac.Write([]byte(expires))
	return hex.EncodeToString(mac.Sum(nil))
}
