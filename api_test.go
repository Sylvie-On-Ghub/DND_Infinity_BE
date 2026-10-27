package character

// api_test.go - tests for api.go through net/http/httptest, using the in-memory store and the
// example content pack (tests skip themselves if content/example isn't there).
// Written without being able to run them: a failure may be a test mistake as much as a code bug.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestServer(t *testing.T, mods ...func(*Config)) (*Server, *MemoryStore) {
	t.Helper()
	lib := loadExample(t)
	store := NewMemoryStore()
	cfg := Config{Store: store, Library: lib}
	for _, m := range mods {
		m(&cfg)
	}
	return NewServer(cfg), store
}

// call sends a request. body may be nil, a string of raw JSON, or any value to marshal.
func call(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		data, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(data)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, rd))
	return rec
}

func into(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("response is not the expected JSON (%v): %s", err, rec.Body.String())
	}
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, status, rec.Body.String())
	}
}

func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	wantStatus(t, rec, status)
	var e errorBody
	into(t, rec, &e)
	if e.Error.Code != code {
		t.Errorf("error code = %q, want %q (message: %s)", e.Error.Code, code, e.Error.Message)
	}
}

type envelope struct {
	Sheet   Sheet  `json:"sheet"`
	EntryID string `json:"entry_id"`
	Removed bool   `json:"removed"`
}

func post(t *testing.T, h http.Handler, path string, body any) envelope {
	t.Helper()
	rec := call(t, h, http.MethodPost, path, body)
	wantStatus(t, rec, http.StatusOK)
	var e envelope
	into(t, rec, &e)
	return e
}

func getChar(t *testing.T, h http.Handler, id string) Character {
	t.Helper()
	rec := call(t, h, http.MethodGet, "/api/characters/"+id, nil)
	wantStatus(t, rec, http.StatusOK)
	var c Character
	into(t, rec, &c)
	return c
}

func rogueFixture(id string) Character {
	return Character{
		ID: id, Name: "Mira",
		Abilities: map[Ability]AbilityScore{
			Str: {Score: 10}, Dex: {Score: 16}, Con: {Score: 14}, Int: {Score: 12}, Wis: {Score: 10}, Cha: {Score: 10},
		},
		Classes:   []ClassLevel{{ClassID: "rogue", SubclassID: "thief", Level: 5, RolledHP: []int{8, 5, 5, 5, 5}}},
		CurrentHP: 38,
		TempHP:    5,
		Selections: []Selection{
			{FeatureID: "class:rogue:start", ChoiceID: "skills", Picked: []string{"stealth", "perception", "acrobatics", "sleight_of_hand"}},
			{FeatureID: "rogue_expertise_1", ChoiceID: "expertise", Picked: []string{"stealth", "sleight_of_hand"}},
		},
	}
}

func wizardFixture(id string) Character {
	return Character{
		ID: id, Name: "Orsik",
		Abilities: map[Ability]AbilityScore{Int: {Score: 16}},
		Classes:   []ClassLevel{{ClassID: "wizard", Level: 5, RolledHP: []int{6, 4, 4, 4, 4}}},
		Spellbooks: map[string]SpellState{"class:wizard": {
			Cantrips: []string{"fire_bolt"},
			Book:     []string{"fireball"},
			Prepared: []string{"fireball"},
		}},
	}
}

func saveFixture(t *testing.T, store *MemoryStore, c Character) {
	t.Helper()
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Basics
// ---------------------------------------------------------------------------

func TestAPIHealthAndRuleset(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := call(t, srv, http.MethodGet, "/api/health", nil)
	wantStatus(t, rec, http.StatusOK)

	rec = call(t, srv, http.MethodGet, "/api/ruleset", nil)
	wantStatus(t, rec, http.StatusOK)
	var r Ruleset
	into(t, rec, &r)
	if len(r.Currencies) != 5 || len(r.SlotTable) != 20 || len(r.PactTable) != 20 {
		t.Errorf("ruleset = %d currencies, %d slot rows, %d pact rows; want the defaults filled in (5, 20, 20)",
			len(r.Currencies), len(r.SlotTable), len(r.PactTable))
	}
}

func TestAPIRoutingErrors(t *testing.T) {
	srv, _ := newTestServer(t)
	wantError(t, call(t, srv, http.MethodGet, "/nope", nil), http.StatusNotFound, "not_found")
	wantError(t, call(t, srv, http.MethodGet, "/api", nil), http.StatusNotFound, "not_found")
	wantError(t, call(t, srv, http.MethodGet, "/api/unknown", nil), http.StatusNotFound, "not_found")

	rec := call(t, srv, http.MethodPost, "/api/health", nil)
	wantError(t, rec, http.StatusMethodNotAllowed, "method_not_allowed")
	if rec.Header().Get("Allow") != "GET" {
		t.Errorf("Allow = %q, want GET", rec.Header().Get("Allow"))
	}
	wantError(t, call(t, srv, http.MethodPost, "/api/characters/x/sheet", nil), http.StatusMethodNotAllowed, "method_not_allowed")
}

func TestAPICORS(t *testing.T) {
	srv, _ := newTestServer(t, func(c *Config) { c.CORSOrigin = "http://localhost:5173" })
	rec := call(t, srv, http.MethodOptions, "/api/characters", nil)
	wantStatus(t, rec, http.StatusNoContent)
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Errorf("preflight headers = %v", rec.Header())
	}
	rec = call(t, srv, http.MethodGet, "/api/health", nil)
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Error("normal responses should carry the CORS header too")
	}

	plain, _ := newTestServer(t)
	rec = call(t, plain, http.MethodGet, "/api/health", nil)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("no CORS headers should be sent when it isn't configured")
	}
}

// ---------------------------------------------------------------------------
// Characters
// ---------------------------------------------------------------------------

func TestAPICharacterLifecycle(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := call(t, srv, http.MethodPost, "/api/characters", `{"name":"Mira","classes":[{"class_id":"rogue","level":1,"rolled_hp":[8]}]}`)
	wantStatus(t, rec, http.StatusCreated)
	var created Character
	into(t, rec, &created)
	if created.ID == "" || rec.Header().Get("Location") != "/api/characters/"+created.ID {
		t.Fatalf("created = %+v, Location %q; want a generated id and a matching Location", created, rec.Header().Get("Location"))
	}
	id := created.ID

	if got := getChar(t, srv, id); got.Name != "Mira" {
		t.Errorf("name = %q, want Mira", got.Name)
	}

	rec = call(t, srv, http.MethodGet, "/api/characters", nil)
	wantStatus(t, rec, http.StatusOK)
	var list []CharacterSummary
	into(t, rec, &list)
	if len(list) != 1 || list[0].ID != id || list[0].Level != 1 {
		t.Errorf("list = %+v, want the one character at level 1", list)
	}

	updated := created
	updated.Name = "Mira the Bold"
	rec = call(t, srv, http.MethodPut, "/api/characters/"+id, updated)
	wantStatus(t, rec, http.StatusOK)
	if getChar(t, srv, id).Name != "Mira the Bold" {
		t.Error("PUT should replace the character")
	}

	rec = call(t, srv, http.MethodPatch, "/api/characters/"+id, `{"inspiration": 2}`)
	wantStatus(t, rec, http.StatusOK)
	if got := getChar(t, srv, id); got.Inspiration != 2 || got.Name != "Mira the Bold" {
		t.Errorf("after PATCH: inspiration %d, name %q; want 2 and the name kept", got.Inspiration, got.Name)
	}

	rec = call(t, srv, http.MethodGet, "/api/characters/"+id+"/sheet", nil)
	wantStatus(t, rec, http.StatusOK)
	var sh Sheet
	into(t, rec, &sh)
	if sh.Level != 1 || sh.ProficiencyBonus != 2 {
		t.Errorf("sheet level %d, PB %d; want 1 and 2", sh.Level, sh.ProficiencyBonus)
	}

	wantStatus(t, call(t, srv, http.MethodDelete, "/api/characters/"+id, nil), http.StatusNoContent)
	wantError(t, call(t, srv, http.MethodGet, "/api/characters/"+id, nil), http.StatusNotFound, "not_found")
}

func TestAPICharacterErrors(t *testing.T) {
	srv, store := newTestServer(t)
	saveFixture(t, store, rogueFixture("exists"))

	wantError(t, call(t, srv, http.MethodGet, "/api/characters/a%20b", nil), http.StatusBadRequest, "invalid_id")
	wantError(t, call(t, srv, http.MethodGet, "/api/characters/missing", nil), http.StatusNotFound, "not_found")
	wantError(t, call(t, srv, http.MethodPost, "/api/characters", `{"name":"x","nmae":"typo"}`), http.StatusBadRequest, "bad_request")
	wantError(t, call(t, srv, http.MethodPost, "/api/characters", `{oops`), http.StatusBadRequest, "bad_request")
	wantError(t, call(t, srv, http.MethodPost, "/api/characters", ""), http.StatusBadRequest, "bad_request")
	wantError(t, call(t, srv, http.MethodPost, "/api/characters", `{"id":"exists","name":"dup"}`), http.StatusConflict, "already_exists")
	wantError(t, call(t, srv, http.MethodPost, "/api/characters", `{"id":"bad id"}`), http.StatusBadRequest, "invalid_id")
	wantError(t, call(t, srv, http.MethodPut, "/api/characters/exists", `{"id":"other"}`), http.StatusBadRequest, "bad_request")
	wantError(t, call(t, srv, http.MethodPut, "/api/characters/missing", `{"name":"x"}`), http.StatusNotFound, "not_found")
	wantError(t, call(t, srv, http.MethodPatch, "/api/characters/exists", `{"inspiraton": 1}`), http.StatusBadRequest, "bad_request")
	wantError(t, call(t, srv, http.MethodPatch, "/api/characters/exists", `{"id": "other"}`), http.StatusBadRequest, "bad_request")
	wantError(t, call(t, srv, http.MethodDelete, "/api/characters/missing", nil), http.StatusNotFound, "not_found")
}

func TestAPISheetPreviewDoesNotSave(t *testing.T) {
	srv, store := newTestServer(t)
	rec := call(t, srv, http.MethodPost, "/api/sheet-preview", rogueFixture("preview"))
	wantStatus(t, rec, http.StatusOK)
	var sh Sheet
	into(t, rec, &sh)
	if sh.Level != 5 || sh.ProficiencyBonus != 3 {
		t.Errorf("preview level %d, PB %d; want 5 and 3", sh.Level, sh.ProficiencyBonus)
	}
	if list, _ := store.List(); len(list) != 0 {
		t.Errorf("a preview must not save anything: %+v", list)
	}
}

// ---------------------------------------------------------------------------
// Play actions
// ---------------------------------------------------------------------------

func TestAPIHPAndRest(t *testing.T) {
	srv, store := newTestServer(t)
	saveFixture(t, store, rogueFixture("r1")) // max HP 38, 38 current, 5 temp

	e := post(t, srv, "/api/characters/r1/hp", `{"damage": 10}`)
	if c := getChar(t, srv, "r1"); c.TempHP != 0 || c.CurrentHP != 33 {
		t.Errorf("after damage: temp %d, hp %d; want 0 and 33", c.TempHP, c.CurrentHP)
	}
	if e.Sheet.MaxHP != 38 {
		t.Errorf("sheet max HP = %d, want 38", e.Sheet.MaxHP)
	}

	post(t, srv, "/api/characters/r1/hp", `{"heal": 100}`)
	if c := getChar(t, srv, "r1"); c.CurrentHP != 38 {
		t.Errorf("heal should stop at max: hp %d", c.CurrentHP)
	}

	post(t, srv, "/api/characters/r1/hp", `{"set": 3}`)
	post(t, srv, "/api/characters/r1/rest", `{"type": "long"}`)
	if c := getChar(t, srv, "r1"); c.CurrentHP != 38 || c.TempHP != 0 {
		t.Errorf("long rest: hp %d, temp %d; want 38 and 0", c.CurrentHP, c.TempHP)
	}

	wantError(t, call(t, srv, http.MethodPost, "/api/characters/r1/hp", `{}`), http.StatusBadRequest, "bad_request")
	wantError(t, call(t, srv, http.MethodPost, "/api/characters/r1/hp", `{"damge": 3}`), http.StatusBadRequest, "bad_request")
	wantError(t, call(t, srv, http.MethodPost, "/api/characters/r1/rest", `{"type": "nap"}`), http.StatusBadRequest, "bad_request")
	wantError(t, call(t, srv, http.MethodPost, "/api/characters/nope/hp", `{"damage": 1}`), http.StatusNotFound, "not_found")
}

func TestAPISlotsAndResources(t *testing.T) {
	srv, store := newTestServer(t)
	saveFixture(t, store, wizardFixture("w1"))

	e := post(t, srv, "/api/characters/w1/spell-slots", `{"level": 1, "spend": 2}`)
	found := false
	for _, s := range e.Sheet.SpellSlots {
		if s.Level == 1 && s.Max == 4 && s.Used == 2 && s.Remaining == 2 {
			found = true
		}
	}
	if !found {
		t.Errorf("slots = %+v, want level 1 with 2 of 4 used", e.Sheet.SpellSlots)
	}

	e = post(t, srv, "/api/characters/w1/resources/arcane_recovery", `{"spend": 1}`)
	used := 0
	for _, r := range e.Sheet.Resources {
		if r.FeatureID == "arcane_recovery" {
			used = r.Used
		}
	}
	if used != 1 {
		t.Errorf("arcane recovery used = %d, want 1", used)
	}

	post(t, srv, "/api/characters/w1/rest", `{"type": "long"}`)
	c := getChar(t, srv, "w1")
	if len(c.SpellSlotsUsed) != 0 || c.ResourceUsed["arcane_recovery"] != 0 {
		t.Errorf("a long rest should restore slots and resources: %+v / %+v", c.SpellSlotsUsed, c.ResourceUsed)
	}

	wantError(t, call(t, srv, http.MethodPost, "/api/characters/w1/spell-slots", `{"level": 1}`), http.StatusBadRequest, "bad_request")
	wantStatus(t, call(t, srv, http.MethodGet, "/api/characters/w1/resources", nil), http.StatusNotFound)
}

func TestAPIConditionsSelectionsAndAcknowledgements(t *testing.T) {
	srv, store := newTestServer(t)
	saveFixture(t, store, rogueFixture("r1"))

	e := post(t, srv, "/api/characters/r1/conditions", `{"add": ["poisoned"]}`)
	if !e.Sheet.Skills["stealth"].Disadvantage {
		t.Error("poisoned should give disadvantage on ability checks")
	}
	e = post(t, srv, "/api/characters/r1/conditions", `{"remove": ["poisoned"]}`)
	if e.Sheet.Skills["stealth"].Disadvantage {
		t.Error("removing the condition should remove the disadvantage")
	}

	// Ability Score Improvement is a feat: pick it at the class feature, then spend its two points.
	// The feat's own choice belongs to this pick, so its feature id says where it came from.
	instance := "ability_score_improvement@rogue_asi_4:feat"
	picked := post(t, srv, "/api/characters/r1/selections", `{"feature_id":"rogue_asi_4","choice_id":"feat","picked":["ability_score_improvement"]}`)
	if p, ok := findPending(picked.Sheet, instance, "scores"); !ok || p.Remaining != 2 {
		t.Errorf("picking the feat should leave its two points pending under %q, got %+v", instance, picked.Sheet.PendingChoices)
	}
	before := post(t, srv, "/api/characters/r1/selections", `{"feature_id":"`+instance+`","choice_id":"scores","allocated":{"str":2}}`)
	if got := before.Sheet.Abilities[Str].Score; got != 12 {
		t.Errorf("Str = %d, want 12 after the improvement", got)
	}
	e = envelopeFrom(t, call(t, srv, http.MethodDelete, "/api/characters/r1/selections?feature_id="+instance+"&choice_id=scores", nil))
	if !e.Removed || e.Sheet.Abilities[Str].Score != 10 {
		t.Errorf("after delete: removed %v, Str %d; want true and 10", e.Removed, e.Sheet.Abilities[Str].Score)
	}
	wantError(t, call(t, srv, http.MethodDelete, "/api/characters/r1/selections?feature_id=x", nil), http.StatusBadRequest, "bad_request")

	post(t, srv, "/api/characters/r1/acknowledgements", `{"key":"over_cap:::5","note":"DM ok"}`)
	if c := getChar(t, srv, "r1"); len(c.Acknowledgements) != 1 || c.Acknowledgements[0].Note != "DM ok" {
		t.Errorf("acknowledgements = %+v", c.Acknowledgements)
	}
	e = envelopeFrom(t, call(t, srv, http.MethodDelete, "/api/characters/r1/acknowledgements?key=over_cap%3A%3A%3A5", nil))
	if !e.Removed {
		t.Error("the acknowledgement should have been removed")
	}
	wantError(t, call(t, srv, http.MethodDelete, "/api/characters/r1/acknowledgements", nil), http.StatusBadRequest, "bad_request")
}

func envelopeFrom(t *testing.T, rec *httptest.ResponseRecorder) envelope {
	t.Helper()
	wantStatus(t, rec, http.StatusOK)
	var e envelope
	into(t, rec, &e)
	return e
}

func TestAPIChoiceOptions(t *testing.T) {
	srv, store := newTestServer(t)
	saveFixture(t, store, rogueFixture("r1"))

	rec := call(t, srv, http.MethodGet, "/api/characters/r1/choice-options?feature_id=class:rogue:start&choice_id=skills", nil)
	wantStatus(t, rec, http.StatusOK)
	var opts []EligibleOption
	into(t, rec, &opts)
	if len(opts) != 11 {
		t.Errorf("starting skill options = %d, want the rogue's 11", len(opts))
	}

	rec = call(t, srv, http.MethodGet, "/api/characters/r1/choice-options?feature_id=rogue_asi_4&choice_id=feat", nil)
	wantStatus(t, rec, http.StatusOK)
	into(t, rec, &opts)
	wantFeats := []string{"ability_score_improvement", "alert", "lucky", "skilled", "watchful"}
	if len(opts) != len(wantFeats) {
		t.Fatalf("feat options = %+v, want %v", opts, wantFeats)
	}
	for i, id := range wantFeats {
		if opts[i].OptionID != id || !opts[i].Eligible {
			t.Errorf("feat option %d = %+v, want %s, eligible", i, opts[i], id)
		}
	}

	// A repeatable feat's own choice can be asked about by its instance id.
	rec = call(t, srv, http.MethodGet, "/api/characters/r1/choice-options?feature_id=ability_score_improvement@rogue_asi_4:feat&choice_id=scores", nil)
	wantStatus(t, rec, http.StatusOK)
	into(t, rec, &opts)
	if len(opts) != 6 {
		t.Errorf("ability score options = %d, want the six abilities", len(opts))
	}

	wantError(t, call(t, srv, http.MethodGet, "/api/characters/r1/choice-options?feature_id=x", nil), http.StatusBadRequest, "bad_request")
	wantError(t, call(t, srv, http.MethodGet, "/api/characters/r1/choice-options?feature_id=nope&choice_id=x", nil), http.StatusNotFound, "not_found")
}

func TestAPILevelUp(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := call(t, srv, http.MethodPost, "/api/characters", `{"id":"new","name":"Newbie"}`)
	wantStatus(t, rec, http.StatusCreated)

	e := post(t, srv, "/api/characters/new/level-up", `{"class_id":"rogue"}`)
	if e.Sheet.Level != 1 {
		t.Errorf("sheet level = %d, want 1", e.Sheet.Level)
	}
	post(t, srv, "/api/characters/new/level-up", `{"class_id":"rogue"}`)
	post(t, srv, "/api/characters/new/level-up", `{"class_id":"rogue","hp":{"method":"manual","value":7}}`)
	post(t, srv, "/api/characters/new/level-up", `{"class_id":"rogue","hp":{"method":"roll"}}`)

	c := getChar(t, srv, "new")
	if len(c.Classes) != 1 || c.Classes[0].Level != 4 {
		t.Fatalf("classes = %+v, want rogue 4", c.Classes)
	}
	hp := c.Classes[0].RolledHP
	if hp[0] != 8 || hp[1] != 5 || hp[2] != 7 || hp[3] < 1 || hp[3] > 8 {
		t.Errorf("rolled hp = %v, want 8 (first level max), 5 (average), 7 (manual) and a d8 roll", hp)
	}

	wantError(t, call(t, srv, http.MethodPost, "/api/characters/new/level-up", `{"class_id":"bard"}`), http.StatusBadRequest, "bad_request")
}

// ---------------------------------------------------------------------------
// Inventory and coins
// ---------------------------------------------------------------------------

func TestAPIInventoryFlow(t *testing.T) {
	srv, store := newTestServer(t)
	saveFixture(t, store, Character{ID: "inv", Name: "Packrat", Abilities: map[Ability]AbilityScore{Str: {Score: 10}}})

	bp := post(t, srv, "/api/characters/inv/inventory", `{"item_id":"backpack"}`).EntryID
	rations := post(t, srv, "/api/characters/inv/inventory", `{"item_id":"rations","quantity":3,"container":"`+bp+`"}`).EntryID
	if bp == "" || rations == "" {
		t.Fatal("adding items should return entry ids")
	}

	e := post(t, srv, "/api/characters/inv/inventory", `{"item_id":"rations"}`) // goes to the top level, not into the pack
	if e.EntryID == rations {
		t.Error("an item in a different container should not merge into the stack inside the pack")
	}
	sh := e.Sheet
	if len(sh.Inventory) != 3 || !approxF(sh.Carry.Weight, 5+3*2+2) {
		t.Errorf("inventory %d entries, weight %v; want 3 entries and 13", len(sh.Inventory), sh.Carry.Weight)
	}

	// Move one ration out of the pack: a split creates a new entry.
	moved := post(t, srv, "/api/characters/inv/inventory/"+rations+"/move", `{"container":"","quantity":1}`)
	if moved.EntryID == rations || moved.EntryID == "" {
		t.Errorf("a partial move should return the new entry's id, got %q", moved.EntryID)
	}

	used := post(t, srv, "/api/characters/inv/inventory/"+rations+"/quantity", `{"delta":-2}`)
	if !used.Removed {
		t.Error("using up the rest of the stack should remove the entry")
	}

	// Patch an entry's state and see it on the sheet.
	rec := call(t, srv, http.MethodPatch, "/api/characters/inv/inventory/"+bp, `{"equipped": true, "notes": "my pack"}`)
	wantStatus(t, rec, http.StatusOK)
	var patched envelope
	into(t, rec, &patched)
	foundPack := false
	for _, v := range patched.Sheet.Inventory {
		if v.EntryID == bp && v.Equipped && v.Notes == "my pack" {
			foundPack = true
		}
	}
	if !foundPack {
		t.Errorf("the patched pack should show as equipped with its note: %+v", patched.Sheet.Inventory)
	}

	wantError(t, call(t, srv, http.MethodPatch, "/api/characters/inv/inventory/"+bp, `{"quantity": 9}`), http.StatusBadRequest, "bad_request")
	wantError(t, call(t, srv, http.MethodPatch, "/api/characters/inv/inventory/nope", `{"notes": "x"}`), http.StatusNotFound, "not_found")
	wantError(t, call(t, srv, http.MethodPost, "/api/characters/inv/inventory", `{"item_id": "nope"}`), http.StatusBadRequest, "bad_request")
	wantError(t, call(t, srv, http.MethodPost, "/api/characters/inv/inventory", `{"item_id": "rope_hempen", "container": "nope"}`), http.StatusBadRequest, "bad_request")
	wantError(t, call(t, srv, http.MethodPost, "/api/characters/inv/inventory", `{"item_id": "rope_hempen", "custom": {"name": "x"}}`), http.StatusBadRequest, "bad_request")
	wantError(t, call(t, srv, http.MethodDelete, "/api/characters/inv/inventory/nope", nil), http.StatusNotFound, "not_found")

	// Deleting a container moves what was inside it to the top level.
	rope := post(t, srv, "/api/characters/inv/inventory", `{"item_id":"rope_hempen","container":"`+bp+`"}`).EntryID
	wantStatus(t, call(t, srv, http.MethodDelete, "/api/characters/inv/inventory/"+bp, nil), http.StatusOK)
	sawRope := false
	for _, e := range getChar(t, srv, "inv").Inventory {
		if e.ID == bp {
			t.Error("the backpack should be gone")
		}
		if e.ID == rope {
			sawRope = true
			if e.Container != "" {
				t.Errorf("the rope should be at the top level, but is in %q", e.Container)
			}
		}
	}
	if !sawRope {
		t.Error("the rope should have survived the pack being deleted")
	}
}

func TestAPICustomItemsAndBags(t *testing.T) {
	srv, store := newTestServer(t)
	saveFixture(t, store, Character{ID: "c", Name: "Bagger", Abilities: map[Ability]AbilityScore{Str: {Score: 10}}})

	// A one-off custom item with its own effect.
	charm := post(t, srv, "/api/characters/c/inventory",
		`{"custom":{"name":"Strange Charm","kind":"other","weight":0.5,"modifiers":[{"target":"ac","kind":"add","value":1}]}}`).EntryID
	if charm == "" {
		t.Fatal("a custom item should get an entry id")
	}

	e := envelopeFrom(t, call(t, srv, http.MethodPatch, "/api/characters/c/inventory/"+charm, `{"equipped": true}`))
	if e.Sheet.AC != 11 { // 10 + Dex 0 + the charm
		t.Errorf("AC = %d, want 11 with the charm equipped", e.Sheet.AC)
	}
	sawCharm := false
	for _, v := range e.Sheet.Inventory {
		if v.EntryID == charm && v.Custom && v.Name == "Strange Charm" {
			sawCharm = true
		}
	}
	if !sawCharm {
		t.Errorf("the custom item should appear on the sheet: %+v", e.Sheet.Inventory)
	}

	// A Bag of Holding: what's inside it adds no weight.
	bag := post(t, srv, "/api/characters/c/inventory", `{"item_id":"bag_of_holding"}`).EntryID
	e = post(t, srv, "/api/characters/c/inventory", `{"item_id":"rope_hempen","quantity":3,"container":"`+bag+`"}`)
	if !approxF(e.Sheet.Carry.Weight, 15.5) { // charm 0.5 + bag 15; the rope inside is weightless
		t.Errorf("weight = %v, want 15.5", e.Sheet.Carry.Weight)
	}
}

func TestAPICoins(t *testing.T) {
	srv, store := newTestServer(t)
	saveFixture(t, store, Character{ID: "c", Name: "Rich"})

	e := post(t, srv, "/api/characters/c/coins", `{"gain": {"gp": 10}}`)
	if e.Sheet.Wallet.Total != 1000 {
		t.Errorf("wallet total = %d, want 1000", e.Sheet.Wallet.Total)
	}
	e = post(t, srv, "/api/characters/c/coins", `{"spend": {"gp": 3}}`)
	if e.Sheet.Wallet.Total != 700 {
		t.Errorf("wallet total = %d, want 700", e.Sheet.Wallet.Total)
	}
	post(t, srv, "/api/characters/c/coins", `{"convert": {"from": "gp", "to": "sp", "amount": 1}}`)
	if c := getChar(t, srv, "c"); c.Coins["gp"] != 6 || c.Coins["sp"] != 10 {
		t.Errorf("coins = %v, want 6 gp and 10 sp", c.Coins)
	}

	// Spending coins you don't have goes negative and warns, rather than failing.
	e = post(t, srv, "/api/characters/c/coins", `{"spend": {"pp": 1}}`)
	if e.Sheet.Wallet.Total != -300 {
		t.Errorf("wallet total = %d, want -300", e.Sheet.Wallet.Total)
	}
	warned := false
	for _, w := range e.Sheet.Warnings {
		if w.Code == WarnNegativeCoins {
			warned = true
		}
	}
	if !warned {
		t.Errorf("expected a negative_coins warning, got %+v", e.Sheet.Warnings)
	}

	wantError(t, call(t, srv, http.MethodPost, "/api/characters/c/coins", `{"convert": {"from": "sp", "to": "gp", "amount": 5}}`), http.StatusBadRequest, "bad_conversion")
	wantError(t, call(t, srv, http.MethodPost, "/api/characters/c/coins", `{"gain": {"zorkmid": 1}}`), http.StatusBadRequest, "bad_request")
	wantError(t, call(t, srv, http.MethodPost, "/api/characters/c/coins", `{"gain": {"gp": 1}, "spend": {"gp": 1}}`), http.StatusBadRequest, "bad_request")
	wantError(t, call(t, srv, http.MethodPost, "/api/characters/c/coins", `{}`), http.StatusBadRequest, "bad_request")
	wantError(t, call(t, srv, http.MethodPost, "/api/characters/c/coins", `{"gian": {"gp": 1}}`), http.StatusBadRequest, "bad_request")
}

// ---------------------------------------------------------------------------
// Content
// ---------------------------------------------------------------------------

func TestAPIContent(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := call(t, srv, http.MethodGet, "/api/content", nil)
	wantStatus(t, rec, http.StatusOK)
	var overview struct {
		Counts   map[string]int `json:"counts"`
		Issues   []Issue        `json:"issues"`
		Errors   int            `json:"errors"`
		Warnings int            `json:"warnings"`
	}
	into(t, rec, &overview)
	if overview.Counts["classes"] != 2 || overview.Counts["skills"] != 18 || overview.Errors != 0 {
		t.Errorf("overview = %+v, want 2 classes, 18 skills and no errors", overview)
	}

	rec = call(t, srv, http.MethodGet, "/api/content/classes", nil)
	wantStatus(t, rec, http.StatusOK)
	var classes []ContentSummary
	into(t, rec, &classes)
	if len(classes) != 2 || classes[0].ID != "rogue" || classes[1].ID != "wizard" {
		t.Errorf("classes = %+v, want rogue and wizard sorted by id", classes)
	}

	rec = call(t, srv, http.MethodGet, "/api/content/classes/rogue", nil)
	wantStatus(t, rec, http.StatusOK)
	var rogue Class
	into(t, rec, &rogue)
	if rogue.HitDie != 8 || rogue.SubclassLevel != 3 {
		t.Errorf("rogue = %+v, want hit die 8 and subclass level 3", rogue)
	}

	rec = call(t, srv, http.MethodGet, "/api/content/skills", nil)
	var skills []ContentSummary
	into(t, rec, &skills)
	if len(skills) != 18 {
		t.Errorf("skills = %d, want 18", len(skills))
	}

	wantError(t, call(t, srv, http.MethodGet, "/api/content/widgets", nil), http.StatusNotFound, "not_found")
	wantError(t, call(t, srv, http.MethodGet, "/api/content/classes/bard", nil), http.StatusNotFound, "not_found")
	wantError(t, call(t, srv, http.MethodPost, "/api/content/classes", nil), http.StatusMethodNotAllowed, "method_not_allowed")
	wantError(t, call(t, srv, http.MethodPost, "/api/content/reload", nil), http.StatusNotImplemented, "not_supported")
}

func TestAPIReloadSwapsContent(t *testing.T) {
	example := loadExample(t)
	srv := NewServer(Config{
		Store:   NewMemoryStore(),
		Library: newContentLibrary(), // starts empty
		Reload:  func() (*ContentLibrary, error) { return example, nil },
	})

	rec := call(t, srv, http.MethodGet, "/api/content/classes", nil)
	var before []ContentSummary
	into(t, rec, &before)
	if len(before) != 0 {
		t.Fatalf("the server should start with no classes, got %+v", before)
	}

	wantStatus(t, call(t, srv, http.MethodPost, "/api/content/reload", nil), http.StatusOK)

	rec = call(t, srv, http.MethodGet, "/api/content/classes", nil)
	var after []ContentSummary
	into(t, rec, &after)
	if len(after) != 2 {
		t.Errorf("after reload: %+v, want 2 classes", after)
	}
}

func TestAPIFailedReloadKeepsOldContent(t *testing.T) {
	srv := NewServer(Config{
		Store:   NewMemoryStore(),
		Library: loadExample(t),
		Reload:  func() (*ContentLibrary, error) { return nil, errors.New("boom") },
	})
	rec := call(t, srv, http.MethodPost, "/api/content/reload", nil)
	wantError(t, rec, http.StatusInternalServerError, "reload_failed")
	if !strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("the failure reason should be reported: %s", rec.Body.String())
	}

	rec = call(t, srv, http.MethodGet, "/api/content/classes", nil)
	var classes []ContentSummary
	into(t, rec, &classes)
	if len(classes) != 2 {
		t.Errorf("a failed reload must leave the old content in place, got %+v", classes)
	}
}

// ---------------------------------------------------------------------------
// Concurrency
// ---------------------------------------------------------------------------

func TestAPIActionsOnOneCharacterAreSerialised(t *testing.T) {
	srv, store := newTestServer(t)
	saveFixture(t, store, Character{ID: "busy", Name: "Busy"})

	const n = 25
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := call(t, srv, http.MethodPost, "/api/characters/busy/coins", `{"gain":{"gp":1}}`)
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d: %s", rec.Code, rec.Body.String())
			}
		}()
	}
	wg.Wait()

	if got := getChar(t, srv, "busy").Coins["gp"]; got != n {
		t.Errorf("gp = %d, want %d: concurrent actions must not overwrite each other", got, n)
	}
}

func approxF(a, b float64) bool {
	d := a - b
	return d < 0.0001 && d > -0.0001
}

func TestAPIPortraitURL(t *testing.T) {
	srv, _ := newTestServer(t)
	wantStatus(t, call(t, srv, http.MethodPost, "/api/characters", `{"id":"p1","name":"Pic"}`), http.StatusCreated)

	rec := call(t, srv, http.MethodPatch, "/api/characters/p1", `{"portrait_url":"https://example.com/mira.png"}`)
	wantStatus(t, rec, http.StatusOK)
	var c Character
	into(t, rec, &c)
	if c.PortraitURL != "https://example.com/mira.png" {
		t.Errorf("portrait_url = %q after PATCH", c.PortraitURL)
	}

	var list []CharacterSummary
	into(t, call(t, srv, http.MethodGet, "/api/characters", nil), &list)
	if len(list) != 1 || list[0].PortraitURL != "https://example.com/mira.png" {
		t.Errorf("the character list should carry the portrait: %+v", list)
	}

	for _, bad := range []string{"javascript:alert(1)", "data:image/png;base64,AAAA", "/local/path.png", "https://"} {
		body, _ := json.Marshal(map[string]string{"portrait_url": bad})
		wantError(t, call(t, srv, http.MethodPatch, "/api/characters/p1", string(body)), http.StatusBadRequest, "bad_request")
	}
	wantError(t, call(t, srv, http.MethodPost, "/api/characters", `{"id":"p2","portrait_url":"ftp://x/y.png"}`), http.StatusBadRequest, "bad_request")

	// A rejected change leaves the stored link alone, and null clears it.
	into(t, call(t, srv, http.MethodGet, "/api/characters/p1", nil), &c)
	if c.PortraitURL != "https://example.com/mira.png" {
		t.Errorf("a rejected PATCH changed portrait_url to %q", c.PortraitURL)
	}
	rec = call(t, srv, http.MethodPatch, "/api/characters/p1", `{"portrait_url":null}`)
	wantStatus(t, rec, http.StatusOK)
	c = Character{}
	into(t, rec, &c)
	if c.PortraitURL != "" {
		t.Errorf("portrait_url = %q after clearing it", c.PortraitURL)
	}
}

func TestAPIDetailsPatchOneFieldAtATime(t *testing.T) {
	srv, _ := newTestServer(t)
	wantStatus(t, call(t, srv, http.MethodPost, "/api/characters",
		`{"id":"d1","details":{"alignment":"Chaotic Good","bonds":"Baba (mentor)"}}`), http.StatusCreated)

	rec := call(t, srv, http.MethodPatch, "/api/characters/d1", `{"details":{"faith":"Mielikki","bonds":null}}`)
	wantStatus(t, rec, http.StatusOK)
	var c Character
	into(t, rec, &c)
	want := Details{Alignment: "Chaotic Good", Faith: "Mielikki"}
	if c.Details != want {
		t.Errorf("details = %+v, want %+v: a PATCH changes only the fields it names", c.Details, want)
	}
	wantError(t, call(t, srv, http.MethodPatch, "/api/characters/d1", `{"details":{"eye":"yellow"}}`), http.StatusBadRequest, "bad_request")
}
