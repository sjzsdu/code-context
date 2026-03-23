package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/sjzsdu/code-memory/internal/api"
)

//go:embed schema.sql
var schemaSQL string

type sqliteStore struct {
	db *sql.DB
}

func NewSQLiteStore(dbPath string) (Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	return &sqliteStore{db: db}, nil
}

func (s *sqliteStore) Init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schemaSQL)
	return err
}

func (s *sqliteStore) UpsertFile(ctx context.Context, f *api.FileInfo) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO files (path, language, content_hash, size) VALUES (?, ?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET content_hash=excluded.content_hash, size=excluded.size, indexed_at=unixepoch()`,
		f.Path, string(f.Language), f.ContentHash, f.Size)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
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

func (s *sqliteStore) Stats(ctx context.Context) (*api.IndexStats, error) {
	var st api.IndexStats
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files`).Scan(&st.TotalFiles)
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM symbols`).Scan(&st.TotalSymbols)
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM imports`).Scan(&st.TotalImports)
	return &st, nil
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
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
