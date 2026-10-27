package character

// api.go - the HTTP API, standard library only.
//
// Conventions
//   - JSON in and out, under /api.
//   - Errors: {"error": {"code": "...", "message": "..."}} with codes bad_request, invalid_id,
//     not_found, already_exists, bad_conversion, method_not_allowed, not_supported, internal.
//   - Request bodies are strict: unknown fields are a 400, so a typo can't silently drop data.
//   - Rules problems are never errors; they come back as warnings on the sheet.
//   - Action routes return {"sheet": ...} plus any extra fields (an entry id, a removed flag).
//   - Read-modify-write actions on one character are serialised by a per-character lock.
//
// Routing is done by hand so it doesn't depend on a recent Go version's pattern mux.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Store      CharacterStore
	Library    *ContentLibrary
	Reload     func() (*ContentLibrary, error) // optional; enables POST /api/content/reload
	Ruleset    Ruleset
	CORSOrigin string // "" sends no CORS headers; "*" allows any origin
}

type Server struct {
	store  CharacterStore
	rules  Ruleset
	cors   string
	reload func() (*ContentLibrary, error)

	mu  sync.RWMutex
	lib *ContentLibrary

	locks *lockMap

	rngMu sync.Mutex
	rng   *rand.Rand
}

func NewServer(cfg Config) *Server {
	lib := cfg.Library
	if lib == nil {
		lib = newContentLibrary()
	}
	return &Server{
		store:  cfg.Store,
		rules:  cfg.Ruleset,
		cors:   cfg.CORSOrigin,
		reload: cfg.Reload,
		lib:    lib,
		locks:  &lockMap{m: map[string]*sync.Mutex{}},
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

var _ http.Handler = (*Server)(nil)

type lockMap struct {
	mu sync.Mutex
	m  map[string]*sync.Mutex
}

// lock takes the lock for one character id and returns the function that releases it.
func (l *lockMap) lock(id string) func() {
	l.mu.Lock()
	m, ok := l.m[id]
	if !ok {
		m = &sync.Mutex{}
		l.m[id] = m
	}
	l.mu.Unlock()
	m.Lock()
	return m.Unlock
}

func (s *Server) library() *ContentLibrary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lib
}

func (s *Server) roll(sides int) int {
	s.rngMu.Lock()
	defer s.rngMu.Unlock()
	return s.rng.Intn(sides) + 1
}

func (s *Server) sheetFor(c Character) (Sheet, error) {
	return Compute(c, s.library(), s.rules)
}

// effectiveRuleset fills in the defaults so a frontend sees what is actually in force.
func effectiveRuleset(r Ruleset) Ruleset {
	r.Currencies = CurrencyDefs(r)
	if r.SlotTable == nil {
		r.SlotTable = defaultSlotTable
	}
	if r.PactTable == nil {
		r.PactTable = defaultPactTable
	}
	return r
}

// ---------------------------------------------------------------------------
// Responses and errors
// ---------------------------------------------------------------------------

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		status = http.StatusInternalServerError
		data = []byte(`{"error":{"code":"internal","message":"could not encode the response"}}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(data)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: msg}})
}

func methodNotAllowed(w http.ResponseWriter, allowed ...string) {
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use "+strings.Join(allowed, " or ")+" here")
}

// fail maps an engine, store or request error to a response.
func (s *Server) fail(w http.ResponseWriter, err error) {
	var re *reqError
	switch {
	case errors.As(err, &re):
		status := http.StatusBadRequest
		switch re.Code {
		case "not_found":
			status = http.StatusNotFound
		case "already_exists":
			status = http.StatusConflict
		}
		writeError(w, status, re.Code, re.Msg)
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, ErrInvalidID):
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
	case errors.Is(err, ErrBadPatch):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}

// readJSON decodes a request body strictly into v.
func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return badReq("a JSON body is required")
		}
		return badReq("invalid JSON body: %v", err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return badReq("unexpected content after the JSON body")
	}
	return nil
}

func splitPath(escaped string) []string {
	trimmed := strings.Trim(escaped, "/")
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "/")
	for i, p := range parts {
		if u, err := url.PathUnescape(p); err == nil {
			parts[i] = u
		}
	}
	return parts
}

func (s *Server) setCORS(w http.ResponseWriter) {
	if s.cors == "" {
		return
	}
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", s.cors)
	h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Content-Type")
	h.Set("Vary", "Origin")
}

// ---------------------------------------------------------------------------
// Router
// ---------------------------------------------------------------------------

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			writeError(w, http.StatusInternalServerError, "internal", fmt.Sprint("unexpected error: ", rec))
		}
	}()
	s.setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	segs := splitPath(r.URL.EscapedPath())
	if len(segs) < 2 || segs[0] != "api" {
		writeError(w, http.StatusNotFound, "not_found", "no such route")
		return
	}
	segs = segs[1:]

	switch segs[0] {
	case "health":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, "GET")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case "ruleset":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, "GET")
			return
		}
		writeJSON(w, http.StatusOK, effectiveRuleset(s.rules))
	case "openapi.json":
		s.serveOpenAPI(w, r)
	case "docs":
		s.serveDocs(w, r)
	case "sheet-preview":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, "POST")
			return
		}
		s.sheetPreview(w, r)
	case "content":
		s.routeContent(w, r, segs[1:])
	case "characters":
		s.routeCharacters(w, r, segs[1:])
	default:
		writeError(w, http.StatusNotFound, "not_found", "no such route")
	}
}

func (s *Server) routeCharacters(w http.ResponseWriter, r *http.Request, segs []string) {
	if len(segs) == 0 {
		switch r.Method {
		case http.MethodGet:
			s.listCharacters(w)
		case http.MethodPost:
			s.createCharacter(w, r)
		default:
			methodNotAllowed(w, "GET", "POST")
		}
		return
	}

	id := segs[0]
	if err := validID(id); err != nil {
		s.fail(w, err)
		return
	}

	if len(segs) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.getCharacter(w, id)
		case http.MethodPut:
			s.putCharacter(w, r, id)
		case http.MethodPatch:
			s.patchCharacter(w, r, id)
		case http.MethodDelete:
			s.deleteCharacter(w, id)
		default:
			methodNotAllowed(w, "GET", "PUT", "PATCH", "DELETE")
		}
		return
	}

	sub, rest := segs[1], segs[2:]
	post := func() bool {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, "POST")
			return false
		}
		return true
	}

	switch sub {
	case "sheet":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, "GET")
			return
		}
		s.getSheet(w, id)

	case "hp":
		if post() {
			actOn(s, w, r, id, func(c *Character, pre Sheet, a HPAction) (map[string]any, error) {
				return nil, ApplyHP(c, pre.MaxHP, a)
			})
		}

	case "spell-slots":
		if post() {
			actOn(s, w, r, id, func(c *Character, _ Sheet, a SlotAction) (map[string]any, error) {
				return nil, ApplySlots(c, a)
			})
		}

	case "resources":
		if len(rest) != 1 {
			writeError(w, http.StatusNotFound, "not_found", "use /resources/{feature id}")
			return
		}
		if post() {
			key := rest[0]
			actOn(s, w, r, id, func(c *Character, _ Sheet, a ResourceAction) (map[string]any, error) {
				return nil, ApplyResource(c, key, a)
			})
		}

	case "rest":
		if post() {
			actOn(s, w, r, id, func(c *Character, pre Sheet, req restRequest) (map[string]any, error) {
				return nil, ApplyRest(c, pre, req.Type)
			})
		}

	case "conditions":
		if post() {
			actOn(s, w, r, id, func(c *Character, _ Sheet, a ConditionsAction) (map[string]any, error) {
				return nil, ApplyConditions(c, a)
			})
		}

	case "selections":
		switch r.Method {
		case http.MethodPost:
			actOn(s, w, r, id, func(c *Character, _ Sheet, sel Selection) (map[string]any, error) {
				return nil, UpsertSelection(c, sel)
			})
		case http.MethodDelete:
			featureID, choiceID := r.URL.Query().Get("feature_id"), r.URL.Query().Get("choice_id")
			if featureID == "" || choiceID == "" {
				s.fail(w, badReq("feature_id and choice_id are required"))
				return
			}
			s.act(w, id, func(c *Character, _ Sheet) (map[string]any, error) {
				return map[string]any{"removed": RemoveSelection(c, featureID, choiceID)}, nil
			})
		default:
			methodNotAllowed(w, "POST", "DELETE")
		}

	case "acknowledgements":
		switch r.Method {
		case http.MethodPost:
			actOn(s, w, r, id, func(c *Character, _ Sheet, a ackRequest) (map[string]any, error) {
				return nil, UpsertAcknowledgement(c, a.Key, a.Note, time.Now().UTC())
			})
		case http.MethodDelete:
			key := r.URL.Query().Get("key")
			if key == "" {
				s.fail(w, badReq("key is required"))
				return
			}
			s.act(w, id, func(c *Character, _ Sheet) (map[string]any, error) {
				return map[string]any{"removed": RemoveAcknowledgement(c, key)}, nil
			})
		default:
			methodNotAllowed(w, "POST", "DELETE")
		}

	case "level-up":
		if post() {
			actOn(s, w, r, id, func(c *Character, _ Sheet, req LevelUpRequest) (map[string]any, error) {
				return nil, LevelUp(c, s.library(), req, s.roll)
			})
		}

	case "choice-options":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, "GET")
			return
		}
		s.choiceOptions(w, r, id)

	case "coins":
		if post() {
			actOn(s, w, r, id, func(c *Character, _ Sheet, a CoinsAction) (map[string]any, error) {
				return nil, ApplyCoins(c, s.rules, a)
			})
		}

	case "inventory":
		s.routeInventory(w, r, id, rest)

	default:
		writeError(w, http.StatusNotFound, "not_found", "no such route")
	}
}

type restRequest struct {
	Type string `json:"type"`
}

type ackRequest struct {
	Key  string `json:"key"`
	Note string `json:"note"`
}

type quantityRequest struct {
	Delta int `json:"delta"`
}

func (s *Server) routeInventory(w http.ResponseWriter, r *http.Request, id string, rest []string) {
	switch len(rest) {
	case 0:
		if r.Method != http.MethodPost {
			methodNotAllowed(w, "POST")
			return
		}
		actOn(s, w, r, id, func(c *Character, _ Sheet, req AddItemRequest) (map[string]any, error) {
			entryID, err := AddItem(c, s.library(), req)
			if err != nil {
				return nil, err
			}
			return map[string]any{"entry_id": entryID}, nil
		})

	case 1:
		entryID := rest[0]
		switch r.Method {
		case http.MethodPatch:
			var patch map[string]json.RawMessage
			if err := readJSON(w, r, &patch); err != nil {
				s.fail(w, err)
				return
			}
			s.act(w, id, func(c *Character, _ Sheet) (map[string]any, error) {
				return nil, PatchEntry(c, entryID, patch)
			})
		case http.MethodDelete:
			deleteContents := r.URL.Query().Get("contents") == "delete"
			s.act(w, id, func(c *Character, _ Sheet) (map[string]any, error) {
				return nil, RemoveEntry(c, entryID, deleteContents)
			})
		default:
			methodNotAllowed(w, "PATCH", "DELETE")
		}

	case 2:
		entryID := rest[0]
		if r.Method != http.MethodPost {
			methodNotAllowed(w, "POST")
			return
		}
		switch rest[1] {
		case "quantity":
			actOn(s, w, r, id, func(c *Character, _ Sheet, req quantityRequest) (map[string]any, error) {
				removed, err := ChangeQuantity(c, entryID, req.Delta)
				if err != nil {
					return nil, err
				}
				return map[string]any{"removed": removed}, nil
			})
		case "move":
			actOn(s, w, r, id, func(c *Character, _ Sheet, req MoveRequest) (map[string]any, error) {
				movedID, err := MoveEntry(c, entryID, req)
				if err != nil {
					return nil, err
				}
				return map[string]any{"entry_id": movedID}, nil
			})
		default:
			writeError(w, http.StatusNotFound, "not_found", "no such route")
		}

	default:
		writeError(w, http.StatusNotFound, "not_found", "no such route")
	}
}

// ---------------------------------------------------------------------------
// Characters
// ---------------------------------------------------------------------------

func (s *Server) listCharacters(w http.ResponseWriter) {
	list, err := s.store.List()
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) createCharacter(w http.ResponseWriter, r *http.Request) {
	var c Character
	if err := readJSON(w, r, &c); err != nil {
		s.fail(w, err)
		return
	}
	if c.ID == "" {
		c.ID = NewCharacterID()
	} else if err := validID(c.ID); err != nil {
		s.fail(w, err)
		return
	}

	unlock := s.locks.lock(c.ID)
	defer unlock()
	if _, err := s.store.Get(c.ID); err == nil {
		s.fail(w, &reqError{Code: "already_exists", Msg: fmt.Sprintf("character %s already exists", c.ID)})
		return
	} else if !errors.Is(err, ErrNotFound) {
		s.fail(w, err)
		return
	}
	if err := s.store.Save(c); err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Location", "/api/characters/"+c.ID)
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) getCharacter(w http.ResponseWriter, id string) {
	c, err := s.store.Get(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) putCharacter(w http.ResponseWriter, r *http.Request, id string) {
	var c Character
	if err := readJSON(w, r, &c); err != nil {
		s.fail(w, err)
		return
	}
	if c.ID != "" && c.ID != id {
		s.fail(w, badReq("the body's id %q doesn't match the URL's %q", c.ID, id))
		return
	}
	c.ID = id

	unlock := s.locks.lock(id)
	defer unlock()
	if _, err := s.store.Get(id); err != nil { // replacing, not creating: a typo in the URL shouldn't make a new character
		s.fail(w, err)
		return
	}
	if err := s.store.Save(c); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) patchCharacter(w http.ResponseWriter, r *http.Request, id string) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		s.fail(w, badReq("could not read the request body: %v", err))
		return
	}

	unlock := s.locks.lock(id)
	defer unlock()
	current, err := s.store.Get(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	patched, err := MergePatchCharacter(current, body)
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.store.Save(patched); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, patched)
}

func (s *Server) deleteCharacter(w http.ResponseWriter, id string) {
	unlock := s.locks.lock(id)
	defer unlock()
	if err := s.store.Delete(id); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getSheet(w http.ResponseWriter, id string) {
	c, err := s.store.Get(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	sh, err := s.sheetFor(c)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sh)
}

func (s *Server) sheetPreview(w http.ResponseWriter, r *http.Request) {
	var c Character
	if err := readJSON(w, r, &c); err != nil {
		s.fail(w, err)
		return
	}
	sh, err := s.sheetFor(c)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sh)
}

func (s *Server) choiceOptions(w http.ResponseWriter, r *http.Request, id string) {
	featureID, choiceID := r.URL.Query().Get("feature_id"), r.URL.Query().Get("choice_id")
	if featureID == "" || choiceID == "" {
		s.fail(w, badReq("feature_id and choice_id are required"))
		return
	}
	c, err := s.store.Get(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	opts, err := ChoiceOptions(c, s.library(), s.rules, featureID, choiceID)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

// act runs one read-modify-write on a character under its lock. fn gets the character and the sheet
// as it was before the change; anything it returns is added next to the fresh sheet in the response.
func (s *Server) act(w http.ResponseWriter, id string, fn func(c *Character, pre Sheet) (map[string]any, error)) {
	unlock := s.locks.lock(id)
	defer unlock()

	c, err := s.store.Get(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	pre, err := s.sheetFor(c)
	if err != nil {
		s.fail(w, err)
		return
	}
	extra, err := fn(&c, pre)
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.store.Save(c); err != nil {
		s.fail(w, err)
		return
	}
	sh, err := s.sheetFor(c)
	if err != nil {
		s.fail(w, err)
		return
	}
	resp := map[string]any{"sheet": sh}
	for k, v := range extra {
		resp[k] = v
	}
	writeJSON(w, http.StatusOK, resp)
}

// actOn decodes the body into T before taking the lock, then runs apply inside act.
func actOn[T any](s *Server, w http.ResponseWriter, r *http.Request, id string,
	apply func(c *Character, pre Sheet, req T) (map[string]any, error)) {

	var req T
	if err := readJSON(w, r, &req); err != nil {
		s.fail(w, err)
		return
	}
	s.act(w, id, func(c *Character, pre Sheet) (map[string]any, error) {
		return apply(c, pre, req)
	})
}

// ---------------------------------------------------------------------------
// Content
// ---------------------------------------------------------------------------

func contentOverview(lib *ContentLibrary) map[string]any {
	issues := lib.Issues()
	if issues == nil {
		issues = []Issue{}
	}
	errs := 0
	for _, is := range issues {
		if is.Severity == SevError {
			errs++
		}
	}
	return map[string]any{
		"counts":   lib.Counts(),
		"issues":   issues,
		"errors":   errs,
		"warnings": len(issues) - errs,
	}
}

func (s *Server) routeContent(w http.ResponseWriter, r *http.Request, segs []string) {
	lib := s.library()

	switch {
	case len(segs) == 0:
		if r.Method != http.MethodGet {
			methodNotAllowed(w, "GET")
			return
		}
		writeJSON(w, http.StatusOK, contentOverview(lib))

	case len(segs) == 1 && segs[0] == "reload":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, "POST")
			return
		}
		if s.reload == nil {
			writeError(w, http.StatusNotImplemented, "not_supported", "this server was started without reload support")
			return
		}
		fresh, err := s.reload()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "reload_failed", err.Error()+" (the previous content is still in use)")
			return
		}
		s.mu.Lock()
		s.lib = fresh
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, contentOverview(fresh))

	case len(segs) == 1:
		if r.Method != http.MethodGet {
			methodNotAllowed(w, "GET")
			return
		}
		list, err := lib.List(segs[0])
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, list)

	case len(segs) == 2:
		if r.Method != http.MethodGet {
			methodNotAllowed(w, "GET")
			return
		}
		v, err := lib.Lookup(segs[0], segs[1])
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)

	default:
		writeError(w, http.StatusNotFound, "not_found", "no such route")
	}
}
