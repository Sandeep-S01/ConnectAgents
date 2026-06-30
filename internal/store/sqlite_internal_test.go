package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenSQLiteLimitsOpenConnectionsForSingleProcessWrites(t *testing.T) {
	t.Parallel()

	s, err := OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	if maxOpen := s.db.Stats().MaxOpenConnections; maxOpen != 1 {
		t.Fatalf("expected max open connections 1, got %d", maxOpen)
	}
}
