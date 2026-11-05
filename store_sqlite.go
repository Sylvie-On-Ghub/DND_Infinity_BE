package character

// store_sqlite.go - the SQLite implementation of CharacterStore.
//
// Every query filters on owner_id, so another user's character is indistinguishable from a
// missing one. The data column holds the whole Character as JSON, without its version: the
// version column is the only source of truth for that.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type SQLiteStore struct {
	db *sql.DB
}

var _ CharacterStore = (*SQLiteStore)(nil)

func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{db: db}
}

func encodeCharacter(c Character) (string, error) {
	c.Version = 0
	data, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("encode character: %w", err)
	}
	return string(data), nil
}

// List reads the summary fields with SQLite's JSON functions rather than decoding each character.
// The type filter matters: a character with no classes stores "classes":null, and json_each
// returns one row for a null.
func (s *SQLiteStore) List(ctx context.Context, ownerID string) ([]CharacterSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name,
		       COALESCE(json_extract(data, '$.portrait_url'), ''),
		       (SELECT COALESCE(SUM(json_extract(value, '$.level')), 0)
		          FROM json_each(data, '$.classes') WHERE type = 'object'),
		       (SELECT json_group_array(json_extract(value, '$.class_id'))
		          FROM json_each(data, '$.classes') WHERE type = 'object'),
		       updated_at
		  FROM characters
		 WHERE owner_id = ?
		 ORDER BY name COLLATE NOCASE, id`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list characters: %w", err)
	}
	defer rows.Close()

	out := []CharacterSummary{}
	for rows.Next() {
		var sum CharacterSummary
		var classIDs string
		var updated int64
		if err := rows.Scan(&sum.ID, &sum.Name, &sum.PortraitURL, &sum.Level, &classIDs, &updated); err != nil {
			return nil, fmt.Errorf("list characters: %w", err)
		}
		if err := json.Unmarshal([]byte(classIDs), &sum.ClassIDs); err != nil {
			return nil, fmt.Errorf("list characters: %s: %w", sum.ID, err)
		}
		sum.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, sum)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list characters: %w", err)
	}
	return out, nil
}

func (s *SQLiteStore) Get(ctx context.Context, ownerID, id string) (Character, error) {
	if err := validID(id); err != nil {
		return Character{}, err
	}
	var data string
	var version int
	err := s.db.QueryRowContext(ctx,
		`SELECT data, version FROM characters WHERE owner_id = ? AND id = ?`, ownerID, id).Scan(&data, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return Character{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return Character{}, fmt.Errorf("read character %s: %w", id, err)
	}
	c, err := decodeCharacter([]byte(data))
	if err != nil {
		return Character{}, fmt.Errorf("character %s is not valid JSON: %w", id, err)
	}
	c.ID = id
	c.Version = version
	return c, nil
}

func (s *SQLiteStore) Create(ctx context.Context, ownerID string, c Character) (Character, error) {
	inserted, err := s.insert(ctx, ownerID, c, time.Now())
	if err != nil {
		return Character{}, err
	}
	if !inserted {
		return Character{}, fmt.Errorf("%w: %s", ErrAlreadyExists, c.ID)
	}
	c.Version = 1
	return c, nil
}

// insert adds the character at version 1, and reports false if the owner already has that id.
func (s *SQLiteStore) insert(ctx context.Context, ownerID string, c Character, updated time.Time) (bool, error) {
	if err := validCharacter(c); err != nil {
		return false, err
	}
	data, err := encodeCharacter(c)
	if err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO characters (owner_id, id, name, data, version, updated_at)
		VALUES (?, ?, ?, ?, 1, ?)
		ON CONFLICT (owner_id, id) DO NOTHING`, ownerID, c.ID, c.Name, data, updated.Unix())
	if err != nil {
		return false, fmt.Errorf("create character %s: %w", c.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("create character %s: %w", c.ID, err)
	}
	return n == 1, nil
}

func (s *SQLiteStore) Update(ctx context.Context, ownerID string, c Character) (Character, error) {
	if err := validCharacter(c); err != nil {
		return Character{}, err
	}
	data, err := encodeCharacter(c)
	if err != nil {
		return Character{}, err
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE characters
		   SET name = ?, data = ?, version = version + 1, updated_at = ?
		 WHERE id = ? AND owner_id = ? AND version = ?`,
		c.Name, data, time.Now().Unix(), c.ID, ownerID, c.Version)
	if err != nil {
		return Character{}, fmt.Errorf("update character %s: %w", c.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Character{}, fmt.Errorf("update character %s: %w", c.ID, err)
	}
	if n == 1 {
		c.Version++
		return c, nil
	}

	// Nothing changed: either the version was stale or this owner has no such character.
	var exists int
	err = s.db.QueryRowContext(ctx, `SELECT 1 FROM characters WHERE owner_id = ? AND id = ?`, ownerID, c.ID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return Character{}, fmt.Errorf("%w: %s", ErrNotFound, c.ID)
	}
	if err != nil {
		return Character{}, fmt.Errorf("update character %s: %w", c.ID, err)
	}
	return Character{}, fmt.Errorf("%w: %s", ErrVersionConflict, c.ID)
}

func (s *SQLiteStore) Delete(ctx context.Context, ownerID, id string) error {
	if err := validID(id); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM characters WHERE owner_id = ? AND id = ?`, ownerID, id)
	if err != nil {
		return fmt.Errorf("delete character %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete character %s: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return nil
}
