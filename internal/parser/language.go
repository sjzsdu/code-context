package parser

import (
	"path/filepath"
	"strings"

	"github.com/sjzsdu/code-context/internal/api"
)

// extMap maps file extensions to supported languages.
var extMap = map[string]api.Language{
	".go": api.Go,
	".ts": api.TypeScript, ".tsx": api.TypeScript,
	".js": api.JavaScript, ".jsx": api.JavaScript, ".mjs": api.JavaScript,
	".py":   api.Python,
	".rs":   api.Rust,
	".java": api.Java,
}

// DetectLanguage detects the language from a file path based on extension.
func DetectLanguage(path string) (api.Language, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	lang, ok := extMap[ext]
	return lang, ok
}

// Common non-code directories to skip during indexing.
var skipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"__pycache__":  true,
	".git":         true,
	".idea":        true,
	".vscode":      true,
	"target":       true, // Rust
	"build":        true,
	"dist":         true,
	"venv":         true,
	".venv":        true,
}

// IsSkipDir returns true if the directory should be skipped during indexing.
func IsSkipDir(name string) bool {
	return skipDirs[name]
}
