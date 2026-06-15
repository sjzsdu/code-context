package store

import (
	"fmt"
	"strings"
)

type Backend string

const (
	BackendSQLite Backend = "sqlite"
	BackendHelix  Backend = "helix"
)

type SQLiteOptions struct {
	Path string
}

type HelixOptions struct {
	URL       string
	APIKey    string
	APIKeyEnv string
}

type Options struct {
	Backend Backend
	SQLite  SQLiteOptions
	Helix   HelixOptions
}

func (o Options) BackendOrDefault() Backend {
	backend := Backend(strings.TrimSpace(string(o.Backend)))
	if backend == "" {
		return BackendSQLite
	}
	return backend
}

func New(opts Options) (Store, error) {
	switch opts.BackendOrDefault() {
	case BackendSQLite:
		if strings.TrimSpace(opts.SQLite.Path) == "" {
			return nil, fmt.Errorf("sqlite store path is required")
		}
		return NewSQLiteStore(opts.SQLite.Path)
	case BackendHelix:
		return NewHelixStore(opts.Helix)
	default:
		return nil, fmt.Errorf("unsupported store backend %q", opts.Backend)
	}
}
