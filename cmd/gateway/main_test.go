package main

import (
	"net/http"
	"testing"
	"time"
)

func TestNewHTTPServerUsesProductionTimeouts(t *testing.T) {
	t.Parallel()

	server := newHTTPServer("127.0.0.1:8080", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	if server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("expected 5s read header timeout, got %s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != 30*time.Second {
		t.Fatalf("expected 30s read timeout, got %s", server.ReadTimeout)
	}
	if server.IdleTimeout != 2*time.Minute {
		t.Fatalf("expected 2m idle timeout, got %s", server.IdleTimeout)
	}
	if server.MaxHeaderBytes != 1<<20 {
		t.Fatalf("expected 1 MiB max header bytes, got %d", server.MaxHeaderBytes)
	}
}
