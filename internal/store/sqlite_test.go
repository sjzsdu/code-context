package store

import (
	"context"
	"database/sql"
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

func TestSchemaStatusIncludesMigrationVersion(t *testing.T) {
	st, clean := newTestStore(t)
	defer clean()

	status, err := st.SchemaStatus(context.Background())
	if err != nil {
		t.Fatalf("schema status: %v", err)
	}
	if status.ExpectedVersion != SchemaVersion {
		t.Fatalf("expected version %q, got %q", SchemaVersion, status.ExpectedVersion)
	}
	if status.AppliedVersion != SchemaVersion || !status.VersionOK {
		t.Fatalf("migration version not recorded: %+v", status)
	}
	if len(status.MissingTables) > 0 || len(status.MissingIndexes) > 0 {
		t.Fatalf("unexpected missing schema objects: %+v", status)
	}
}

func TestInitMigratesLegacyDocumentLinks(t *testing.T) {
	f, err := os.CreateTemp("", "code_memory_legacy_*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	dbPath := f.Name()
	_ = f.Close()
	defer os.Remove(dbPath)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	_, err = db.Exec(`
CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at INTEGER NOT NULL DEFAULT (unixepoch()));
INSERT INTO schema_migrations(version) VALUES ('schema.v1.code-context');
CREATE TABLE documents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT UNIQUE NOT NULL,
    language TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    title TEXT,
    summary TEXT,
    size INTEGER NOT NULL DEFAULT 0,
    indexed_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE TABLE document_links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    document_id INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    target_type TEXT NOT NULL,
    target_value TEXT NOT NULL,
    line INTEGER NOT NULL DEFAULT 0,
    evidence TEXT,
    confidence REAL NOT NULL DEFAULT 1.0
);`)
	_ = db.Close()
	if err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}

	st, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if err := st.Init(context.Background()); err != nil {
		t.Fatalf("init migrates legacy schema: %v", err)
	}
	status, err := st.SchemaStatus(context.Background())
	if err != nil {
		t.Fatalf("schema status: %v", err)
	}
	if status.AppliedVersion != SchemaVersion || !status.VersionOK {
		t.Fatalf("expected latest schema after migration, got %+v", status)
	}
	for _, col := range []string{"section_title", "section_slug", "section_line"} {
		if !documentLinkColumnExists(t, st, col) {
			t.Fatalf("expected migrated column %s", col)
		}
	}
	if _, err := st.(*sqliteStore).db.ExecContext(context.Background(), `SELECT COUNT(*) FROM embedding_cache`); err != nil {
		t.Fatalf("expected embedding_cache migration: %v", err)
	}
}

func documentLinkColumnExists(t *testing.T, st Store, column string) bool {
	t.Helper()
	s := st.(*sqliteStore)
	rows, err := s.db.QueryContext(context.Background(), `PRAGMA table_info(document_links)`)
	if err != nil {
		t.Fatalf("table info: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan table info: %v", err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table info rows: %v", err)
	}
	return false
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

func TestEmbeddingCacheUpsertAndGet(t *testing.T) {
	st, clean := newTestStore(t)
	defer clean()
	cache, ok := st.(EmbeddingCache)
	if !ok {
		t.Fatalf("sqlite store does not implement EmbeddingCache")
	}

	entry := EmbeddingCacheEntry{
		Key:         "cache-key",
		Model:       "text-embedding-test",
		Dimensions:  3,
		ContentHash: "hash",
		InputKind:   EmbeddingInputSymbol,
		Target: TargetRef{
			Kind:    TargetSymbol,
			Path:    "main.go",
			Name:    "HealthHandler",
			Type:    "function",
			Line:    10,
			EndLine: 12,
		},
		Values:   []float32{0.1, 0.2, 0.3},
		Metadata: map[string]string{"kind": "function"},
	}
	if err := cache.UpsertEmbedding(context.Background(), entry); err != nil {
		t.Fatalf("upsert embedding: %v", err)
	}
	got, err := cache.GetEmbedding(context.Background(), entry.Key)
	if err != nil {
		t.Fatalf("get embedding: %v", err)
	}
	if got == nil {
		t.Fatalf("expected cache entry")
	}
	if got.Model != entry.Model || got.Dimensions != entry.Dimensions || got.ContentHash != entry.ContentHash {
		t.Fatalf("unexpected entry metadata: %+v", got)
	}
	if got.Target.Kind != TargetSymbol || got.Target.Name != "HealthHandler" {
		t.Fatalf("unexpected target: %+v", got.Target)
	}
	if len(got.Values) != 3 || got.Values[2] != float32(0.3) {
		t.Fatalf("unexpected vector: %+v", got.Values)
	}
	if got.Metadata["kind"] != "function" {
		t.Fatalf("unexpected metadata: %+v", got.Metadata)
	}
}

func TestEmbeddingCacheListNamespaces(t *testing.T) {
	st, clean := newTestStore(t)
	defer clean()
	cache, ok := st.(EmbeddingCache)
	if !ok {
		t.Fatalf("sqlite store does not implement EmbeddingCache")
	}
	inspector, ok := st.(EmbeddingCacheInspector)
	if !ok {
		t.Fatalf("sqlite store does not implement EmbeddingCacheInspector")
	}
	ctx := context.Background()
	entries := []EmbeddingCacheEntry{
		{
			Key:         "model-b-doc",
			Model:       "model-b",
			Dimensions:  2,
			ContentHash: "hash-b",
			InputKind:   EmbeddingInputDocument,
			Target:      TargetRef{Kind: TargetDocument, Path: "README.md"},
			Values:      []float32{0.1, 0.2},
		},
		{
			Key:         "model-a-symbol",
			Model:       "model-a",
			Dimensions:  3,
			ContentHash: "hash-a1",
			InputKind:   EmbeddingInputSymbol,
			Target:      TargetRef{Kind: TargetSymbol, Path: "main.go", Name: "Foo"},
			Values:      []float32{0.1, 0.2, 0.3},
		},
		{
			Key:         "model-a-doc",
			Model:       "model-a",
			Dimensions:  3,
			ContentHash: "hash-a2",
			InputKind:   EmbeddingInputDocument,
			Target:      TargetRef{Kind: TargetDocument, Path: "README.md"},
			Values:      []float32{0.4, 0.5, 0.6},
		},
	}
	for _, entry := range entries {
		if err := cache.UpsertEmbedding(ctx, entry); err != nil {
			t.Fatalf("upsert embedding %s: %v", entry.Key, err)
		}
	}

	namespaces, err := inspector.ListEmbeddingNamespaces(ctx)
	if err != nil {
		t.Fatalf("list namespaces: %v", err)
	}
	if len(namespaces) != 2 {
		t.Fatalf("namespaces = %+v, want 2", namespaces)
	}
	first := namespaces[0]
	if first.Model != "model-a" || first.Dimensions != 3 || first.Chunks != 2 {
		t.Fatalf("first namespace = %+v, want model-a/3 with 2 chunks", first)
	}
	if first.InputKinds[EmbeddingInputSymbol] != 1 || first.InputKinds[EmbeddingInputDocument] != 1 {
		t.Fatalf("input kind counts = %+v, want symbol=1 document=1", first.InputKinds)
	}
	if first.TargetKinds[TargetSymbol] != 1 || first.TargetKinds[TargetDocument] != 1 {
		t.Fatalf("target kind counts = %+v, want symbol=1 document=1", first.TargetKinds)
	}
	if namespaces[1].Model != "model-b" || namespaces[1].Dimensions != 2 || namespaces[1].Chunks != 1 {
		t.Fatalf("second namespace = %+v, want model-b/2 with 1 chunk", namespaces[1])
	}

	pruner, ok := st.(EmbeddingCachePruner)
	if !ok {
		t.Fatalf("sqlite store does not implement EmbeddingCachePruner")
	}
	deleted, err := pruner.DeleteEmbeddingNamespace(ctx, "model-a", 3)
	if err != nil {
		t.Fatalf("delete namespace: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2", deleted)
	}
	namespaces, err = inspector.ListEmbeddingNamespaces(ctx)
	if err != nil {
		t.Fatalf("list namespaces after delete: %v", err)
	}
	if len(namespaces) != 1 || namespaces[0].Model != "model-b" || namespaces[0].Chunks != 1 {
		t.Fatalf("namespaces after delete = %+v, want only model-b", namespaces)
	}
	got, err := cache.GetEmbedding(ctx, "model-a-symbol")
	if err != nil {
		t.Fatalf("get deleted embedding: %v", err)
	}
	if got != nil {
		t.Fatalf("deleted embedding still present: %+v", got)
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

func TestGetCallersMatchesQualifiedNamesBySeparator(t *testing.T) {
	st, clean := newTestStore(t)
	defer clean()
	ctx := context.Background()
	f := &api.FileInfo{Path: "calls/main.go", Language: api.Go, ContentHash: "h", Size: 20}
	id, err := st.UpsertFile(ctx, f)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	calls := []api.CallEdge{
		{FromSymbol: "A", ToName: "Target", Line: 1, Confidence: "HEURISTIC"},
		{FromSymbol: "B", ToName: "pkg.Target", Line: 2, Confidence: "HEURISTIC"},
		{FromSymbol: "C", ToName: "mod::Target", Line: 3, Confidence: "HEURISTIC"},
		{FromSymbol: "D", ToName: "MyTarget", Line: 4, Confidence: "HEURISTIC"},
	}
	if err := st.ReplaceCalls(ctx, id, calls); err != nil {
		t.Fatalf("replace calls: %v", err)
	}
	got, err := st.GetCallers(ctx, "Target")
	if err != nil {
		t.Fatalf("get callers: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected exact/dot/colon callers only, got %+v", got)
	}
	for _, call := range got {
		if call.ToName == "MyTarget" {
			t.Fatalf("unexpected suffix false positive: %+v", got)
		}
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
