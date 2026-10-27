package character

// store.go - saving and loading characters.
//
// CharacterStore is the interface the API will depend on. FileStore keeps one JSON file per
// character (<dir>/<id>.json), which is easy to read, diff and fix by hand. MemoryStore is for
// tests. Swapping in SQLite later means writing another implementation of the same interface.
//
// Characters are user data, not authored content, so reading is lenient: unknown fields in a
// file are ignored, which keeps older files loadable as the Character struct evolves.
// Saving does no rules validation (the engine is permissive); it only checks the id is safe to
// use as a filename.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound  = errors.New("character not found")
	ErrInvalidID = errors.New("invalid character id")
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
	Error       string    `json:"error,omitempty"` // set when the stored data couldn't be read
}

// CharacterStore persists characters. Implementations must be safe for concurrent use.
type CharacterStore interface {
	Get(id string) (Character, error)
	List() ([]CharacterSummary, error)
	Save(c Character) error // creates or overwrites; c.ID must be set
	Delete(id string) error
}

var (
	_ CharacterStore = (*FileStore)(nil)
	_ CharacterStore = (*MemoryStore)(nil)
)

func summarize(c Character, updated time.Time) CharacterSummary {
	s := CharacterSummary{ID: c.ID, Name: c.Name, PortraitURL: c.PortraitURL, UpdatedAt: updated, ClassIDs: []string{}}
	for _, cl := range c.Classes {
		s.Level += cl.Level
		s.ClassIDs = append(s.ClassIDs, cl.ClassID)
	}
	return s
}

func sortSummaries(list []CharacterSummary) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := strings.ToLower(list[i].Name), strings.ToLower(list[j].Name)
		if a != b {
			return a < b
		}
		return list[i].ID < list[j].ID
	})
}

func decodeCharacter(data []byte) (Character, error) {
	var c Character
	if err := json.Unmarshal(data, &c); err != nil { // lenient: unknown fields are ignored
		return Character{}, err
	}
	return c, nil
}

// ---------------------------------------------------------------------------
// FileStore
// ---------------------------------------------------------------------------

// FileStore keeps one JSON file per character. It is safe for concurrent use within one process;
// it does not coordinate between processes.
type FileStore struct {
	dir string
	mu  sync.RWMutex
}

// NewFileStore uses dir, creating it if needed.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create character directory: %w", err)
	}
	return &FileStore{dir: dir}, nil
}

func (s *FileStore) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func (s *FileStore) Get(id string) (Character, error) {
	if err := validID(id); err != nil {
		return Character{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return Character{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return Character{}, fmt.Errorf("read character %s: %w", id, err)
	}
	c, err := decodeCharacter(data)
	if err != nil {
		return Character{}, fmt.Errorf("character %s is not valid JSON: %w", id, err)
	}
	switch {
	case c.ID == "":
		c.ID = id
	case c.ID != id:
		return Character{}, fmt.Errorf("character file %s.json contains id %q", id, c.ID)
	}
	return c, nil
}

// Save writes atomically: the data goes to a temporary file in the same folder, which is then
// renamed over the real one, so a crash can't leave a half-written character.
func (s *FileStore) Save(c Character) error {
	if err := validCharacter(c); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode character: %w", err)
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	tmp, err := os.CreateTemp(s.dir, ".tmp-"+c.ID+"-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	fail := func(step string, err error) error {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("%s: %w", step, err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail("write character", err)
	}
	if err := tmp.Sync(); err != nil {
		return fail("sync character", err)
	}
	_ = tmp.Chmod(0o644) // CreateTemp makes files private; best effort, not all platforms support it
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close character: %w", err)
	}
	if err := os.Rename(tmpName, s.path(c.ID)); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("replace character: %w", err)
	}
	return nil
}

func (s *FileStore) Delete(id string) error {
	if err := validID(id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	err := os.Remove(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return err
}

// List returns every character, sorted by name. A file that can't be read doesn't fail the
// listing: it appears with Error set so a frontend can show that something needs fixing.
func (s *FileStore) List() ([]CharacterSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("read character directory: %w", err)
	}

	out := []CharacterSummary{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")

		var updated time.Time
		if info, err := e.Info(); err == nil {
			updated = info.ModTime()
		}
		broken := func(msg string) CharacterSummary {
			return CharacterSummary{ID: id, UpdatedAt: updated, ClassIDs: []string{}, Error: msg}
		}

		if err := validID(id); err != nil {
			out = append(out, broken(err.Error()))
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, name))
		if err != nil {
			out = append(out, broken("cannot read file: "+err.Error()))
			continue
		}
		c, err := decodeCharacter(data)
		if err != nil {
			out = append(out, broken("not valid JSON: "+err.Error()))
			continue
		}
		if c.ID != "" && c.ID != id {
			out = append(out, broken(fmt.Sprintf("file contains id %q", c.ID)))
			continue
		}
		c.ID = id
		out = append(out, summarize(c, updated))
	}
	sortSummaries(out)
	return out, nil
}

// ---------------------------------------------------------------------------
// MemoryStore
// ---------------------------------------------------------------------------

// MemoryStore keeps characters in memory, as JSON so callers never share memory with the store.
// Useful for tests, including API tests.
type MemoryStore struct {
	mu      sync.RWMutex
	data    map[string][]byte
	updated map[string]time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: map[string][]byte{}, updated: map[string]time.Time{}}
}

func (s *MemoryStore) Get(id string) (Character, error) {
	if err := validID(id); err != nil {
		return Character{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.data[id]
	if !ok {
		return Character{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return decodeCharacter(data)
}

func (s *MemoryStore) Save(c Character) error {
	if err := validCharacter(c); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode character: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[c.ID] = data
	s.updated[c.ID] = time.Now()
	return nil
}

func (s *MemoryStore) Delete(id string) error {
	if err := validID(id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[id]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	delete(s.data, id)
	delete(s.updated, id)
	return nil
}

func (s *MemoryStore) List() ([]CharacterSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []CharacterSummary{}
	for id, data := range s.data {
		c, err := decodeCharacter(data)
		if err != nil {
			out = append(out, CharacterSummary{ID: id, ClassIDs: []string{}, Error: "not valid JSON: " + err.Error()})
			continue
		}
		out = append(out, summarize(c, s.updated[id]))
	}
	sortSummaries(out)
	return out, nil
}
