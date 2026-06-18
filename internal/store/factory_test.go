package store

import (
	"path/filepath"
	"testing"
	"time"
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

func TestNewHelixStoreAppliesTimeoutAndWriteRetryOptions(t *testing.T) {
	st, err := NewHelixStore(HelixOptions{
		URL:                "http://localhost:6969",
		Timeout:            15 * time.Second,
		WriteRetryAttempts: 4,
		WriteRetryBackoff:  75 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewHelixStore: %v", err)
	}
	defer st.Close()
	hs, ok := st.(*helixStore)
	if !ok {
		t.Fatalf("store type = %T", st)
	}
	if hs.requestTimeout != 15*time.Second {
		t.Fatalf("requestTimeout = %s", hs.requestTimeout)
	}
	if hs.writeRetryAttempts != 4 {
		t.Fatalf("writeRetryAttempts = %d", hs.writeRetryAttempts)
	}
	if hs.writeRetryBackoff != 75*time.Millisecond {
		t.Fatalf("writeRetryBackoff = %s", hs.writeRetryBackoff)
	}
}

func TestNewHelixStoreRejectsNegativeRuntimeOptions(t *testing.T) {
	cases := []HelixOptions{
		{URL: "http://localhost:6969", Timeout: -time.Second},
		{URL: "http://localhost:6969", WriteRetryAttempts: -1},
		{URL: "http://localhost:6969", WriteRetryBackoff: -time.Millisecond},
	}
	for _, opts := range cases {
		if _, err := NewHelixStore(opts); err == nil {
			t.Fatalf("NewHelixStore(%+v) succeeded, want error", opts)
		}
	}
}
