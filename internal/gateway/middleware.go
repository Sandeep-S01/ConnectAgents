package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"time"
)

type rateLimitWindow struct {
	Count     int
	StartedAt time.Time
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func requestIDFrom(r *http.Request) string {
	requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if requestID != "" {
		return requestID
	}
	return newRequestID()
}

func newRequestID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(bytes[:])
}

func setSecurityHeaders(headers http.Header, r *http.Request) {
	headers.Set("X-Content-Type-Options", "nosniff")
	headers.Set("X-Frame-Options", "DENY")
	headers.Set("Referrer-Policy", "no-referrer")
	headers.Set("Content-Security-Policy", "default-src 'self'; base-uri 'self'; frame-ancestors 'none'")
	if requestScheme(r) == "https" {
		headers.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}
}

func setSensitiveResponseHeaders(headers http.Header, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
		headers.Set("Cache-Control", "no-store")
	}
}

func remoteAddressKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	if r.RemoteAddr != "" {
		return r.RemoteAddr
	}
	return "unknown"
}

func (s *Server) apiWriteRateLimited(remoteAddr string, now time.Time) (time.Duration, bool) {
	s.apiWriteMu.Lock()
	defer s.apiWriteMu.Unlock()

	window := s.apiWriteWindows[remoteAddr]
	if window.StartedAt.IsZero() || now.Sub(window.StartedAt) >= s.apiWriteRateWindow {
		s.apiWriteWindows[remoteAddr] = rateLimitWindow{Count: 1, StartedAt: now}
		return 0, false
	}
	if window.Count >= s.apiWriteLimit {
		retryAfter := s.apiWriteRateWindow - now.Sub(window.StartedAt)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		return retryAfter, true
	}
	window.Count++
	s.apiWriteWindows[remoteAddr] = window
	return 0, false
}
