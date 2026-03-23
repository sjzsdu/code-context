package parser

import (
	"testing"

	"github.com/sjzsdu/code-memory/internal/api"
)

func TestDetectLanguage_SupportedExtensions(t *testing.T) {
	tests := []struct {
		path     string
		wantLang api.Language
	}{
		{"main.go", api.Go},
		{"path/to/file.go", api.Go},
		{"app.ts", api.TypeScript},
		{"component.tsx", api.TypeScript},
		{"index.js", api.JavaScript},
		{"component.jsx", api.JavaScript},
		{"module.mjs", api.JavaScript},
		{"script.py", api.Python},
		{"lib.rs", api.Rust},
		{"Main.java", api.Java},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			lang, ok := DetectLanguage(tt.path)
			if !ok {
				t.Fatalf("DetectLanguage(%q) ok = false, want true", tt.path)
			}
			if lang != tt.wantLang {
				t.Errorf("DetectLanguage(%q) = %q, want %q", tt.path, lang, tt.wantLang)
			}
		})
	}
}

func TestDetectLanguage_UnsupportedExtensions(t *testing.T) {
	unsupported := []string{
		"readme.txt",
		"README.md",
		"config.yaml",
		"main.c",
		"Makefile",
		"",
		"noext",
	}

	for _, path := range unsupported {
		t.Run(path, func(t *testing.T) {
			_, ok := DetectLanguage(path)
			if ok {
				t.Errorf("DetectLanguage(%q) ok = true, want false", path)
			}
		})
	}
}

func TestDetectLanguage_CaseInsensitive(t *testing.T) {
	tests := []struct {
		path     string
		wantLang api.Language
	}{
		{"FILE.GO", api.Go},
		{"App.TS", api.TypeScript},
		{"Module.PY", api.Python},
		{"Lib.RS", api.Rust},
		{"Main.JAVA", api.Java},
		{"index.JS", api.JavaScript},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			lang, ok := DetectLanguage(tt.path)
			if !ok {
				t.Fatalf("DetectLanguage(%q) should detect language, got ok=false", tt.path)
			}
			if lang != tt.wantLang {
				t.Errorf("DetectLanguage(%q) = %q, want %q", tt.path, lang, tt.wantLang)
			}
		})
	}
}

func TestIsSkipDir_KnownDirs(t *testing.T) {
	skipDirNames := []string{
		"node_modules", "vendor", "__pycache__", ".git",
		".idea", ".vscode", "target", "build", "dist",
		"venv", ".venv",
	}
	for _, name := range skipDirNames {
		t.Run(name, func(t *testing.T) {
			if !IsSkipDir(name) {
				t.Errorf("IsSkipDir(%q) = false, want true", name)
			}
		})
	}
}

func TestIsSkipDir_NonSkipDirs(t *testing.T) {
	nonSkipDirs := []string{
		"src", "lib", "internal", "cmd", "pkg", "api", "",
	}
	for _, name := range nonSkipDirs {
		t.Run(name, func(t *testing.T) {
			if IsSkipDir(name) {
				t.Errorf("IsSkipDir(%q) = true, want false", name)
			}
		})
	}
}
