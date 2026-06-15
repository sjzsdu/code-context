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
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
	projectDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}

	configPath := filepath.Join(projectDir, ".code-context.yaml")
	content := []byte("root: ./src\ndb: ./.cache/index.db\nserver:\n  port: 7070\nwatch:\n  enabled: true\n  interval: 2s\n  debounce: 250ms\ndocs:\n  fail_on_broken: true\n  min_route_coverage: 80\n  min_symbol_coverage: 60\n")
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
	if !loaded.Config.Docs.FailOnBroken {
		t.Fatalf("docs.fail_on_broken = false")
	}
	if loaded.Config.Docs.MinRouteCoverage == nil || *loaded.Config.Docs.MinRouteCoverage != 80 {
		t.Fatalf("docs.min_route_coverage = %v", loaded.Config.Docs.MinRouteCoverage)
	}
	if loaded.Config.Docs.MinSymbolCoverage == nil || *loaded.Config.Docs.MinSymbolCoverage != 60 {
		t.Fatalf("docs.min_symbol_coverage = %v", loaded.Config.Docs.MinSymbolCoverage)
	}
}

func TestLoadJSONConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
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

func TestLoadStoreConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
	configPath := filepath.Join(tmpDir, ".code-context.yaml")
	content := []byte("store:\n  backend: helix\n  sqlite:\n    db: ./.cache/index.db\n  helix:\n    url: http://localhost:6969\n    api_key_env: HELIX_API_KEY\n    project_id: custom-project\n")
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loaded, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Config.Store.Backend != "helix" {
		t.Fatalf("store.backend = %q", loaded.Config.Store.Backend)
	}
	if loaded.Config.Store.SQLite.DB != filepath.Join(tmpDir, ".cache", "index.db") {
		t.Fatalf("store.sqlite.db = %q", loaded.Config.Store.SQLite.DB)
	}
	if loaded.Config.Store.Helix.URL != "http://localhost:6969" {
		t.Fatalf("store.helix.url = %q", loaded.Config.Store.Helix.URL)
	}
	if loaded.Config.Store.Helix.APIKeyEnv != "HELIX_API_KEY" {
		t.Fatalf("store.helix.api_key_env = %q", loaded.Config.Store.Helix.APIKeyEnv)
	}
	if loaded.Config.Store.Helix.ProjectID != "custom-project" {
		t.Fatalf("store.helix.project_id = %q", loaded.Config.Store.Helix.ProjectID)
	}
}

func TestLoadConfigNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))
	_, err := Load(filepath.Join(tmpDir, "repo"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLoadMergesUserAndProjectConfig(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	projectDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(filepath.Join(homeDir, ".code-context"), 0o755); err != nil {
		t.Fatalf("mkdir home config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectDir, ".code-context"), 0o755); err != nil {
		t.Fatalf("mkdir project config: %v", err)
	}
	t.Setenv("HOME", homeDir)

	userConfigPath := filepath.Join(homeDir, ".code-context", "config.yaml")
	userConfig := []byte("store:\n  backend: helix\n  helix:\n    url: http://user-helix:6969\n    api_key_env: USER_HELIX_KEY\nserver:\n  port: 7070\nwatch:\n  enabled: true\n  interval: 5s\ndocs:\n  fail_on_broken: true\n")
	if err := os.WriteFile(userConfigPath, userConfig, 0o644); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	projectConfigPath := filepath.Join(projectDir, ".code-context", "config.yaml")
	projectConfig := []byte("root: .\ndb: .code-context/index.db\nstore:\n  helix:\n    project_id: project-a\nserver:\n  port: 9090\nwatch:\n  enabled: false\ndocs:\n  fail_on_broken: false\n")
	if err := os.WriteFile(projectConfigPath, projectConfig, 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	loaded, err := Load(projectDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded.Sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(loaded.Sources))
	}
	if loaded.Sources[0].Path != userConfigPath || loaded.Sources[1].Path != projectConfigPath {
		t.Fatalf("sources = %#v", loaded.Sources)
	}
	if loaded.Path != projectConfigPath {
		t.Fatalf("path = %q, want %q", loaded.Path, projectConfigPath)
	}
	if loaded.Config.Root != projectDir {
		t.Fatalf("root = %q", loaded.Config.Root)
	}
	if loaded.Config.DB != filepath.Join(projectDir, ".code-context", "index.db") {
		t.Fatalf("db = %q", loaded.Config.DB)
	}
	if loaded.Config.Store.Backend != "helix" {
		t.Fatalf("store.backend = %q", loaded.Config.Store.Backend)
	}
	if loaded.Config.Store.Helix.URL != "http://user-helix:6969" {
		t.Fatalf("helix.url = %q", loaded.Config.Store.Helix.URL)
	}
	if loaded.Config.Store.Helix.APIKeyEnv != "USER_HELIX_KEY" {
		t.Fatalf("helix.api_key_env = %q", loaded.Config.Store.Helix.APIKeyEnv)
	}
	if loaded.Config.Store.Helix.ProjectID != "project-a" {
		t.Fatalf("helix.project_id = %q", loaded.Config.Store.Helix.ProjectID)
	}
	if loaded.Config.Server.Port != 9090 {
		t.Fatalf("server.port = %d", loaded.Config.Server.Port)
	}
	if loaded.Config.Watch.Enabled {
		t.Fatalf("watch.enabled = true, want false project override")
	}
	if loaded.Config.Watch.Interval != 5*time.Second {
		t.Fatalf("watch.interval = %s", loaded.Config.Watch.Interval)
	}
	if loaded.Config.Docs.FailOnBroken {
		t.Fatalf("docs.fail_on_broken = true, want false project override")
	}
}
