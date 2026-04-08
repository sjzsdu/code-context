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
	".code-context.yaml",
	".code-context.yml",
	".code-context.json",
}

type Config struct {
	Root   string       `json:"root" yaml:"root"`
	DB     string       `json:"db" yaml:"db"`
	Server ServerConfig `json:"server" yaml:"server"`
	Watch  WatchConfig  `json:"watch" yaml:"watch"`
}

type ServerConfig struct {
	Port int `json:"port" yaml:"port"`
}

type WatchConfig struct {
	Enabled  bool          `json:"enabled" yaml:"enabled"`
	Interval time.Duration `json:"interval" yaml:"interval"`
	Debounce time.Duration `json:"debounce" yaml:"debounce"`
}

type Loaded struct {
	Path   string
	Config Config
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

	configPath, err := findConfig(absStart)
	if err != nil {
		return nil, err
	}

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

	baseDir := filepath.Dir(configPath)
	if cfg.Root != "" {
		cfg.Root = resolvePath(baseDir, cfg.Root)
	}
	if cfg.DB != "" {
		cfg.DB = resolvePath(baseDir, cfg.DB)
	}

	return &Loaded{Path: configPath, Config: cfg}, nil
}

func findConfig(startDir string) (string, error) {
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
