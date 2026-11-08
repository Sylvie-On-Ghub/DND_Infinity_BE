package character

// api.go - the HTTP API, standard library only.
//
// Conventions
//   - JSON in and out, under /api.
//   - Errors: {"error": {"code": "...", "message": "..."}} with codes bad_request, invalid_id,
//     unauthorized, not_found, already_exists, version_conflict, bad_conversion, rate_limited,
//     method_not_allowed, not_supported, internal.
//   - Request bodies are strict: unknown fields are a 400, so a typo can't silently drop data.
//   - Rules problems are never errors; they come back as warnings on the sheet.
//   - Action routes return {"sheet": ...} plus any extra fields (an entry id, a removed flag).
//   - Read-modify-write actions on one character are serialised by a per-character lock.
//   - PUT and PATCH send the version they read; a stale one is a 409, so edits can't be lost.
//
//   - Character routes need a session; see auth.go. Content and server information are public.
//
// Routing is done by hand so it doesn't depend on a recent Go version's pattern mux.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Store       CharacterStore
	Library     *ContentLibrary
	Reload      func() (*ContentLibrary, error) // optional; enables POST /api/content/reload
	Ruleset     Ruleset
	CORSOrigins []string // browser origins allowed to call the API; empty sends no CORS headers, "*" allows any
	Auth        *Auth    // signs users in; without it, protected routes accept nobody

	// TrustedProxyHeader names the header holding the client's address, such as X-Forwarded-For.
	// Set it only if every request comes through a proxy that sets it, since clients can forge it.
	TrustedProxyHeader string
}

type Server struct {
	store  CharacterStore
	rules  Ruleset
	cors   []string
	reload func() (*ContentLibrary, error)
	auth   *Auth

	proxyHeader string

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
		cors:   cfg.CORSOrigins,
		reload: cfg.Reload,
		auth:   cfg.Auth,

		proxyHeader: cfg.TrustedProxyHeader,

		lib:   lib,
		locks: &lockMap{m: map[string]*sync.Mutex{}},
		rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

var _ http.Handler = (*Server)(nil)

type lockMap struct {
	mu sync.Mutex
	m  map[string]*sync.Mutex
}

// lock takes the lock for one owner's character and returns the function that releases it.
func (l *lockMap) lock(ownerID, id string) func() {
	key := ownerID + "/" + id // neither can contain a slash
	l.mu.Lock()
	m, ok := l.m[key]
	if !ok {
		m = &sync.Mutex{}
		l.m[key] = m
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
	sh, err := Compute(c, s.library(), s.rules)
	if err != nil {
		return Sheet{}, err
	}
	sh.Version = c.Version
	return sh, nil
}

type userIDKey struct{}

func withUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}

// userID returns the signed-in user; requireAuth has already run on any route that uses it.
func userID(r *http.Request) string {
	id, _ := r.Context().Value(userIDKey{}).(string)
	return id
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
	case errors.Is(err, ErrAlreadyExists):
		writeError(w, http.StatusConflict, "already_exists", err.Error())
	case errors.Is(err, ErrVersionConflict):
		writeError(w, http.StatusConflict, "version_conflict", "this character has changed since you read it; fetch it again and retry")
	case errors.Is(err, ErrInvalidID):
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
	case errors.Is(err, ErrBadPatch):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
	default:
		log.Printf("internal error: %v", err) // can name files and SQL, so it stays out of the response
		writeError(w, http.StatusInternalServerError, "internal", "something went wrong on the server")
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

func (s *Server) setCORS(w http.ResponseWriter, r *http.Request) {
	if len(s.cors) == 0 {
		return
	}
	h := w.Header()
	h.Set("Vary", "Origin")
	origin := r.Header.Get("Origin")
	allowed := ""
	for _, o := range s.cors {
		if o == "*" || o == origin {
			allowed = o
			break
		}
	}
	if allowed == "" {
		return
	}
	h.Set("Access-Control-Allow-Origin", allowed)
	h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	h.Set("Access-Control-Expose-Headers", "Retry-After")
}

// ---------------------------------------------------------------------------
// Router
// ---------------------------------------------------------------------------

// accessLog is filled in while a request is served and logged once it's done.
type accessLog struct {
	http.ResponseWriter
	status int
	user   string // set by requireAuth
}

func (a *accessLog) WriteHeader(code int) {
	if a.status == 0 {
		a.status = code
	}
	a.ResponseWriter.WriteHeader(code)
}

func (a *accessLog) Write(b []byte) (int, error) {
	if a.status == 0 {
		a.status = http.StatusOK
	}
	return a.ResponseWriter.Write(b)
}

type accessLogKey struct{}

func (s *Server) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	start := time.Now()
	al := &accessLog{ResponseWriter: rw}
	w := http.ResponseWriter(al)
	r = r.WithContext(context.WithValue(r.Context(), accessLogKey{}, al))
	defer func() {
		user, status := al.user, al.status
		if user == "" {
			user = "-"
		}
		if status == 0 { // nothing written, so net/http sends 200
			status = http.StatusOK
		}
		// No query string, headers or body: they can carry codes and tokens. %q stops a crafted path forging log lines.
		log.Printf("%s %q %d %dms ip=%s user=%s", r.Method, r.URL.Path, status, time.Since(start).Milliseconds(), s.clientIP(r), user)
	}()
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("panic serving %s %q: %v", r.Method, r.URL.Path, rec)
			writeError(w, http.StatusInternalServerError, "internal", "something went wrong on the server")
		}
	}()
	s.setCORS(w, r)
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
		s.requireAuth(w, r, s.sheetPreview)
	case "me":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, "GET")
			return
		}
		s.requireAuth(w, r, s.me)
	case "auth":
		s.routeAuth(w, r, segs[1:])
	case "content":
		s.routeContent(w, r, segs[1:])
	case "characters":
		s.requireAuth(w, r, func(w http.ResponseWriter, r *http.Request) {
			s.routeCharacters(w, r, segs[1:])
		})
	default:
		writeError(w, http.StatusNotFound, "not_found", "no such route")
	}
}

func (s *Server) routeAuth(w http.ResponseWriter, r *http.Request, segs []string) {
	var handle func(http.ResponseWriter, *http.Request)
	switch strings.Join(segs, "/") {
	case "logout":
		handle = func(w http.ResponseWriter, r *http.Request) { s.requireAuth(w, r, s.logout) }
	case "otp/request":
		handle = s.requestCode
	case "otp/verify":
		handle = s.verifyCode
	default:
		writeError(w, http.StatusNotFound, "not_found", "no such route")
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if s.auth == nil {
		writeError(w, http.StatusNotImplemented, "not_supported", "this server was started without sign-in")
		return
	}
	handle(w, r)
}

func (s *Server) routeCharacters(w http.ResponseWriter, r *http.Request, segs []string) {
	if len(segs) == 0 {
		switch r.Method {
		case http.MethodGet:
			s.listCharacters(w, r)
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
			s.getCharacter(w, r, id)
		case http.MethodPut:
			s.putCharacter(w, r, id)
		case http.MethodPatch:
			s.patchCharacter(w, r, id)
		case http.MethodDelete:
			s.deleteCharacter(w, r, id)
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
		s.getSheet(w, r, id)

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
			s.act(w, r, id, func(c *Character, _ Sheet) (map[string]any, error) {
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
			s.act(w, r, id, func(c *Character, _ Sheet) (map[string]any, error) {
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

	case "pets":
		s.routePets(w, r, id, rest)

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

func (s *Server) routePets(w http.ResponseWriter, r *http.Request, id string, rest []string) {
	switch len(rest) {
	case 0:
		if r.Method != http.MethodPost {
			methodNotAllowed(w, "POST")
			return
		}
		actOn(s, w, r, id, func(c *Character, pre Sheet, req AddPetRequest) (map[string]any, error) {
			petID, err := AddPet(c, pre, s.library(), req)
			if err != nil {
				return nil, err
			}
			return map[string]any{"pet_id": petID}, nil
		})

	case 1:
		petID := rest[0]
		switch r.Method {
		case http.MethodPatch:
			actOn(s, w, r, id, func(c *Character, pre Sheet, p PetPatch) (map[string]any, error) {
				return nil, PatchPet(c, pre, s.library(), petID, p)
			})
		case http.MethodDelete:
			s.act(w, r, id, func(c *Character, _ Sheet) (map[string]any, error) {
				return nil, RemovePet(c, petID)
			})
		default:
			methodNotAllowed(w, "PATCH", "DELETE")
		}

	case 2:
		petID := rest[0]
		if r.Method != http.MethodPost {
			methodNotAllowed(w, "POST")
			return
		}
		switch rest[1] {
		case "hp":
			actOn(s, w, r, id, func(c *Character, pre Sheet, a HPAction) (map[string]any, error) {
				return nil, ApplyPetHP(c, pre, petID, a)
			})
		case "conditions":
			actOn(s, w, r, id, func(c *Character, _ Sheet, a ConditionsAction) (map[string]any, error) {
				return nil, ApplyPetConditions(c, petID, a)
			})
		default:
			writeError(w, http.StatusNotFound, "not_found", "no such route")
		}

	default:
		writeError(w, http.StatusNotFound, "not_found", "no such route")
	}
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
			s.act(w, r, id, func(c *Character, _ Sheet) (map[string]any, error) {
				return nil, PatchEntry(c, s.library(), entryID, patch)
			})
		case http.MethodDelete:
			deleteContents := r.URL.Query().Get("contents") == "delete"
			s.act(w, r, id, func(c *Character, _ Sheet) (map[string]any, error) {
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

func (s *Server) listCharacters(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.List(r.Context(), userID(r))
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

	created, err := s.store.Create(r.Context(), userID(r), c)
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Location", "/api/characters/"+created.ID)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) getCharacter(w http.ResponseWriter, r *http.Request, id string) {
	c, err := s.store.Get(r.Context(), userID(r), id)
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
	if c.Version <= 0 {
		s.fail(w, badReq("version is required: send the version you read"))
		return
	}

	owner := userID(r)
	unlock := s.locks.lock(owner, id)
	defer unlock()
	saved, err := s.store.Update(r.Context(), owner, c) // replacing, not creating: a typo in the URL shouldn't make a new character
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) patchCharacter(w http.ResponseWriter, r *http.Request, id string) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		s.fail(w, badReq("could not read the request body: %v", err))
		return
	}

	owner := userID(r)
	unlock := s.locks.lock(owner, id)
	defer unlock()
	current, err := s.store.Get(r.Context(), owner, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	patched, err := MergePatchCharacter(current, body)
	if err != nil {
		s.fail(w, err)
		return
	}
	// Merging keeps the current version when the patch has none, which would skip the conflict
	// check, so look for it in the patch itself. MergePatchCharacter has already validated the JSON.
	var sent struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(body, &sent); err != nil || sent.Version <= 0 {
		s.fail(w, badReq("version is required: send the version you read"))
		return
	}
	saved, err := s.store.Update(r.Context(), owner, patched)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) deleteCharacter(w http.ResponseWriter, r *http.Request, id string) {
	owner := userID(r)
	unlock := s.locks.lock(owner, id)
	defer unlock()
	if err := s.store.Delete(r.Context(), owner, id); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getSheet(w http.ResponseWriter, r *http.Request, id string) {
	c, err := s.store.Get(r.Context(), userID(r), id)
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
	c, err := s.store.Get(r.Context(), userID(r), id)
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
func (s *Server) act(w http.ResponseWriter, r *http.Request, id string, fn func(c *Character, pre Sheet) (map[string]any, error)) {
	owner := userID(r)
	unlock := s.locks.lock(owner, id)
	defer unlock()

	c, err := s.store.Get(r.Context(), owner, id)
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
	saved, err := s.store.Update(r.Context(), owner, c)
	if err != nil {
		s.fail(w, err)
		return
	}
	sh, err := s.sheetFor(saved)
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
	s.act(w, r, id, func(c *Character, pre Sheet) (map[string]any, error) {
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

func (s *Server) reloadContent(w http.ResponseWriter, r *http.Request) {
	if s.reload == nil {
		writeError(w, http.StatusNotImplemented, "not_supported", "this server was started without reload support")
		return
	}
	fresh, err := s.reload()
	if err != nil {
		log.Printf("content reload by user %s failed: %v", userID(r), err)
		writeError(w, http.StatusInternalServerError, "reload_failed", err.Error()+" (the previous content is still in use)")
		return
	}
	s.mu.Lock()
	s.lib = fresh
	s.mu.Unlock()
	c := fresh.Counts()
	log.Printf("content reloaded by user %s: %d features, %d classes, %d items, %d spells, %d skills",
		userID(r), c["features"], c["classes"], c["items"], c["spells"], c["skills"])
	writeJSON(w, http.StatusOK, contentOverview(fresh))
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
		s.requireAuth(w, r, s.reloadContent)

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
