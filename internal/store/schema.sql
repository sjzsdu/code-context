PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version    TEXT PRIMARY KEY,
    applied_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS files (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    path         TEXT UNIQUE NOT NULL,
    language     TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    size         INTEGER NOT NULL DEFAULT 0,
    indexed_at   INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE IF NOT EXISTS symbols (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id   INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    name      TEXT NOT NULL,
    kind      TEXT NOT NULL,
    line      INTEGER NOT NULL,
    end_line  INTEGER NOT NULL DEFAULT 0,
    signature TEXT NOT NULL DEFAULT '',
    parent    TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS imports (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    source  TEXT NOT NULL,
    line    INTEGER NOT NULL DEFAULT 0
);

CREATE VIRTUAL TABLE IF NOT EXISTS symbols_fts USING fts5(
    name, signature,
    content=symbols, content_rowid=id,
    tokenize='unicode61'
);

CREATE TRIGGER IF NOT EXISTS symbols_ai AFTER INSERT ON symbols BEGIN
    INSERT INTO symbols_fts(rowid, name, signature) VALUES (new.id, new.name, new.signature);
END;
CREATE TRIGGER IF NOT EXISTS symbols_ad AFTER DELETE ON symbols BEGIN
    INSERT INTO symbols_fts(symbols_fts, rowid, name, signature) VALUES ('delete', old.id, old.name, old.signature);
END;
CREATE TRIGGER IF NOT EXISTS symbols_au AFTER UPDATE ON symbols BEGIN
    INSERT INTO symbols_fts(symbols_fts, rowid, name, signature) VALUES ('delete', old.id, old.name, old.signature);
    INSERT INTO symbols_fts(rowid, name, signature) VALUES (new.id, new.name, new.signature);
END;

CREATE INDEX IF NOT EXISTS idx_symbols_name ON symbols(name);
CREATE INDEX IF NOT EXISTS idx_symbols_kind ON symbols(kind);
CREATE INDEX IF NOT EXISTS idx_symbols_file ON symbols(file_id);
CREATE INDEX IF NOT EXISTS idx_imports_source ON imports(source);
CREATE INDEX IF NOT EXISTS idx_imports_file ON imports(file_id);

CREATE TABLE IF NOT EXISTS calls (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id     INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    from_symbol TEXT NOT NULL,
    to_name     TEXT NOT NULL,
    line        INTEGER NOT NULL DEFAULT 0,
    confidence  TEXT NOT NULL DEFAULT 'HEURISTIC'
);

CREATE INDEX IF NOT EXISTS idx_calls_from ON calls(from_symbol);
CREATE INDEX IF NOT EXISTS idx_calls_to ON calls(to_name);
CREATE INDEX IF NOT EXISTS idx_calls_file ON calls(file_id);

CREATE TABLE IF NOT EXISTS routes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id     INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    method      TEXT NOT NULL DEFAULT '',
    path        TEXT NOT NULL,
    handler     TEXT NOT NULL DEFAULT '',
    framework   TEXT NOT NULL DEFAULT '',
    line        INTEGER NOT NULL DEFAULT 0,
    confidence  TEXT NOT NULL DEFAULT 'HEURISTIC'
);

CREATE INDEX IF NOT EXISTS idx_routes_path ON routes(path);
CREATE INDEX IF NOT EXISTS idx_routes_handler ON routes(handler);
CREATE INDEX IF NOT EXISTS idx_routes_file ON routes(file_id);

-- Documents table for .md/.txt files
CREATE TABLE IF NOT EXISTS documents (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    path         TEXT UNIQUE NOT NULL,
    language     TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    title       TEXT,
    summary     TEXT,
    size        INTEGER NOT NULL DEFAULT 0,
    indexed_at  INTEGER NOT NULL DEFAULT (unixepoch())
);

-- Document links table for document-to-code relationships
CREATE TABLE IF NOT EXISTS document_links (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    document_id   INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    target_type   TEXT NOT NULL,
    target_value  TEXT NOT NULL,
    line         INTEGER NOT NULL DEFAULT 0,
    section_title TEXT NOT NULL DEFAULT '',
    section_slug  TEXT NOT NULL DEFAULT '',
    section_line  INTEGER NOT NULL DEFAULT 0,
    evidence     TEXT,
    confidence   REAL NOT NULL DEFAULT 1.0
);

CREATE INDEX IF NOT EXISTS idx_document_links_doc ON document_links(document_id);
CREATE INDEX IF NOT EXISTS idx_document_links_target ON document_links(target_type, target_value);

-- Embedding cache stores provider-generated vectors by model + dimensions + chunk hash.
-- It is intentionally separate from vector-search indexes so multiple backends can reuse
-- the same cached embedding results.
CREATE TABLE IF NOT EXISTS embedding_cache (
    key          TEXT PRIMARY KEY,
    model        TEXT NOT NULL,
    dimensions   INTEGER NOT NULL,
    content_hash TEXT NOT NULL,
    input_kind   TEXT NOT NULL DEFAULT '',
    target_kind  TEXT NOT NULL DEFAULT '',
    target_path  TEXT NOT NULL DEFAULT '',
    target_name  TEXT NOT NULL DEFAULT '',
    target_json  TEXT NOT NULL DEFAULT '{}',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    vector_json  TEXT NOT NULL,
    created_at   INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at   INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE INDEX IF NOT EXISTS idx_embedding_cache_model_hash ON embedding_cache(model, dimensions, content_hash);
CREATE INDEX IF NOT EXISTS idx_embedding_cache_target ON embedding_cache(target_kind, target_path, target_name);
