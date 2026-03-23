package store

import (
	"context"
	"os"
	"testing"

	"github.com/sjzsdu/code-context/internal/api"
)

// newTestStore creates a temporary SQLite DB, initializes the store
// and returns the store instance along with a cleanup function.
func newTestStore(t *testing.T) (Store, func()) {
	t.Helper()
	// Create a temporary file for sqlite database
	f, err := os.CreateTemp("", "code_memory_store_*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	dbPath := f.Name()
	// sqlite will open the file; close the descriptor created by CreateTemp
	// so that the sqlite driver can manage the file
	_ = f.Close()

	st, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	if err := st.Init(context.Background()); err != nil {
		t.Fatalf("init store: %v", err)
	}

	cleanup := func() {
		_ = st.Close()
		_ = os.Remove(dbPath)
	}
	return st, cleanup
}

func TestInit(t *testing.T) {
	st, clean := newTestStore(t)
	defer clean()
	// Init again to ensure schema can be re-run without error
	if err := st.Init(context.Background()); err != nil {
		t.Fatalf("init should succeed: %v", err)
	}
}

func TestUpsertAndGetFile(t *testing.T) {
	st, clean := newTestStore(t)
	defer clean()

	f := &api.FileInfo{Path: "src/main.go", Language: api.Go, ContentHash: "hash1", Size: 123}
	ctx := context.Background()
	if _, err := st.UpsertFile(ctx, f); err != nil {
		t.Fatalf("upsert file: %v", err)
	}
	got, err := st.GetFile(ctx, f.Path)
	if err != nil {
		t.Fatalf("get file: %v", err)
	}
	if got == nil {
		t.Fatalf("expected file, got nil")
	}
	if got.Path != f.Path || got.Language != f.Language || got.ContentHash != f.ContentHash || got.Size != f.Size {
		t.Fatalf("mismatched file fields: got=%v want=%v", got, f)
	}
}

func TestUpsertFileUpdate(t *testing.T) {
	st, clean := newTestStore(t)
	defer clean()
	ctx := context.Background()
	f1 := &api.FileInfo{Path: "src/utils.go", Language: api.Go, ContentHash: "h1", Size: 42}
	id, err := st.UpsertFile(ctx, f1)
	if err != nil {
		t.Fatalf("upsert first: %v", err)
	}
	// Update with new hash/size
	f2 := &api.FileInfo{Path: "src/utils.go", Language: api.Go, ContentHash: "h2", Size: 84}
	if _, err := st.UpsertFile(ctx, f2); err != nil {
		t.Fatalf("upsert update: %v", err)
	}
	got, err := st.GetFile(ctx, f2.Path)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got == nil || got.ContentHash != f2.ContentHash || got.Size != f2.Size {
		t.Fatalf("update not reflected: got=%v", got)
	}
	_ = id
}

func TestDeleteFile(t *testing.T) {
	st, clean := newTestStore(t)
	defer clean()
	ctx := context.Background()
	f := &api.FileInfo{Path: "del/me.go", Language: api.Go, ContentHash: "d", Size: 1}
	if _, err := st.UpsertFile(ctx, f); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := st.DeleteFile(ctx, f.Path); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err := st.GetFile(ctx, f.Path)
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil after delete, got: %v", got)
	}
}

func TestListFiles(t *testing.T) {
	st, clean := newTestStore(t)
	defer clean()
	ctx := context.Background()
	files := []*api.FileInfo{
		{Path: "a.go", Language: api.Go, ContentHash: "a", Size: 10},
		{Path: "b.ts", Language: api.TypeScript, ContentHash: "b", Size: 20},
		{Path: "c.java", Language: api.Java, ContentHash: "c", Size: 30},
	}
	for _, f := range files {
		if _, err := st.UpsertFile(ctx, f); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	all, err := st.ListFiles(ctx, nil)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != len(files) {
		t.Fatalf("expected %d files, got %d", len(files), len(all))
	}
	lang := api.Java
	byLang, err := st.ListFiles(ctx, &lang)
	if err != nil {
		t.Fatalf("list by lang: %v", err)
	}
	// Only one Java file
	if len(byLang) != 1 {
		t.Fatalf("expected 1 java file, got %d", len(byLang))
	}
}

func TestReplaceSymbols(t *testing.T) {
	st, clean := newTestStore(t)
	defer clean()
	ctx := context.Background()
	f := &api.FileInfo{Path: "sym/defs.go", Language: api.Go, ContentHash: "h", Size: 5}
	id, err := st.UpsertFile(ctx, f)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	g, _ := st.GetFile(ctx, f.Path)
	if g == nil {
		t.Fatalf("could not fetch file")
	}
	syms := []api.Symbol{{Name: "ComputeFoo", Kind: api.Function, Line: 1, EndLine: 1, Signature: "func ComputeFoo()", Parent: ""}}
	if err := st.ReplaceSymbols(ctx, id, syms); err != nil {
		t.Fatalf("replace symbols: %v", err)
	}
	got, err := st.GetFileSymbols(ctx, f.Path)
	if err != nil {
		t.Fatalf("get file symbols: %v", err)
	}
	if len(got) != 1 || got[0].Name != "ComputeFoo" {
		t.Fatalf("unexpected symbols: %#v", got)
	}
}

func TestReplaceImports(t *testing.T) {
	st, clean := newTestStore(t)
	defer clean()
	ctx := context.Background()
	f := &api.FileInfo{Path: "imp/one.go", Language: api.Go, ContentHash: "h", Size: 7}
	id, err := st.UpsertFile(ctx, f)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := st.ReplaceImports(ctx, id, []api.ImportEdge{{FromFile: f.Path, ToSource: "fmt", Line: 1}}); err != nil {
		t.Fatalf("replace imports: %v", err)
	}
	got, err := st.GetImports(ctx, f.Path)
	if err != nil {
		t.Fatalf("get imports: %v", err)
	}
	if len(got) != 1 || got[0].ToSource != "fmt" {
		t.Fatalf("unexpected imports: %#v", got)
	}
}

func TestSearchSymbols(t *testing.T) {
	st, clean := newTestStore(t)
	defer clean()
	ctx := context.Background()
	f := &api.FileInfo{Path: "s/search.go", Language: api.Go, ContentHash: "h", Size: 9}
	id, err := st.UpsertFile(ctx, f)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := st.ReplaceSymbols(ctx, id, []api.Symbol{{Name: "ComputeSearch", Kind: api.Function, Line: 1, EndLine: 1, Signature: "func ComputeSearch()", Parent: ""}}); err != nil {
		t.Fatalf("replace symbols: %v", err)
	}
	res, err := st.SearchSymbols(ctx, "ComputeSearch", nil, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) == 0 || res[0].Name != "ComputeSearch" {
		t.Fatalf("unexpected search results: %#v", res)
	}
}

func TestSearchSymbolsWithKind(t *testing.T) {
	st, clean := newTestStore(t)
	defer clean()
	ctx := context.Background()
	f := &api.FileInfo{Path: "s/kind.go", Language: api.Go, ContentHash: "h", Size: 8}
	id, err := st.UpsertFile(ctx, f)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	syms := []api.Symbol{{Name: "Alpha", Kind: api.Function, Line: 1, EndLine: 1, Signature: "func Alpha()", Parent: ""}, {Name: "Beta", Kind: api.Type, Line: 1, EndLine: 1, Signature: "type Beta struct{}", Parent: ""}}
	if err := st.ReplaceSymbols(ctx, id, syms); err != nil {
		t.Fatalf("replace symbols: %v", err)
	}
	var kindVar = api.Function
	res, err := st.SearchSymbols(ctx, "Alpha", &kindVar, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) != 1 || res[0].Name != "Alpha" {
		t.Fatalf("unexpected filtered results: %#v", res)
	}
}

func TestFindDefinitions(t *testing.T) {
	st, clean := newTestStore(t)
	defer clean()
	ctx := context.Background()
	f := &api.FileInfo{Path: "defs.go", Language: api.Go, ContentHash: "h", Size: 4}
	id, err := st.UpsertFile(ctx, f)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := st.ReplaceSymbols(ctx, id, []api.Symbol{{Name: "Compute", Kind: api.Function, Line: 1, EndLine: 1, Signature: "func Compute()", Parent: ""}, {Name: "MyStruct", Kind: api.Type, Line: 1, EndLine: 1, Signature: "type MyStruct struct{}", Parent: ""}}); err != nil {
		t.Fatalf("replace symbols: %v", err)
	}
	got, err := st.FindDefinitions(ctx, "Compute")
	if err != nil {
		t.Fatalf("find definitions: %v", err)
	}
	if len(got) == 0 || got[0].Name != "Compute" {
		t.Fatalf("expected to find definition Compute, got: %#v", got)
	}
}

func TestGetImporters(t *testing.T) {
	st, clean := newTestStore(t)
	defer clean()
	ctx := context.Background()
	f := &api.FileInfo{Path: "impers/main.go", Language: api.Go, ContentHash: "h", Size: 12}
	id, err := st.UpsertFile(ctx, f)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := st.ReplaceImports(ctx, id, []api.ImportEdge{{FromFile: f.Path, ToSource: "fmt", Line: 1}}); err != nil {
		t.Fatalf("replace imports: %v", err)
	}
	res, err := st.GetImporters(ctx, "fmt")
	if err != nil {
		t.Fatalf("get importers: %v", err)
	}
	if len(res) == 0 || res[0].ToSource != "fmt" {
		t.Fatalf("unexpected importers: %#v", res)
	}
}

func TestStats(t *testing.T) {
	st, clean := newTestStore(t)
	defer clean()
	ctx := context.Background()
	f := &api.FileInfo{Path: "stats.go", Language: api.Go, ContentHash: "h", Size: 11}
	if _, err := st.UpsertFile(ctx, f); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := st.UpsertFile(ctx, &api.FileInfo{Path: "a/b.java", Language: api.Java, ContentHash: "h2", Size: 22}); err != nil {
		t.Fatalf("upsert2: %v", err)
	}
	stt, err := st.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stt.TotalFiles == 0 || stt.TotalSymbols < 0 || stt.TotalImports < 0 {
		t.Fatalf("unexpected stats: %#v", stt)
	}
}

func TestCascadeDelete(t *testing.T) {
	st, clean := newTestStore(t)
	defer clean()
	ctx := context.Background()
	f := &api.FileInfo{Path: "cascade.go", Language: api.Go, ContentHash: "h", Size: 9}
	id, err := st.UpsertFile(ctx, f)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	syms := []api.Symbol{{Name: "ToBeDeleted", Kind: api.Function, Line: 1, EndLine: 1, Signature: "func ToBeDeleted()"}}
	if err := st.ReplaceSymbols(ctx, id, syms); err != nil {
		t.Fatalf("replace symbols: %v", err)
	}
	if err := st.ReplaceImports(ctx, id, []api.ImportEdge{{FromFile: f.Path, ToSource: "fmt", Line: 1}}); err != nil {
		t.Fatalf("replace imports: %v", err)
	}
	if err := st.DeleteFile(ctx, f.Path); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got, _ := st.GetFile(ctx, f.Path); got != nil {
		t.Fatalf("expected file to be deleted, got: %v", got)
	}
	si, err := st.GetFileSymbols(ctx, f.Path)
	if err != nil {
		t.Fatalf("get symbols after cascade: %v", err)
	}
	if len(si) != 0 {
		t.Fatalf("expected 0 symbols after cascade, got: %d", len(si))
	}
	ii, err := st.GetImports(ctx, f.Path)
	if err != nil {
		t.Fatalf("get imports after cascade: %v", err)
	}
	if len(ii) != 0 {
		t.Fatalf("expected 0 imports after cascade, got: %d", len(ii))
	}
}
