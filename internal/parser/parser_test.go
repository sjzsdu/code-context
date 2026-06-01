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

func TestParseGoCallsSkipImportedQualifiedSelectors(t *testing.T) {
	p := newTestParser()
	code := []byte(`package main

import (
	"fmt"
	alias "example.com/external/pkg"
)

type Worker struct{}

func (w *Worker) Do() {}

func Local() {}

func main() {
	fmt.Println("hello")
	alias.Run()
	Local()
	w := &Worker{}
	w.Do()
}
`)

	result, err := p.Parse(context.Background(), "main.go", code, api.Go)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if hasCall(result.Calls, "fmt.Println") || hasCall(result.Calls, "alias.Run") {
		t.Fatalf("expected imported qualified calls to be skipped, got %+v", result.Calls)
	}
	if !hasCall(result.Calls, "Local") {
		t.Fatalf("expected local call to remain, got %+v", result.Calls)
	}
	if !hasCall(result.Calls, "w.Do") {
		t.Fatalf("expected local receiver-style call to remain, got %+v", result.Calls)
	}
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

func TestExtractRoutesWithFrameworkPrefixes(t *testing.T) {
	tests := []struct {
		name     string
		lang     api.Language
		content  string
		wantPath string
		want     string
	}{
		{
			name: "nestjs controller prefix",
			lang: api.TypeScript,
			content: `@Controller('users')
export class UsersController {
  @Get(':id')
  findOne() { return null }
}`,
			wantPath: "/users/:id",
			want:     "findOne",
		},
		{
			name: "fastapi router prefix",
			lang: api.Python,
			content: `router = APIRouter(prefix="/api/v1")

@router.get("/users/{id}")
def get_user():
    pass`,
			wantPath: "/api/v1/users/{id}",
			want:     "get_user",
		},
		{
			name: "flask blueprint prefix",
			lang: api.Python,
			content: `bp = Blueprint("users", __name__, url_prefix="/api")

@bp.route("/users")
def list_users():
    pass`,
			wantPath: "/api/users",
			want:     "list_users",
		},
		{
			name: "spring class request mapping prefix",
			lang: api.Java,
			content: `@RestController
@RequestMapping("/api")
public class UserController {
  @GetMapping("/users/{id}")
  public User getUser() { return null; }
}`,
			wantPath: "/api/users/{id}",
			want:     "getUser",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routes := extractRoutes("fixture", tt.content, tt.lang)
			expectRoute(t, routes, tt.wantPath, tt.want)
		})
	}
}

func TestExtractCallsSkipsCommentsAndStrings(t *testing.T) {
	content := `package main

func Caller() {
	// FakeCall()
	message := "FakeStringCall()"
	/* FakeBlockCall() */
	RealCall()
}

func RealCall() {}
`
	symbols := []api.Symbol{
		{Name: "Caller", Kind: api.Function, FilePath: "main.go", Line: 3, EndLine: 8},
		{Name: "RealCall", Kind: api.Function, FilePath: "main.go", Line: 10, EndLine: 10},
	}
	calls := extractCalls("main.go", content, api.Go, symbols)
	if len(calls) != 1 {
		t.Fatalf("expected one real call, got %+v", calls)
	}
	if calls[0].FromSymbol != "Caller" || calls[0].ToName != "RealCall" {
		t.Fatalf("unexpected call edge: %+v", calls[0])
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

func expectRoute(t *testing.T, routes []api.Route, path, handler string) {
	t.Helper()
	for _, r := range routes {
		if r.Path == path && r.Handler == handler {
			return
		}
	}
	t.Fatalf("missing route path=%q handler=%q; got: %+v", path, handler, routes)
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

func hasCall(calls []api.CallEdge, toName string) bool {
	for _, call := range calls {
		if call.ToName == toName {
			return true
		}
	}
	return false
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
