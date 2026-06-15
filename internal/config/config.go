package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var ErrNotFound = errors.New("code-context config not found")

var configNames = []string{
	filepath.Join(".code-context", "config.yaml"),
	filepath.Join(".code-context", "config.yml"),
	filepath.Join(".code-context", "config.json"),
	".code-context.yaml",
	".code-context.yml",
	".code-context.json",
}

type Config struct {
	Root   string       `json:"root" yaml:"root"`
	DB     string       `json:"db" yaml:"db"`
	Store  StoreConfig  `json:"store" yaml:"store"`
	Server ServerConfig `json:"server" yaml:"server"`
	Watch  WatchConfig  `json:"watch" yaml:"watch"`
	Docs   DocsConfig   `json:"docs" yaml:"docs"`
}

type StoreConfig struct {
	Backend string            `json:"backend" yaml:"backend"`
	SQLite  SQLiteStoreConfig `json:"sqlite" yaml:"sqlite"`
	Helix   HelixStoreConfig  `json:"helix" yaml:"helix"`
}

type SQLiteStoreConfig struct {
	DB string `json:"db" yaml:"db"`
}

type HelixStoreConfig struct {
	URL       string `json:"url" yaml:"url"`
	APIKey    string `json:"api_key" yaml:"api_key"`
	APIKeyEnv string `json:"api_key_env" yaml:"api_key_env"`
	ProjectID string `json:"project_id" yaml:"project_id"`
}

type ServerConfig struct {
	Port int `json:"port" yaml:"port"`
}

type WatchConfig struct {
	Enabled  bool          `json:"enabled" yaml:"enabled"`
	Interval time.Duration `json:"interval" yaml:"interval"`
	Debounce time.Duration `json:"debounce" yaml:"debounce"`
}

type DocsConfig struct {
	FailOnBroken      bool     `json:"fail_on_broken" yaml:"fail_on_broken"`
	MinRouteCoverage  *float64 `json:"min_route_coverage" yaml:"min_route_coverage"`
	MinSymbolCoverage *float64 `json:"min_symbol_coverage" yaml:"min_symbol_coverage"`
}

type Loaded struct {
	// Path is the highest-priority config path loaded. It is kept for backward compatibility.
	Path    string
	Sources []Source
	Config  Config
	fields  configFields
}

type Source struct {
	Path   string
	Config Config
	fields configFields
}

type configFields struct {
	Root                  bool
	DB                    bool
	StoreBackend          bool
	StoreSQLiteDB         bool
	StoreHelixURL         bool
	StoreHelixAPIKey      bool
	StoreHelixAPIKeyEnv   bool
	StoreHelixProjectID   bool
	ServerPort            bool
	WatchEnabled          bool
	WatchInterval         bool
	WatchDebounce         bool
	DocsFailOnBroken      bool
	DocsMinRouteCoverage  bool
	DocsMinSymbolCoverage bool
}

func Load(startDir string) (*Loaded, error) {
	if startDir == "" {
		var err error
		startDir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}

	absStart, err := filepath.Abs(startDir)
	if err != nil {
		return nil, err
	}

	paths, err := findConfigPaths(absStart)
	if err != nil {
		return nil, err
	}

	loaded := &Loaded{}
	for _, configPath := range paths {
		source, err := loadSource(configPath)
		if err != nil {
			return nil, err
		}
		loaded.Sources = append(loaded.Sources, *source)
		mergeConfig(&loaded.Config, &loaded.fields, source.Config, source.fields)
		loaded.Path = source.Path
	}

	if len(loaded.Sources) == 0 {
		return nil, ErrNotFound
	}

	return loaded, nil
}

func loadSource(configPath string) (*Source, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	switch filepath.Ext(configPath) {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	case ".json":
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported config format: %s", configPath)
	}

	baseDir := configBaseDir(configPath)
	if cfg.Root != "" {
		cfg.Root = resolvePath(baseDir, cfg.Root)
	}
	if cfg.DB != "" {
		cfg.DB = resolvePath(baseDir, cfg.DB)
	}
	if cfg.Store.SQLite.DB != "" {
		cfg.Store.SQLite.DB = resolvePath(baseDir, cfg.Store.SQLite.DB)
	}

	fields := detectFields(data, configPath)

	return &Source{Path: configPath, Config: cfg, fields: fields}, nil
}

func findConfig(startDir string) (string, error) {
	paths, err := findConfigPaths(startDir)
	if err != nil {
		return "", err
	}
	return paths[len(paths)-1], nil
}

func findConfigPaths(startDir string) ([]string, error) {
	var paths []string

	userConfig, err := findUserConfig()
	if err != nil {
		return nil, err
	}
	if userConfig != "" {
		paths = append(paths, userConfig)
	}

	projectConfig, err := findProjectConfig(startDir)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			projectConfig = ""
		} else {
			return nil, err
		}
	}
	if projectConfig != "" {
		paths = append(paths, projectConfig)
	}

	if len(paths) == 0 {
		return nil, ErrNotFound
	}
	return paths, nil
}

func findUserConfig() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", nil
	}
	for _, name := range []string{
		filepath.Join(".code-context", "config.yaml"),
		filepath.Join(".code-context", "config.yml"),
		filepath.Join(".code-context", "config.json"),
	} {
		path := filepath.Join(home, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return "", nil
}

func findProjectConfig(startDir string) (string, error) {
	dir := startDir
	for {
		for _, name := range configNames {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil {
				return path, nil
			} else if !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNotFound
		}
		dir = parent
	}
}

func configBaseDir(configPath string) string {
	baseDir := filepath.Dir(configPath)
	if filepath.Base(baseDir) == ".code-context" {
		return filepath.Dir(baseDir)
	}
	return baseDir
}

func resolvePath(baseDir string, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	if strings.HasPrefix(value, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			if value == "~" {
				return home
			}
			if strings.HasPrefix(value, "~/") {
				return filepath.Join(home, strings.TrimPrefix(value, "~/"))
			}
		}
	}
	return filepath.Clean(filepath.Join(baseDir, value))
}

func detectFields(data []byte, configPath string) configFields {
	var raw map[string]any
	switch filepath.Ext(configPath) {
	case ".yaml", ".yml":
		_ = yaml.Unmarshal(data, &raw)
	case ".json":
		_ = json.Unmarshal(data, &raw)
	}
	return fieldsFromMap(raw)
}

func fieldsFromMap(raw map[string]any) configFields {
	var f configFields
	if raw == nil {
		return f
	}
	f.Root = has(raw, "root")
	f.DB = has(raw, "db")

	if store := nested(raw, "store"); store != nil {
		f.StoreBackend = has(store, "backend")
		if sqlite := nested(store, "sqlite"); sqlite != nil {
			f.StoreSQLiteDB = has(sqlite, "db")
		}
		if helix := nested(store, "helix"); helix != nil {
			f.StoreHelixURL = has(helix, "url")
			f.StoreHelixAPIKey = has(helix, "api_key")
			f.StoreHelixAPIKeyEnv = has(helix, "api_key_env")
			f.StoreHelixProjectID = has(helix, "project_id")
		}
	}
	if server := nested(raw, "server"); server != nil {
		f.ServerPort = has(server, "port")
	}
	if watch := nested(raw, "watch"); watch != nil {
		f.WatchEnabled = has(watch, "enabled")
		f.WatchInterval = has(watch, "interval")
		f.WatchDebounce = has(watch, "debounce")
	}
	if docs := nested(raw, "docs"); docs != nil {
		f.DocsFailOnBroken = has(docs, "fail_on_broken")
		f.DocsMinRouteCoverage = has(docs, "min_route_coverage")
		f.DocsMinSymbolCoverage = has(docs, "min_symbol_coverage")
	}
	return f
}

func nested(raw map[string]any, key string) map[string]any {
	v, ok := raw[key]
	if !ok {
		return nil
	}
	m, _ := v.(map[string]any)
	return m
}

func has(raw map[string]any, key string) bool {
	_, ok := raw[key]
	return ok
}

func mergeConfig(dst *Config, dstFields *configFields, src Config, srcFields configFields) {
	if srcFields.Root {
		dst.Root = src.Root
		dstFields.Root = true
	}
	if srcFields.DB {
		dst.DB = src.DB
		dstFields.DB = true
	}
	if srcFields.StoreBackend {
		dst.Store.Backend = src.Store.Backend
		dstFields.StoreBackend = true
	}
	if srcFields.StoreSQLiteDB {
		dst.Store.SQLite.DB = src.Store.SQLite.DB
		dstFields.StoreSQLiteDB = true
	}
	if srcFields.StoreHelixURL {
		dst.Store.Helix.URL = src.Store.Helix.URL
		dstFields.StoreHelixURL = true
	}
	if srcFields.StoreHelixAPIKey {
		dst.Store.Helix.APIKey = src.Store.Helix.APIKey
		dstFields.StoreHelixAPIKey = true
	}
	if srcFields.StoreHelixAPIKeyEnv {
		dst.Store.Helix.APIKeyEnv = src.Store.Helix.APIKeyEnv
		dstFields.StoreHelixAPIKeyEnv = true
	}
	if srcFields.StoreHelixProjectID {
		dst.Store.Helix.ProjectID = src.Store.Helix.ProjectID
		dstFields.StoreHelixProjectID = true
	}
	if srcFields.ServerPort {
		dst.Server.Port = src.Server.Port
		dstFields.ServerPort = true
	}
	if srcFields.WatchEnabled {
		dst.Watch.Enabled = src.Watch.Enabled
		dstFields.WatchEnabled = true
	}
	if srcFields.WatchInterval {
		dst.Watch.Interval = src.Watch.Interval
		dstFields.WatchInterval = true
	}
	if srcFields.WatchDebounce {
		dst.Watch.Debounce = src.Watch.Debounce
		dstFields.WatchDebounce = true
	}
	if srcFields.DocsFailOnBroken {
		dst.Docs.FailOnBroken = src.Docs.FailOnBroken
		dstFields.DocsFailOnBroken = true
	}
	if srcFields.DocsMinRouteCoverage {
		dst.Docs.MinRouteCoverage = src.Docs.MinRouteCoverage
		dstFields.DocsMinRouteCoverage = true
	}
	if srcFields.DocsMinSymbolCoverage {
		dst.Docs.MinSymbolCoverage = src.Docs.MinSymbolCoverage
		dstFields.DocsMinSymbolCoverage = true
	}
}
