-- Users are added by cmd/admin; there is no sign-up.
CREATE TABLE users (
    id               TEXT PRIMARY KEY,
    discord_id       TEXT NOT NULL UNIQUE,
    discord_username TEXT NOT NULL UNIQUE COLLATE NOCASE,
    created_at       INTEGER NOT NULL
);

-- token_hash is the sha256 of the bearer token; the token itself is never stored.
CREATE TABLE sessions (
    token_hash BLOB PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX sessions_user_id ON sessions(user_id);

-- One active code per user: requesting a new code replaces the old one.
CREATE TABLE otp_codes (
    user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    code_hash  BLOB NOT NULL,
    expires_at INTEGER NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);

-- Ids are per owner, so one user's choice of id can't collide with (or reveal) another's.
-- The primary key also serves as the index on owner_id.
CREATE TABLE characters (
    owner_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    id         TEXT NOT NULL,
    name       TEXT NOT NULL,
    data       TEXT NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (owner_id, id)
);
