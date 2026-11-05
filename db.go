package character

// db.go - opening the SQLite database and bringing its schema up to date.
//
// Migrations are the numbered files in migrations/ (0001_init.sql, 0002_...), embedded in the
// binary. PRAGMA user_version records how many have run; each pending one runs in its own
// transaction on startup.

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// OpenDB opens the database at path, creating it if needed, and applies pending migrations.
func OpenDB(ctx context.Context, path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	// SQLite would create the file world-readable. Its -wal and -shm files copy these permissions.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create database file: %w", err)
	}
	f.Close()

	// _txlock=immediate takes the write lock at BEGIN, so busy_timeout applies. A deferred
	// transaction that upgrades from read to write can fail with SQLITE_BUSY straight away.
	dsn := "file:" + (&url.URL{Path: path}).EscapedPath() +
		"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

type migration struct {
	version int
	name    string
}

// listMigrations returns the embedded migrations in order, checking they are numbered 1, 2, 3...
func listMigrations() ([]migration, error) {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var out []migration
	for _, e := range entries {
		num, _, _ := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(num)
		if err != nil {
			return nil, fmt.Errorf("migration %s: the name must start with a number", e.Name())
		}
		out = append(out, migration{version: v, name: e.Name()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i, m := range out {
		if m.version != i+1 {
			return nil, fmt.Errorf("migration %s: expected number %d", m.name, i+1)
		}
	}
	return out, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	list, err := listMigrations()
	if err != nil {
		return err
	}
	for _, m := range list {
		if err := runMigration(ctx, db, m, len(list)); err != nil {
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
	}
	return nil
}

func runMigration(ctx context.Context, db *sql.DB, m migration, latest int) error {
	body, err := migrationFiles.ReadFile("migrations/" + m.name)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Read inside the transaction, so two processes starting together can't both run a migration.
	var current int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	if current > latest {
		return fmt.Errorf("the database is at version %d but this program only knows %d: is it out of date?", current, latest)
	}
	if current >= m.version {
		return nil
	}
	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
		return err
	}
	return tx.Commit()
}
