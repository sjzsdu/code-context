package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/sjzsdu/code-context/internal/api"
)

//go:embed schema.sql
var schemaSQL string

const SchemaVersion = "schema.v2.code-context"

type schemaMigration struct {
	version string
	apply   func(context.Context, *sqliteStore) error
}

var schemaMigrations = []schemaMigration{
	{version: "schema.v1.code-context", apply: func(context.Context, *sqliteStore) error { return nil }},
	{version: "schema.v2.code-context", apply: migrateDocumentLinkSections},
}

type sqliteStore struct {
	db *sql.DB
}

func NewSQLiteStore(dbPath string) (Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return &sqliteStore{db: db}, nil
}

func (s *sqliteStore) Init(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `PRAGMA busy_timeout = 5000`); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, schemaSQL); err != nil {
		return err
	}
	return s.runSchemaMigrations(ctx)
}

func (s *sqliteStore) runSchemaMigrations(ctx context.Context) error {
	for _, migration := range schemaMigrations {
		applied, err := s.schemaVersionApplied(ctx, migration.version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := migration.apply(ctx, s); err != nil {
			return fmt.Errorf("apply schema migration %s: %w", migration.version, err)
		}
		if err := s.recordSchemaVersion(ctx, migration.version); err != nil {
			return err
		}
	}
	return nil
}

func (s *sqliteStore) schemaVersionApplied(ctx context.Context, version string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&count)
	return count > 0, err
}

func (s *sqliteStore) recordSchemaVersion(ctx context.Context, version string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES (?) ON CONFLICT(version) DO NOTHING`, version)
	return err
}

func (s *sqliteStore) appliedSchemaVersion(ctx context.Context) (string, error) {
	var version string
	err := s.db.QueryRowContext(ctx, `SELECT version FROM schema_migrations ORDER BY applied_at DESC, version DESC LIMIT 1`).Scan(&version)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return version, err
}

func migrateDocumentLinkSections(ctx context.Context, s *sqliteStore) error {
	columns := map[string]string{
		"section_title": `ALTER TABLE document_links ADD COLUMN section_title TEXT NOT NULL DEFAULT ''`,
		"section_slug":  `ALTER TABLE document_links ADD COLUMN section_slug TEXT NOT NULL DEFAULT ''`,
		"section_line":  `ALTER TABLE document_links ADD COLUMN section_line INTEGER NOT NULL DEFAULT 0`,
	}
	existing := map[string]bool{}
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(document_links)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for col, stmt := range columns {
		if !existing[col] {
			if _, err := s.db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *sqliteStore) SchemaStatus(ctx context.Context) (*api.SchemaStatus, error) {
	expectedTables := []string{"schema_migrations", "files", "symbols", "imports", "symbols_fts", "calls", "routes", "documents", "document_links"}
	expectedIndexes := []string{"idx_symbols_name", "idx_symbols_kind", "idx_symbols_file", "idx_imports_source", "idx_imports_file", "idx_calls_from", "idx_calls_to", "idx_calls_file", "idx_routes_path", "idx_routes_handler", "idx_routes_file", "idx_document_links_doc", "idx_document_links_target"}
	tables, err := s.sqliteObjects(ctx, "table")
	if err != nil {
		return nil, err
	}
	indexes, err := s.sqliteObjects(ctx, "index")
	if err != nil {
		return nil, err
	}
	appliedVersion, err := s.appliedSchemaVersion(ctx)
	if err != nil {
		return nil, err
	}
	return &api.SchemaStatus{ExpectedVersion: SchemaVersion, AppliedVersion: appliedVersion, VersionOK: appliedVersion == SchemaVersion, Tables: intersectNames(tables, expectedTables), MissingTables: missingNames(tables, expectedTables), Indexes: intersectNames(indexes, expectedIndexes), MissingIndexes: missingNames(indexes, expectedIndexes)}, nil
}

func (s *sqliteStore) sqliteObjects(ctx context.Context, typ string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = ?`, typ)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

func missingNames(existing map[string]bool, expected []string) []string {
	var out []string
	for _, name := range expected {
		if !existing[name] {
			out = append(out, name)
		}
	}
	return out
}

func intersectNames(existing map[string]bool, expected []string) []string {
	var out []string
	for _, name := range expected {
		if existing[name] {
			out = append(out, name)
		}
	}
	return out
}

func (s *sqliteStore) ResetIndex(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`DELETE FROM document_links`,
		`DELETE FROM documents`,
		`DELETE FROM files`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) UpsertFile(ctx context.Context, f *api.FileInfo) (int64, error) {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO files (path, language, content_hash, size) VALUES (?, ?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET content_hash=excluded.content_hash, size=excluded.size, indexed_at=unixepoch()`,
		f.Path, string(f.Language), f.ContentHash, f.Size)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `SELECT id FROM files WHERE path = ?`, f.Path).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (s *sqliteStore) ReplaceFileIndex(ctx context.Context, idx FileIndex) (int64, error) {
	if idx.File == nil {
		return 0, fmt.Errorf("file index file is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO files (path, language, content_hash, size) VALUES (?, ?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET language=excluded.language, content_hash=excluded.content_hash, size=excluded.size, indexed_at=unixepoch()`,
		idx.File.Path, string(idx.File.Language), idx.File.ContentHash, idx.File.Size); err != nil {
		return 0, err
	}

	var fileID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM files WHERE path = ?`, idx.File.Path).Scan(&fileID); err != nil {
		return 0, err
	}

	for _, stmt := range []string{
		`DELETE FROM symbols WHERE file_id = ?`,
		`DELETE FROM imports WHERE file_id = ?`,
		`DELETE FROM calls WHERE file_id = ?`,
		`DELETE FROM routes WHERE file_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, fileID); err != nil {
			return 0, err
		}
	}

	symStmt, err := tx.PrepareContext(ctx, `INSERT INTO symbols (file_id, name, kind, line, end_line, signature, parent) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer symStmt.Close()
	for _, sym := range idx.Symbols {
		if _, err := symStmt.ExecContext(ctx, fileID, sym.Name, string(sym.Kind), sym.Line, sym.EndLine, sym.Signature, sym.Parent); err != nil {
			return 0, err
		}
	}

	importStmt, err := tx.PrepareContext(ctx, `INSERT INTO imports (file_id, source, line) VALUES (?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer importStmt.Close()
	for _, imp := range idx.Imports {
		if _, err := importStmt.ExecContext(ctx, fileID, imp.ToSource, imp.Line); err != nil {
			return 0, err
		}
	}

	callStmt, err := tx.PrepareContext(ctx, `INSERT INTO calls (file_id, from_symbol, to_name, line, confidence) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer callStmt.Close()
	for _, call := range idx.Calls {
		confidence := call.Confidence
		if confidence == "" {
			confidence = "HEURISTIC"
		}
		if _, err := callStmt.ExecContext(ctx, fileID, call.FromSymbol, call.ToName, call.Line, confidence); err != nil {
			return 0, err
		}
	}

	routeStmt, err := tx.PrepareContext(ctx, `INSERT INTO routes (file_id, method, path, handler, framework, line, confidence) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer routeStmt.Close()
	for _, route := range idx.Routes {
		confidence := route.Confidence
		if confidence == "" {
			confidence = "HEURISTIC"
		}
		if _, err := routeStmt.ExecContext(ctx, fileID, route.Method, route.Path, route.Handler, route.Framework, route.Line, confidence); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return fileID, nil
}

func (s *sqliteStore) GetFile(ctx context.Context, path string) (*api.FileInfo, error) {
	row := s.db.QueryRowContext(ctx, `SELECT path, language, content_hash, size FROM files WHERE path = ?`, path)
	var f api.FileInfo
	var lang string
	if err := row.Scan(&f.Path, &lang, &f.ContentHash, &f.Size); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	f.Language = api.Language(lang)
	return &f, nil
}

func (s *sqliteStore) DeleteFile(ctx context.Context, path string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM files WHERE path = ?`, path)
	return err
}

func (s *sqliteStore) ListFiles(ctx context.Context, lang *api.Language) ([]*api.FileInfo, error) {
	var rows *sql.Rows
	var err error
	if lang != nil {
		rows, err = s.db.QueryContext(ctx, `SELECT path, language, content_hash, size FROM files WHERE language = ?`, string(*lang))
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT path, language, content_hash, size FROM files`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*api.FileInfo
	for rows.Next() {
		var f api.FileInfo
		var l string
		if err := rows.Scan(&f.Path, &l, &f.ContentHash, &f.Size); err != nil {
			return nil, err
		}
		f.Language = api.Language(l)
		result = append(result, &f)
	}
	return result, rows.Err()
}

func (s *sqliteStore) ReplaceSymbols(ctx context.Context, fileID int64, symbols []api.Symbol) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `DELETE FROM symbols WHERE file_id = ?`, fileID)
	if err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO symbols (file_id, name, kind, line, end_line, signature, parent) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, sym := range symbols {
		_, err = stmt.ExecContext(ctx, fileID, sym.Name, string(sym.Kind), sym.Line, sym.EndLine, sym.Signature, sym.Parent)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) ReplaceImports(ctx context.Context, fileID int64, imports []api.ImportEdge) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `DELETE FROM imports WHERE file_id = ?`, fileID)
	if err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO imports (file_id, source, line) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, imp := range imports {
		_, err = stmt.ExecContext(ctx, fileID, imp.ToSource, imp.Line)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) ReplaceCalls(ctx context.Context, fileID int64, calls []api.CallEdge) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM calls WHERE file_id = ?`, fileID); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO calls (file_id, from_symbol, to_name, line, confidence) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, call := range calls {
		confidence := call.Confidence
		if confidence == "" {
			confidence = "HEURISTIC"
		}
		if _, err := stmt.ExecContext(ctx, fileID, call.FromSymbol, call.ToName, call.Line, confidence); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) ReplaceRoutes(ctx context.Context, fileID int64, routes []api.Route) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM routes WHERE file_id = ?`, fileID); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO routes (file_id, method, path, handler, framework, line, confidence) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, route := range routes {
		confidence := route.Confidence
		if confidence == "" {
			confidence = "HEURISTIC"
		}
		if _, err := stmt.ExecContext(ctx, fileID, route.Method, route.Path, route.Handler, route.Framework, route.Line, confidence); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) SearchSymbols(ctx context.Context, query string, kind *api.SymbolKind, limit int) ([]api.Symbol, error) {
	if limit <= 0 {
		limit = 50
	}
	q := strings.TrimSpace(query)
	var rows *sql.Rows
	var err error

	if kind != nil {
		rows, err = s.db.QueryContext(ctx,
			`SELECT s.name, s.kind, f.path, s.line, s.end_line, s.signature, s.parent
			 FROM symbols_fts fts JOIN symbols s ON s.id = fts.rowid
			 JOIN files f ON f.id = s.file_id
			 WHERE symbols_fts MATCH ? AND s.kind = ?
			 LIMIT ?`, q, string(*kind), limit)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT s.name, s.kind, f.path, s.line, s.end_line, s.signature, s.parent
			 FROM symbols_fts fts JOIN symbols s ON s.id = fts.rowid
			 JOIN files f ON f.id = s.file_id
			 WHERE symbols_fts MATCH ?
			 LIMIT ?`, q, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSymbols(rows)
}

func (s *sqliteStore) FindDefinitions(ctx context.Context, name string) ([]api.Symbol, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT s.name, s.kind, f.path, s.line, s.end_line, s.signature, s.parent
		 FROM symbols s JOIN files f ON f.id = s.file_id
		 WHERE s.name = ? AND s.kind IN ('function','method','class','type','interface')
		 ORDER BY s.kind`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSymbols(rows)
}

func (s *sqliteStore) FindReferences(ctx context.Context, name string) ([]api.Symbol, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT s.name, s.kind, f.path, s.line, s.end_line, s.signature, s.parent
		 FROM symbols s JOIN files f ON f.id = s.file_id
		 WHERE s.name = ?
		 ORDER BY s.kind`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSymbols(rows)
}

func (s *sqliteStore) GetFileSymbols(ctx context.Context, path string) ([]api.Symbol, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT s.name, s.kind, f.path, s.line, s.end_line, s.signature, s.parent
		 FROM symbols s JOIN files f ON f.id = s.file_id
		 WHERE f.path = ?
		 ORDER BY s.line`, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSymbols(rows)
}

func (s *sqliteStore) GetImports(ctx context.Context, filePath string) ([]api.ImportEdge, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.path, i.source, i.line
		 FROM imports i JOIN files f ON f.id = i.file_id
		 WHERE f.path = ?`, filePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []api.ImportEdge
	for rows.Next() {
		var e api.ImportEdge
		if err := rows.Scan(&e.FromFile, &e.ToSource, &e.Line); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

func (s *sqliteStore) GetImporters(ctx context.Context, importSource string) ([]api.ImportEdge, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.path, i.source, i.line
		 FROM imports i JOIN files f ON f.id = i.file_id
		 WHERE i.source LIKE ?`, "%"+importSource+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []api.ImportEdge
	for rows.Next() {
		var e api.ImportEdge
		if err := rows.Scan(&e.FromFile, &e.ToSource, &e.Line); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

func (s *sqliteStore) GetCallees(ctx context.Context, fromSymbol string) ([]api.CallEdge, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.path, c.from_symbol, c.to_name, c.line, c.confidence
		 FROM calls c JOIN files f ON f.id = c.file_id
		 WHERE c.from_symbol = ?
		 ORDER BY c.line`, fromSymbol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCalls(rows)
}

func (s *sqliteStore) GetCallers(ctx context.Context, toName string) ([]api.CallEdge, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.path, c.from_symbol, c.to_name, c.line, c.confidence
			 FROM calls c JOIN files f ON f.id = c.file_id
			 WHERE c.to_name = ? OR c.to_name LIKE ? OR c.to_name LIKE ?
			 ORDER BY f.path, c.line`, toName, "%."+toName, "%::"+toName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCalls(rows)
}

func (s *sqliteStore) ListRoutes(ctx context.Context, query string) ([]api.Route, error) {
	query = strings.TrimSpace(query)
	var rows *sql.Rows
	var err error
	if query == "" {
		rows, err = s.db.QueryContext(ctx, `SELECT f.path, r.method, r.path, r.handler, r.framework, r.line, r.confidence FROM routes r JOIN files f ON f.id = r.file_id ORDER BY r.path, r.method`)
	} else {
		like := "%" + query + "%"
		rows, err = s.db.QueryContext(ctx, `SELECT f.path, r.method, r.path, r.handler, r.framework, r.line, r.confidence FROM routes r JOIN files f ON f.id = r.file_id WHERE r.path LIKE ? OR r.handler LIKE ? OR r.framework LIKE ? ORDER BY r.path, r.method`, like, like, like)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRoutes(rows)
}

func (s *sqliteStore) Stats(ctx context.Context) (*api.IndexStats, error) {
	var st api.IndexStats
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files`).Scan(&st.TotalFiles)
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM symbols`).Scan(&st.TotalSymbols)
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM imports`).Scan(&st.TotalImports)
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM documents`).Scan(&st.TotalDocuments)
	s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(indexed_at), 0) FROM files`).Scan(&st.LastIndexedUnix)
	if st.LastIndexedUnix > 0 {
		st.LastIndexedAt = time.Unix(st.LastIndexedUnix, 0).UTC().Format(time.RFC3339)
	}
	st.IndexVersion = "graph-export.v2"
	return &st, nil
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}

func (s *sqliteStore) GetDocumentStats(ctx context.Context) (total, indexed int, err error) {
	var count int
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM documents`).Scan(&count)
	if err != nil {
		return 0, 0, err
	}
	return count, count, nil
}

func scanSymbols(rows *sql.Rows) ([]api.Symbol, error) {
	var result []api.Symbol
	for rows.Next() {
		var sym api.Symbol
		var kind string
		if err := rows.Scan(&sym.Name, &kind, &sym.FilePath, &sym.Line, &sym.EndLine, &sym.Signature, &sym.Parent); err != nil {
			return nil, err
		}
		sym.Kind = api.SymbolKind(kind)
		result = append(result, sym)
	}
	return result, rows.Err()
}

func scanCalls(rows *sql.Rows) ([]api.CallEdge, error) {
	var result []api.CallEdge
	for rows.Next() {
		var call api.CallEdge
		if err := rows.Scan(&call.FromFile, &call.FromSymbol, &call.ToName, &call.Line, &call.Confidence); err != nil {
			return nil, err
		}
		result = append(result, call)
	}
	return result, rows.Err()
}

func scanRoutes(rows *sql.Rows) ([]api.Route, error) {
	var result []api.Route
	for rows.Next() {
		var route api.Route
		if err := rows.Scan(&route.FilePath, &route.Method, &route.Path, &route.Handler, &route.Framework, &route.Line, &route.Confidence); err != nil {
			return nil, err
		}
		result = append(result, route)
	}
	return result, rows.Err()
}

func (s *sqliteStore) UpsertDocument(ctx context.Context, doc *api.Document) (int64, error) {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO documents (path, language, content_hash, title, summary, size) VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET content_hash=excluded.content_hash, title=excluded.title, summary=excluded.summary, size=excluded.size, indexed_at=unixepoch()`,
		doc.Path, doc.Language, doc.ContentHash, doc.Title, doc.Summary, doc.Size)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `SELECT id FROM documents WHERE path = ?`, doc.Path).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (s *sqliteStore) GetDocument(ctx context.Context, path string) (*api.Document, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, path, language, content_hash, title, summary, size FROM documents WHERE path = ?`, path)
	var doc api.Document
	if err := row.Scan(&doc.ID, &doc.Path, &doc.Language, &doc.ContentHash, &doc.Title, &doc.Summary, &doc.Size); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (s *sqliteStore) DeleteDocument(ctx context.Context, path string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM documents WHERE path = ?`, path)
	return err
}

func (s *sqliteStore) ListDocuments(ctx context.Context) ([]*api.Document, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, path, language, content_hash, title, summary, size FROM documents`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*api.Document
	for rows.Next() {
		var doc api.Document
		if err := rows.Scan(&doc.ID, &doc.Path, &doc.Language, &doc.ContentHash, &doc.Title, &doc.Summary, &doc.Size); err != nil {
			return nil, err
		}
		result = append(result, &doc)
	}
	return result, rows.Err()
}

func (s *sqliteStore) ReplaceDocumentLinks(ctx context.Context, docID int64, links []api.DocumentLink) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `DELETE FROM document_links WHERE document_id = ?`, docID)
	if err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO document_links (document_id, target_type, target_value, line, section_title, section_slug, section_line, evidence, confidence) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, link := range links {
		_, err = stmt.ExecContext(ctx, docID, link.TargetType, link.TargetValue, link.Line, link.SectionTitle, link.SectionSlug, link.SectionLine, link.Evidence, link.Confidence)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) GetDocumentLinks(ctx context.Context, docPath string) ([]api.DocumentLink, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT dl.id, dl.document_id, dl.target_type, dl.target_value, dl.line, dl.section_title, dl.section_slug, dl.section_line, dl.evidence, dl.confidence
		 FROM document_links dl JOIN documents d ON d.id = dl.document_id
		 WHERE d.path = ?`, docPath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDocumentLinks(rows)
}

func (s *sqliteStore) GetDocumentsByTarget(ctx context.Context, targetType, targetValue string) ([]api.DocumentLink, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT dl.id, dl.document_id, dl.target_type, dl.target_value, dl.line, dl.section_title, dl.section_slug, dl.section_line, dl.evidence, dl.confidence
		 FROM document_links dl
		 WHERE dl.target_type = ? AND dl.target_value = ?`, targetType, targetValue)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDocumentLinks(rows)
}

func scanDocumentLinks(rows *sql.Rows) ([]api.DocumentLink, error) {
	var result []api.DocumentLink
	for rows.Next() {
		var link api.DocumentLink
		if err := rows.Scan(&link.ID, &link.DocumentID, &link.TargetType, &link.TargetValue, &link.Line, &link.SectionTitle, &link.SectionSlug, &link.SectionLine, &link.Evidence, &link.Confidence); err != nil {
			return nil, err
		}
		result = append(result, link)
	}
	return result, rows.Err()
}
