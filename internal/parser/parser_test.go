package parser

import (
	"context"
	"testing"

	"github.com/sjzsdu/code-context/internal/api"
	"github.com/sjzsdu/code-context/internal/lang"
)

func newTestParser() Parser {
	return NewTreeSitterParser(lang.NewRegistry())
}

func TestSupportsLanguage(t *testing.T) {
	p := newTestParser()

	supported := []api.Language{api.Go, api.TypeScript, api.JavaScript, api.Python, api.Rust, api.Java}
	for _, l := range supported {
		if !p.SupportsLanguage(l) {
			t.Errorf("SupportsLanguage(%q) = false, want true", l)
		}
	}

	unsupported := []api.Language{"c", "cpp", "ruby", "php", ""}
	for _, l := range unsupported {
		if p.SupportsLanguage(l) {
			t.Errorf("SupportsLanguage(%q) = true, want false", l)
		}
	}
}

func TestParse_Go(t *testing.T) {
	p := newTestParser()
	code := []byte(`package main

import "fmt"

const Version = "1.0"

type Server struct {
	Port int
}

func (s *Server) Start() error {
	return nil
}

func main() {
	fmt.Println("hello")
}
`)

	result, err := p.Parse(context.Background(), "main.go", code, api.Go)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	expectSymbol(t, result.Symbols, "main", api.Package)
	expectSymbol(t, result.Symbols, "main", api.Function)
	expectSymbol(t, result.Symbols, "Version", api.Constant)
	expectSymbol(t, result.Symbols, "Server", api.Type)
	expectSymbol(t, result.Symbols, "Start", api.Method)

	mainCount := countSymbol(result.Symbols, "main")
	if mainCount != 2 {
		t.Errorf("expected 2 'main' symbols (package + function), got %d", mainCount)
	}

	for _, s := range result.Symbols {
		if s.FilePath != "main.go" {
			t.Errorf("symbol %q: FilePath = %q, want %q", s.Name, s.FilePath, "main.go")
		}
		if s.Line <= 0 {
			t.Errorf("symbol %q: Line = %d, want > 0", s.Name, s.Line)
		}
	}

	expectImport(t, result.Imports, "fmt", "main.go")
}

func TestParse_TypeScript(t *testing.T) {
	p := newTestParser()
	code := []byte(`import { useState } from 'react';

interface Props {
    name: string;
}

export function Hello(props: Props) {
    return props.name;
}

export class Greeter {
    greet() { return "hi"; }
}
`)

	result, err := p.Parse(context.Background(), "app.ts", code, api.TypeScript)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	expectSymbol(t, result.Symbols, "Hello", api.Function)
	expectSymbol(t, result.Symbols, "Greeter", api.Class)
	expectSymbol(t, result.Symbols, "Props", api.Interface)
	expectSymbol(t, result.Symbols, "greet", api.Method)

	for _, s := range result.Symbols {
		if s.FilePath != "app.ts" {
			t.Errorf("symbol %q: FilePath = %q, want %q", s.Name, s.FilePath, "app.ts")
		}
	}

	expectImport(t, result.Imports, "react", "app.ts")
}

func TestParse_Python(t *testing.T) {
	p := newTestParser()
	code := []byte(`import os
from pathlib import Path

def hello(name: str) -> str:
    return f"Hello {name}"

class Greeter:
    def greet(self):
        return "hi"
`)

	result, err := p.Parse(context.Background(), "app.py", code, api.Python)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	expectSymbol(t, result.Symbols, "hello", api.Function)
	expectSymbol(t, result.Symbols, "Greeter", api.Class)
	expectSymbol(t, result.Symbols, "greet", api.Function)

	for _, s := range result.Symbols {
		if s.FilePath != "app.py" {
			t.Errorf("symbol %q: FilePath = %q, want %q", s.Name, s.FilePath, "app.py")
		}
	}

	expectImport(t, result.Imports, "os", "app.py")
	expectImport(t, result.Imports, "pathlib", "app.py")
}

func TestParse_UnsupportedLanguage(t *testing.T) {
	p := newTestParser()
	_, err := p.Parse(context.Background(), "file.c", []byte("int main() {}"), "c")
	if err == nil {
		t.Error("expected error for unsupported language, got nil")
	}
}

func TestParse_EmptyFile(t *testing.T) {
	p := newTestParser()
	result, err := p.Parse(context.Background(), "empty.go", []byte(""), api.Go)
	if err != nil {
		t.Fatalf("Parse() error on empty file: %v", err)
	}
	if len(result.Symbols) != 0 {
		t.Errorf("expected 0 symbols for empty file, got %d", len(result.Symbols))
	}
	if len(result.Imports) != 0 {
		t.Errorf("expected 0 imports for empty file, got %d", len(result.Imports))
	}
}

func expectSymbol(t *testing.T, symbols []api.Symbol, name string, kind api.SymbolKind) {
	t.Helper()
	for _, s := range symbols {
		if s.Name == name && s.Kind == kind {
			return
		}
	}
	t.Errorf("missing symbol %q (kind %q); got: %v", name, kind, formatSymbols(symbols))
}

func expectImport(t *testing.T, imports []api.ImportEdge, source string, fromFile string) {
	t.Helper()
	for _, imp := range imports {
		if imp.ToSource == source {
			if imp.FromFile != fromFile {
				t.Errorf("import %q: FromFile = %q, want %q", source, imp.FromFile, fromFile)
			}
			if imp.Line <= 0 {
				t.Errorf("import %q: Line = %d, want > 0", source, imp.Line)
			}
			return
		}
	}
	t.Errorf("missing import %q; got: %v", source, formatImports(imports))
}

func countSymbol(symbols []api.Symbol, name string) int {
	n := 0
	for _, s := range symbols {
		if s.Name == name {
			n++
		}
	}
	return n
}

func formatSymbols(symbols []api.Symbol) []string {
	out := make([]string, len(symbols))
	for i, s := range symbols {
		out[i] = s.Name + "(" + string(s.Kind) + ")"
	}
	return out
}

func formatImports(imports []api.ImportEdge) []string {
	out := make([]string, len(imports))
	for i, imp := range imports {
		out[i] = imp.ToSource
	}
	return out
}
