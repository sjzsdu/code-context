PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA foreign_keys=ON;

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
