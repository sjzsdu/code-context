package store

import (
	"path/filepath"
	"testing"
)

func TestNewDefaultsToSQLite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	st, err := New(Options{SQLite: SQLiteOptions{Path: dbPath}})
	if err != nil {
		t.Fatalf("New default sqlite store: %v", err)
	}
	defer st.Close()
}

func TestNewRejectsUnsupportedBackend(t *testing.T) {
	_, err := New(Options{Backend: Backend("unknown")})
	if err == nil {
		t.Fatalf("expected unsupported backend error")
	}
}

func TestNewHelixStoreCanBeConstructed(t *testing.T) {
	st, err := New(Options{
		Backend: BackendHelix,
		Helix:   HelixOptions{URL: "http://localhost:6969"},
	})
	if err != nil {
		t.Fatalf("New helix store: %v", err)
	}
	defer st.Close()
}

func TestNewHelixStoreUsesSDKDefaultURL(t *testing.T) {
	st, err := New(Options{Backend: BackendHelix})
	if err != nil {
		t.Fatalf("New helix store with default URL: %v", err)
	}
	defer st.Close()
}
