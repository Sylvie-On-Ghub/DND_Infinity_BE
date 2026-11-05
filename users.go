package character

// users.go - the accounts allowed to sign in. They are managed by cmd/admin, and the bot's admins can
// add, remove and rename them with slash commands; there is no HTTP route for changing them.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrUserNotFound = errors.New("user not found")
	ErrUserExists   = errors.New("that Discord id or username is already in use")
)

// User is an approved account. Discord ids aren't sent to clients.
type User struct {
	ID              string    `json:"id"`
	DiscordID       string    `json:"-"`
	DiscordUsername string    `json:"discord_username"`
	CreatedAt       time.Time `json:"created_at"`
}

const userColumns = `id, discord_id, discord_username, created_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (User, error) {
	var u User
	var created int64
	err := row.Scan(&u.ID, &u.DiscordID, &u.DiscordUsername, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("read user: %w", err)
	}
	u.CreatedAt = time.Unix(created, 0).UTC()
	return u, nil
}

func userByID(ctx context.Context, db *sql.DB, id string) (User, error) {
	return scanUser(db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
}

func userByDiscordID(ctx context.Context, db *sql.DB, discordID string) (User, error) {
	return scanUser(db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE discord_id = ?`, discordID))
}

// UserByUsername finds a user by Discord username, ignoring case.
func UserByUsername(ctx context.Context, db *sql.DB, username string) (User, error) {
	return scanUser(db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE discord_username = ?`, username))
}

// ListUsers returns everyone, sorted by username.
func ListUsers(ctx context.Context, db *sql.DB) ([]User, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY discord_username`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return out, nil
}

// AddUser approves a Discord account. Discord ids are numeric, which catches the two arguments
// being swapped.
func AddUser(ctx context.Context, db *sql.DB, discordID, username string) (User, error) {
	discordID = strings.TrimSpace(discordID)
	if discordID == "" || strings.Trim(discordID, "0123456789") != "" {
		return User{}, fmt.Errorf("Discord id %q should be all digits", discordID)
	}
	username, err := cleanUsername(username)
	if err != nil {
		return User{}, err
	}

	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return User{}, fmt.Errorf("generate user id: %w", err)
	}
	u := User{ID: hex.EncodeToString(b[:]), DiscordID: discordID, DiscordUsername: username, CreatedAt: time.Now().UTC().Truncate(time.Second)}
	res, err := db.ExecContext(ctx, `
		INSERT INTO users (id, discord_id, discord_username, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT DO NOTHING`, u.ID, u.DiscordID, u.DiscordUsername, u.CreatedAt.Unix())
	if err != nil {
		return User{}, fmt.Errorf("add user: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return User{}, fmt.Errorf("add user: %w", err)
	} else if n == 0 {
		return User{}, ErrUserExists
	}
	return u, nil
}

// RenameUser records a new Discord username, for when someone changes theirs. Sessions and
// characters are unaffected.
func RenameUser(ctx context.Context, db *sql.DB, oldName, newName string) error {
	newName, err := cleanUsername(newName)
	if err != nil {
		return err
	}
	u, err := UserByUsername(ctx, db, oldName)
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `UPDATE OR IGNORE users SET discord_username = ? WHERE id = ?`, newName, u.ID)
	if err != nil {
		return fmt.Errorf("rename user: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("rename user: %w", err)
	} else if n == 0 {
		return ErrUserExists
	}
	return nil
}

// RemoveUser deletes the account along with its sessions, codes and characters.
func RemoveUser(ctx context.Context, db *sql.DB, id string) error {
	res, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("remove user: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("remove user: %w", err)
	} else if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// CountCharacters says how many characters a user owns.
func CountCharacters(ctx context.Context, db *sql.DB, userID string) (int, error) {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM characters WHERE owner_id = ?`, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count characters: %w", err)
	}
	return n, nil
}
