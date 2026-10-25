CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    is_admin      INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
    token      TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at INTEGER NOT NULL
);

CREATE TABLE libraries (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('movies', 'tv', 'music')),
    created_at INTEGER NOT NULL
);

CREATE TABLE library_paths (
    library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    path       TEXT NOT NULL,
    PRIMARY KEY (library_id, path)
);

-- Every browsable thing is an item. Files are leaves (movie, episode, track);
-- containers (show, season, artist, album) have no path.
CREATE TABLE items (
    id             INTEGER PRIMARY KEY,
    library_id     INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    parent_id      INTEGER REFERENCES items(id) ON DELETE CASCADE,
    kind           TEXT NOT NULL CHECK (kind IN ('movie', 'show', 'season', 'episode', 'artist', 'album', 'track')),
    title          TEXT NOT NULL,
    sort_title     TEXT NOT NULL,
    year           INTEGER,
    index_number   INTEGER, -- season, episode or track number
    disc_number    INTEGER,
    path           TEXT UNIQUE,
    size           INTEGER,
    mtime          INTEGER,
    duration_ms    INTEGER,
    container      TEXT,
    video_codec    TEXT,
    audio_codec    TEXT,
    width          INTEGER,
    height         INTEGER,
    added_at       INTEGER NOT NULL
);
CREATE INDEX items_parent ON items(parent_id, index_number, sort_title);
CREATE INDEX items_library ON items(library_id, kind, sort_title);
CREATE INDEX items_added ON items(added_at);
-- Containers are unique by name under their parent, so rescans reuse them.
CREATE UNIQUE INDEX items_container_key ON items(library_id, kind, IFNULL(parent_id, 0), title, IFNULL(index_number, -1)) WHERE path IS NULL;

CREATE VIRTUAL TABLE items_fts USING fts5(title, content='items', content_rowid='id', tokenize='unicode61 remove_diacritics 2');
CREATE TRIGGER items_ai AFTER INSERT ON items BEGIN
    INSERT INTO items_fts(rowid, title) VALUES (new.id, new.title);
END;
CREATE TRIGGER items_ad AFTER DELETE ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title) VALUES ('delete', old.id, old.title);
END;
CREATE TRIGGER items_au AFTER UPDATE OF title ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title) VALUES ('delete', old.id, old.title);
    INSERT INTO items_fts(rowid, title) VALUES (new.id, new.title);
END;

CREATE TABLE progress (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id     INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    position_ms INTEGER NOT NULL DEFAULT 0,
    played      INTEGER NOT NULL DEFAULT 0,
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (user_id, item_id)
);
CREATE INDEX progress_recent ON progress(user_id, updated_at);
