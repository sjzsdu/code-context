package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadYAMLConfigAndResolveRelativePaths(t *testing.T) {
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}

	configPath := filepath.Join(projectDir, ".code-context.yaml")
	content := []byte("root: ./src\ndb: ./.cache/index.db\nserver:\n  port: 7070\nwatch:\n  enabled: true\n  interval: 2s\n  debounce: 250ms\n")
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	nestedDir := filepath.Join(projectDir, "src", "pkg")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	loaded, err := Load(nestedDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Path != configPath {
		t.Fatalf("config path = %q, want %q", loaded.Path, configPath)
	}
	if loaded.Config.Root != filepath.Join(projectDir, "src") {
		t.Fatalf("root = %q", loaded.Config.Root)
	}
	if loaded.Config.DB != filepath.Join(projectDir, ".cache", "index.db") {
		t.Fatalf("db = %q", loaded.Config.DB)
	}
	if loaded.Config.Server.Port != 7070 {
		t.Fatalf("port = %d", loaded.Config.Server.Port)
	}
	if !loaded.Config.Watch.Enabled {
		t.Fatalf("watch.enabled = false")
	}
	if loaded.Config.Watch.Interval != 2*time.Second {
		t.Fatalf("watch.interval = %s", loaded.Config.Watch.Interval)
	}
	if loaded.Config.Watch.Debounce != 250*time.Millisecond {
		t.Fatalf("watch.debounce = %s", loaded.Config.Watch.Debounce)
	}
}

func TestLoadJSONConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".code-context.json")
	content := []byte(`{"root":".","db":"./index.db","server":{"port":8181},"watch":{"enabled":true,"interval":3000000000,"debounce":150000000}}`)
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loaded, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Config.Server.Port != 8181 {
		t.Fatalf("port = %d", loaded.Config.Server.Port)
	}
	if !loaded.Config.Watch.Enabled {
		t.Fatalf("watch.enabled = false")
	}
	if loaded.Config.Watch.Interval != 3*time.Second {
		t.Fatalf("watch.interval = %s", loaded.Config.Watch.Interval)
	}
	if loaded.Config.Watch.Debounce != 150*time.Millisecond {
		t.Fatalf("watch.debounce = %s", loaded.Config.Watch.Debounce)
	}
}

func TestLoadConfigNotFound(t *testing.T) {
	_, err := Load(t.TempDir())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
