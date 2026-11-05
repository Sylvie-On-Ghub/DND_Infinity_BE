package character

// store.go - saving and loading characters.
//
// CharacterStore is the interface the API depends on; SQLiteStore implements it. Every method is
// scoped to one owner, and a character belonging to someone else behaves exactly like a missing one.
//
// Characters are user data, not authored content, so reading is lenient: unknown fields are
// ignored, which keeps older characters loadable as the Character struct evolves.
// Saving does no rules validation (the engine is permissive); it only checks the id and portrait.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	ErrNotFound        = errors.New("character not found")
	ErrInvalidID       = errors.New("invalid character id")
	ErrAlreadyExists   = errors.New("character already exists")
	ErrVersionConflict = errors.New("character was changed by another request")
)

var idRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func validID(id string) error {
	if !idRe.MatchString(id) {
		return fmt.Errorf("%w: %q (use letters, digits, - and _, up to 64 characters)", ErrInvalidID, id)
	}
	return nil
}

// validCharacter checks what the store refuses to save: a bad id, or a portrait that isn't an http(s) link.
func validCharacter(c Character) error {
	if err := validID(c.ID); err != nil {
		return err
	}
	if c.PortraitURL != "" {
		u, err := url.Parse(c.PortraitURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return badReq("portrait_url %q must be an http or https link", c.PortraitURL)
		}
	}
	return nil
}

// NewCharacterID returns a random 16-character hex id that is always valid.
func NewCharacterID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("character: no randomness available: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// CharacterSummary is what listing returns: enough to show a character picker.
type CharacterSummary struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	PortraitURL string    `json:"portrait_url,omitempty"`
	Level       int       `json:"level"`
	ClassIDs    []string  `json:"class_ids"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CharacterStore persists characters. Implementations must be safe for concurrent use.
type CharacterStore interface {
	List(ctx context.Context, ownerID string) ([]CharacterSummary, error)
	Get(ctx context.Context, ownerID, id string) (Character, error)
	// Create stores a new character at version 1 and returns it. c.ID must be set.
	Create(ctx context.Context, ownerID string, c Character) (Character, error)
	// Update replaces the character if c.Version is still current, and returns it with its new
	// version. A stale version is ErrVersionConflict; a character this owner doesn't have is ErrNotFound.
	Update(ctx context.Context, ownerID string, c Character) (Character, error)
	Delete(ctx context.Context, ownerID, id string) error
}

func decodeCharacter(data []byte) (Character, error) {
	var c Character
	if err := json.Unmarshal(data, &c); err != nil { // lenient: unknown fields are ignored
		return Character{}, err
	}
	upgradeVariantItems(&c)
	return c, nil
}

// ReadCharacterFile reads a character from the JSON files older versions saved (<id>.json). A file
// with no id takes it from the filename, and one whose id disagrees with its filename is an error.
func ReadCharacterFile(path string) (Character, error) {
	id := strings.TrimSuffix(filepath.Base(path), ".json")
	if err := validID(id); err != nil {
		return Character{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Character{}, err
	}
	c, err := decodeCharacter(data)
	if err != nil {
		return Character{}, fmt.Errorf("not valid JSON: %w", err)
	}
	switch {
	case c.ID == "":
		c.ID = id
	case c.ID != id:
		return Character{}, fmt.Errorf("the file contains id %q", c.ID)
	}
	return c, nil
}
