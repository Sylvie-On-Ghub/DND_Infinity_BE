package character

// actions.go - the changes a frontend can make to a character, as plain functions on Character.
//
// None of this knows about HTTP. Each function applies one change and returns an error only when the
// request itself can't be carried out (malformed, unknown id, a conversion that doesn't come out
// whole). Rules problems are never errors: they surface as warnings on the sheet, as everywhere else.
// Errors are *reqError so the API layer can map them to status codes.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// reqError is a request the engine can't act on. Code becomes the API error code.
type reqError struct{ Code, Msg string }

func (e *reqError) Error() string { return e.Msg }

func badReq(format string, args ...any) error {
	return &reqError{Code: "bad_request", Msg: fmt.Sprintf(format, args...)}
}

func notFound(format string, args ...any) error {
	return &reqError{Code: "not_found", Msg: fmt.Sprintf(format, args...)}
}

// ---------------------------------------------------------------------------
// Hit points
// ---------------------------------------------------------------------------

// HPAction takes exactly one field. Damage hits temp HP first and stops at 0; heal stops at max HP
// (and never lowers HP); temp replaces temp HP; set bypasses every rule.
type HPAction struct {
	Damage *int `json:"damage"`
	Heal   *int `json:"heal"`
	Temp   *int `json:"temp"`
	Set    *int `json:"set"`
}

func ApplyHP(c *Character, maxHP int, a HPAction) error {
	fields := 0
	for _, p := range []*int{a.Damage, a.Heal, a.Temp, a.Set} {
		if p != nil {
			fields++
		}
	}
	if fields != 1 {
		return badReq("send exactly one of damage, heal, temp or set")
	}

	switch {
	case a.Damage != nil:
		if *a.Damage < 0 {
			return badReq("damage cannot be negative")
		}
		n := *a.Damage
		absorbed := n
		if c.TempHP < absorbed {
			absorbed = c.TempHP
		}
		c.TempHP -= absorbed
		n -= absorbed
		c.CurrentHP -= n
		if c.CurrentHP < 0 {
			c.CurrentHP = 0
		}
	case a.Heal != nil:
		if *a.Heal < 0 {
			return badReq("heal cannot be negative")
		}
		before := c.CurrentHP
		hp := before + *a.Heal
		if hp > maxHP {
			hp = maxHP
		}
		if hp < before {
			hp = before // healing never takes HP away, even when it is already above the maximum
		}
		c.CurrentHP = hp
		if before == 0 && hp > 0 {
			c.DeathSaves = [2]int{}
		}
	case a.Temp != nil:
		if *a.Temp < 0 {
			return badReq("temp cannot be negative")
		}
		c.TempHP = *a.Temp
	case a.Set != nil:
		if *a.Set < 0 {
			return badReq("set cannot be negative")
		}
		c.CurrentHP = *a.Set
	}
	return nil
}

// ---------------------------------------------------------------------------
// Spell slots, resources, rests, conditions
// ---------------------------------------------------------------------------

// SlotAction spends or restores slots, relative to what is already used. Overspending is allowed
// (the sheet warns). Pact true targets the pact pool and ignores level.
type SlotAction struct {
	Level   int  `json:"level"`
	Pact    bool `json:"pact"`
	Spend   int  `json:"spend"`
	Restore int  `json:"restore"`
}

func ApplySlots(c *Character, a SlotAction) error {
	if a.Spend < 0 || a.Restore < 0 {
		return badReq("spend and restore cannot be negative")
	}
	if a.Spend == 0 && a.Restore == 0 {
		return badReq("send spend or restore")
	}
	if a.Spend > 0 && a.Restore > 0 {
		return badReq("send spend or restore, not both")
	}
	delta := a.Spend - a.Restore // positive means more slots used

	if a.Pact {
		c.PactSlotsUsed += delta
		if c.PactSlotsUsed < 0 {
			c.PactSlotsUsed = 0
		}
		return nil
	}
	if a.Level < 1 || a.Level > 9 {
		return badReq("level must be between 1 and 9 (or set pact to true)")
	}
	used := c.SpellSlotsUsed[a.Level] + delta
	if used <= 0 {
		delete(c.SpellSlotsUsed, a.Level)
		return nil
	}
	if c.SpellSlotsUsed == nil {
		c.SpellSlotsUsed = map[int]int{}
	}
	c.SpellSlotsUsed[a.Level] = used
	return nil
}

// ResourceAction takes exactly one field.
type ResourceAction struct {
	Spend   *int `json:"spend"`
	Restore *int `json:"restore"`
	Set     *int `json:"set"`
}

func ApplyResource(c *Character, key string, a ResourceAction) error {
	if key == "" {
		return badReq("resource id is required")
	}
	fields := 0
	for _, p := range []*int{a.Spend, a.Restore, a.Set} {
		if p != nil {
			fields++
			if *p < 0 {
				return badReq("amounts cannot be negative")
			}
		}
	}
	if fields != 1 {
		return badReq("send exactly one of spend, restore or set")
	}

	cur := c.ResourceUsed[key]
	switch {
	case a.Spend != nil:
		cur += *a.Spend
	case a.Restore != nil:
		cur -= *a.Restore
		if cur < 0 {
			cur = 0
		}
	case a.Set != nil:
		cur = *a.Set
	}
	if cur == 0 {
		delete(c.ResourceUsed, key)
		return nil
	}
	if c.ResourceUsed == nil {
		c.ResourceUsed = map[string]int{}
	}
	c.ResourceUsed[key] = cur
	return nil
}

func restoreResources(c *Character, sh Sheet, match func(RechargeType) bool) {
	for _, r := range sh.Resources {
		if match(r.Recharge) {
			delete(c.ResourceUsed, r.FeatureID)
		}
	}
}

// ApplyRest uses the pre-rest sheet for max HP, level and each resource's recharge.
//
//	short: pact slots and short-rest resources come back.
//	long:  also HP to max, temp HP cleared, all slots back, long-rest and dawn resources back,
//	       half your hit dice back (at least 1), death saves cleared, concentration ends,
//	       exhaustion drops by 1.
//
// Spending hit dice during a short rest is not automated.
func ApplyRest(c *Character, sh Sheet, kind string) error {
	switch kind {
	case "short":
		c.PactSlotsUsed = 0
		restoreResources(c, sh, func(r RechargeType) bool { return r == RechargeShortRest })
	case "long":
		c.CurrentHP = sh.MaxHP
		c.TempHP = 0
		c.SpellSlotsUsed = nil
		c.PactSlotsUsed = 0
		c.DeathSaves = [2]int{}
		c.Concentrating = ""
		if c.Exhaustion > 0 {
			c.Exhaustion--
		}
		recovered := sh.Level / 2
		if recovered < 1 {
			recovered = 1
		}
		c.HitDiceUsed -= recovered
		if c.HitDiceUsed < 0 {
			c.HitDiceUsed = 0
		}
		restoreResources(c, sh, func(r RechargeType) bool {
			return r == RechargeShortRest || r == RechargeLongRest || r == RechargeDawn
		})
	default:
		return badReq(`type must be "short" or "long"`)
	}
	return nil
}

type ConditionsAction struct {
	Add        []Condition `json:"add"`
	Remove     []Condition `json:"remove"`
	Exhaustion *int        `json:"exhaustion"`
}

func ApplyConditions(c *Character, a ConditionsAction) error {
	for _, cond := range a.Add {
		if cond == "" {
			return badReq("a condition name cannot be empty")
		}
	}
	if a.Exhaustion != nil && *a.Exhaustion < 0 {
		return badReq("exhaustion cannot be negative")
	}

	removing := map[Condition]bool{}
	for _, cond := range a.Remove {
		removing[cond] = true
	}
	kept := []Condition{}
	for _, cond := range c.Conditions {
		if !removing[cond] {
			kept = append(kept, cond)
		}
	}
	for _, cond := range a.Add {
		present := false
		for _, k := range kept {
			if k == cond {
				present = true
			}
		}
		if !present {
			kept = append(kept, cond)
		}
	}
	c.Conditions = kept
	if a.Exhaustion != nil {
		c.Exhaustion = *a.Exhaustion
	}
	return nil
}

// ---------------------------------------------------------------------------
// Selections and acknowledgements
// ---------------------------------------------------------------------------

// UpsertSelection sets or replaces the selection for a feature and choice.
func UpsertSelection(c *Character, s Selection) error {
	if s.FeatureID == "" || s.ChoiceID == "" {
		return badReq("feature_id and choice_id are required")
	}
	for i := range c.Selections {
		if c.Selections[i].FeatureID == s.FeatureID && c.Selections[i].ChoiceID == s.ChoiceID {
			c.Selections[i] = s
			return nil
		}
	}
	c.Selections = append(c.Selections, s)
	return nil
}

func RemoveSelection(c *Character, featureID, choiceID string) bool {
	out := make([]Selection, 0, len(c.Selections))
	removed := false
	for _, s := range c.Selections {
		if s.FeatureID == featureID && s.ChoiceID == choiceID {
			removed = true
			continue
		}
		out = append(out, s)
	}
	c.Selections = out
	return removed
}

func UpsertAcknowledgement(c *Character, key, note string, now time.Time) error {
	if key == "" {
		return badReq("key is required")
	}
	for i := range c.Acknowledgements {
		if c.Acknowledgements[i].Key == key {
			c.Acknowledgements[i].Note = note
			c.Acknowledgements[i].At = now
			return nil
		}
	}
	c.Acknowledgements = append(c.Acknowledgements, Acknowledgement{Key: key, Note: note, At: now})
	return nil
}

func RemoveAcknowledgement(c *Character, key string) bool {
	out := make([]Acknowledgement, 0, len(c.Acknowledgements))
	removed := false
	for _, a := range c.Acknowledgements {
		if a.Key == key {
			removed = true
			continue
		}
		out = append(out, a)
	}
	c.Acknowledgements = out
	return removed
}

// ---------------------------------------------------------------------------
// Level up
// ---------------------------------------------------------------------------

type HPChoice struct {
	Method string `json:"method"` // average (default), roll, max, manual
	Value  int    `json:"value"`  // for manual
}

type LevelUpRequest struct {
	ClassID string   `json:"class_id"`
	HP      HPChoice `json:"hp"`
}

// LevelUp adds a level in the given class (adding the class at level 1 if the character doesn't have
// it) and records the hit points for that level. A character's very first level always gets the
// maximum die unless the method is "manual". Current HP is not changed; heal or set it afterwards.
// Levels above the class maximum are allowed; the sheet warns.
func LevelUp(c *Character, lib Library, req LevelUpRequest, roll func(sides int) int) error {
	if req.ClassID == "" {
		return badReq("class_id is required")
	}
	class, err := lib.Class(req.ClassID)
	if err != nil {
		return badReq("class %q was not found in the library", req.ClassID)
	}

	total := 0
	for _, cl := range c.Classes {
		total += cl.Level
	}

	method := req.HP.Method
	if method == "" {
		method = "average"
	}
	hp := 0
	switch method {
	case "manual":
		if req.HP.Value < 0 {
			return badReq("hp value cannot be negative")
		}
		hp = req.HP.Value
	case "average", "max", "roll":
		if class.HitDie < 1 {
			return badReq("class %q has no hit die; use method \"manual\"", req.ClassID)
		}
		switch {
		case total == 0 || method == "max":
			hp = class.HitDie
		case method == "average":
			hp = class.HitDie/2 + 1
		default:
			if roll == nil {
				return badReq("rolling is not available")
			}
			hp = roll(class.HitDie)
		}
	default:
		return badReq("unknown hp method %q (average, roll, max, manual)", method)
	}

	idx := -1
	for i := range c.Classes {
		if c.Classes[i].ClassID == req.ClassID {
			idx = i
			break
		}
	}
	if idx < 0 {
		c.Classes = append(c.Classes, ClassLevel{ClassID: req.ClassID})
		idx = len(c.Classes) - 1
	}
	c.Classes[idx].Level++
	c.Classes[idx].RolledHP = append(c.Classes[idx].RolledHP, hp)
	return nil
}

// ---------------------------------------------------------------------------
// Inventory
// ---------------------------------------------------------------------------

func entryIndex(c *Character, id string) int {
	if id == "" {
		return -1
	}
	for i := range c.Inventory {
		if c.Inventory[i].ID == id {
			return i
		}
	}
	return -1
}

// ensureEntryIDs gives every entry an id, so older data can be addressed too.
func ensureEntryIDs(c *Character) {
	for i := range c.Inventory {
		if c.Inventory[i].ID == "" {
			c.Inventory[i].ID = NewCharacterID()
		}
	}
}

// wouldCycle reports whether putting entry i inside the entry named containerID would create a loop.
func wouldCycle(c *Character, i int, containerID string) bool {
	cur := entryIndex(c, containerID)
	for steps := 0; cur != -1 && steps <= len(c.Inventory); steps++ {
		if cur == i {
			return true
		}
		cur = entryIndex(c, c.Inventory[cur].Container)
	}
	return false
}

type AddItemRequest struct {
	ItemID    string `json:"item_id"`
	Custom    *Item  `json:"custom"`
	Quantity  int    `json:"quantity"` // 0 means 1
	Container string `json:"container"`
	NewStack  bool   `json:"new_stack"` // don't merge into an existing stack
	Notes     string `json:"notes"`
}

// AddItem adds an item by library id or as an inline custom item, merging into an existing plain stack
// of the same item in the same container unless NewStack is set. It returns the entry id.
// Unlike editing a character directly, adding insists on a known item id: it is an explicit choice of
// an item, so a typo is better caught here.
func AddItem(c *Character, lib Library, req AddItemRequest) (string, error) {
	if (req.ItemID == "") == (req.Custom == nil) {
		return "", badReq("send exactly one of item_id or custom")
	}
	qty := req.Quantity
	if qty == 0 {
		qty = 1
	}
	if qty < 0 {
		return "", badReq("quantity cannot be negative")
	}
	if req.ItemID != "" {
		if _, err := lib.Item(req.ItemID); err != nil {
			return "", badReq("item %q was not found in the library", req.ItemID)
		}
	}
	ensureEntryIDs(c)
	if req.Container != "" && entryIndex(c, req.Container) < 0 {
		return "", badReq("container %q is not one of this character's inventory entries", req.Container)
	}

	if req.ItemID != "" && !req.NewStack && req.Notes == "" {
		for i := range c.Inventory {
			e := &c.Inventory[i]
			if e.ItemID == req.ItemID && e.Custom == nil && e.Container == req.Container &&
				!e.Equipped && !e.Attuned && !e.Stashed && e.Notes == "" && e.AbilityOverride == "" {
				e.Quantity += qty
				return e.ID, nil
			}
		}
	}

	e := InventoryItem{ID: NewCharacterID(), ItemID: req.ItemID, Quantity: qty, Container: req.Container, Notes: req.Notes}
	if req.Custom != nil {
		cp := *req.Custom
		e.Custom = &cp
	}
	c.Inventory = append(c.Inventory, e)
	return e.ID, nil
}

// PatchEntry edits an entry's state. Only equipped, attuned, stashed, proficient, container, notes,
// ability_override and custom can be changed; null clears a field. custom is a merge patch onto a
// custom entry's inline item, for fixing its name, weight, cost and so on.
func PatchEntry(c *Character, entryID string, patch map[string]json.RawMessage) error {
	ensureEntryIDs(c)
	i := entryIndex(c, entryID)
	if i < 0 {
		return notFound("inventory entry %q not found", entryID)
	}

	allowed := map[string]bool{
		"equipped": true, "attuned": true, "stashed": true, "proficient": true, "container": true, "notes": true,
		"ability_override": true, "custom": true,
	}
	var unknown []string
	for k := range patch {
		if !allowed[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return badReq("these fields can't be patched: %s", strings.Join(unknown, ", "))
	}

	// Work on a copy so a bad field leaves the entry untouched.
	e := c.Inventory[i]
	for _, k := range sortedKeys(patch) {
		raw := patch[k]
		null := bytes.Equal(bytes.TrimSpace(raw), []byte("null"))

		if k == "custom" {
			it, err := patchCustomItem(e.Custom, raw)
			if err != nil {
				return err
			}
			e.Custom = it
			continue
		}

		var b bool
		var s string
		var err error
		switch k {
		case "equipped", "attuned", "stashed", "proficient":
			if !null {
				err = json.Unmarshal(raw, &b)
			}
		default:
			if !null {
				err = json.Unmarshal(raw, &s)
			}
		}
		if err != nil {
			return badReq("%s has the wrong type: %v", k, err)
		}

		switch k {
		case "equipped":
			e.Equipped = b
		case "attuned":
			e.Attuned = b
		case "stashed":
			e.Stashed = b
		case "proficient":
			e.Proficient = b
		case "notes":
			e.Notes = s
		case "ability_override":
			if s != "" && !isAbility(s) {
				return badReq("ability_override %q is not an ability", s)
			}
			e.AbilityOverride = Ability(s)
		case "container":
			if s != "" {
				if entryIndex(c, s) < 0 {
					return badReq("container %q is not one of this character's inventory entries", s)
				}
				if wouldCycle(c, i, s) {
					return badReq("an entry can't be stored inside itself or something inside it")
				}
			}
			e.Container = s
		}
	}
	c.Inventory[i] = e
	return nil
}

// patchCustomItem applies a JSON merge patch to a custom entry's inline item and returns the new copy.
func patchCustomItem(cur *Item, raw json.RawMessage) (*Item, error) {
	if cur == nil {
		return nil, badReq("custom can only be patched on a custom entry; this entry is a library item")
	}
	var p interface{}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, badReq("custom has the wrong type: %v", err)
	}
	if _, ok := p.(map[string]interface{}); !ok {
		return nil, badReq("custom must be an object of item fields to change (it can't be cleared)")
	}
	b, err := json.Marshal(cur)
	if err != nil {
		return nil, err
	}
	var target interface{}
	if err := json.Unmarshal(b, &target); err != nil {
		return nil, err
	}
	merged, err := json.Marshal(mergePatch(target, p))
	if err != nil {
		return nil, err
	}
	var out Item
	dec := json.NewDecoder(bytes.NewReader(merged))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return nil, badReq("custom: %v", err)
	}
	return &out, nil
}

// ChangeQuantity adds delta (negative to use some up). An entry that reaches 0 is removed, and its
// contents move to the top level.
func ChangeQuantity(c *Character, entryID string, delta int) (removed bool, err error) {
	if delta == 0 {
		return false, badReq("delta cannot be 0")
	}
	ensureEntryIDs(c)
	i := entryIndex(c, entryID)
	if i < 0 {
		return false, notFound("inventory entry %q not found", entryID)
	}
	if q := c.Inventory[i].Quantity + delta; q > 0 {
		c.Inventory[i].Quantity = q
		return false, nil
	}
	removeEntryAt(c, i, false)
	return true, nil
}

type MoveRequest struct {
	Container string `json:"container"` // "" moves to the top level
	Quantity  *int   `json:"quantity"`  // omitted or too large moves the whole stack
}

// MoveEntry moves some or all of a stack into a container (or out of one) and returns the id of the
// entry that was moved, which is a new entry when only part of a stack moved.
func MoveEntry(c *Character, entryID string, req MoveRequest) (string, error) {
	ensureEntryIDs(c)
	i := entryIndex(c, entryID)
	if i < 0 {
		return "", notFound("inventory entry %q not found", entryID)
	}
	if req.Container != "" {
		if entryIndex(c, req.Container) < 0 {
			return "", badReq("container %q is not one of this character's inventory entries", req.Container)
		}
		if wouldCycle(c, i, req.Container) {
			return "", badReq("an entry can't be stored inside itself or something inside it")
		}
	}
	if req.Quantity != nil && *req.Quantity <= 0 {
		return "", badReq("quantity must be positive")
	}

	e := c.Inventory[i]
	if req.Quantity == nil || *req.Quantity >= e.Quantity {
		c.Inventory[i].Container = req.Container
		return e.ID, nil
	}

	c.Inventory[i].Quantity -= *req.Quantity
	moved := InventoryItem{ID: NewCharacterID(), ItemID: e.ItemID, Quantity: *req.Quantity, Container: req.Container, Notes: e.Notes}
	if e.Custom != nil {
		cp := *e.Custom
		moved.Custom = &cp
	}
	c.Inventory = append(c.Inventory, moved)
	return moved.ID, nil
}

// RemoveEntry deletes an entry. Anything stored inside it moves to the top level, unless
// deleteContents is set, which removes it too.
func RemoveEntry(c *Character, entryID string, deleteContents bool) error {
	ensureEntryIDs(c)
	i := entryIndex(c, entryID)
	if i < 0 {
		return notFound("inventory entry %q not found", entryID)
	}
	removeEntryAt(c, i, deleteContents)
	return nil
}

func removeEntryAt(c *Character, i int, deleteContents bool) {
	id := c.Inventory[i].ID
	drop := map[string]bool{id: true}
	if deleteContents {
		for changed := true; changed; {
			changed = false
			for _, e := range c.Inventory {
				if e.Container != "" && drop[e.Container] && !drop[e.ID] {
					drop[e.ID] = true
					changed = true
				}
			}
		}
	}
	out := make([]InventoryItem, 0, len(c.Inventory))
	for _, e := range c.Inventory {
		if drop[e.ID] {
			continue
		}
		if !deleteContents && e.Container == id {
			e.Container = ""
		}
		out = append(out, e)
	}
	c.Inventory = out
}

// ---------------------------------------------------------------------------
// Coins
// ---------------------------------------------------------------------------

type ConvertAction struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Amount int    `json:"amount"`
}

// CoinsAction takes exactly one of gain, spend, pay or convert.
type CoinsAction struct {
	Gain    map[string]int `json:"gain"`
	Spend   map[string]int `json:"spend"`
	Pay     map[string]int `json:"pay"` // a price, paid by value from any coins held, with change
	Convert *ConvertAction `json:"convert"`
}

// ApplyCoins checks the currencies exist in the ruleset (a typo here is better caught than banked),
// then gains, spends or converts exactly the coins named, or pays a price from whatever is held.
func ApplyCoins(c *Character, rules Ruleset, a CoinsAction) error {
	given := 0
	if len(a.Gain) > 0 {
		given++
	}
	if len(a.Spend) > 0 {
		given++
	}
	if len(a.Pay) > 0 {
		given++
	}
	if a.Convert != nil {
		given++
	}
	if given != 1 {
		return badReq("send exactly one of gain, spend, pay or convert")
	}

	defs := CurrencyDefs(rules)
	known := map[string]bool{}
	for _, d := range defs {
		known[d.ID] = true
	}
	check := func(coins map[string]int) error {
		for _, id := range sortedKeys(coins) {
			if !known[id] {
				return badReq("unknown currency %q", id)
			}
			if coins[id] < 0 {
				return badReq("amounts cannot be negative")
			}
		}
		return nil
	}

	switch {
	case len(a.Gain) > 0:
		if err := check(a.Gain); err != nil {
			return err
		}
		c.AddCoins(a.Gain)
	case len(a.Spend) > 0:
		if err := check(a.Spend); err != nil {
			return err
		}
		c.SpendCoins(a.Spend)
	case len(a.Pay) > 0:
		if err := check(a.Pay); err != nil {
			return err
		}
		if _, _, err := PayCoins(c, defs, a.Pay); err != nil {
			switch {
			case errors.Is(err, ErrCantAfford):
				return &reqError{Code: "cant_afford", Msg: err.Error()}
			case errors.Is(err, ErrBadConversion):
				return &reqError{Code: "bad_conversion", Msg: err.Error()}
			}
			return err
		}
	default:
		if err := ConvertCoins(c, defs, a.Convert.From, a.Convert.To, a.Convert.Amount); err != nil {
			if errors.Is(err, ErrBadConversion) {
				return &reqError{Code: "bad_conversion", Msg: err.Error()}
			}
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Choice options
// ---------------------------------------------------------------------------

func findChoice(lib Library, featureID, choiceID string) (Choice, error) {
	var choices []Choice
	if strings.HasPrefix(featureID, "class:") && strings.HasSuffix(featureID, ":start") {
		classID := strings.TrimSuffix(strings.TrimPrefix(featureID, "class:"), ":start")
		cl, err := lib.Class(classID)
		if err != nil {
			return Choice{}, notFound("class %q not found", classID)
		}
		choices = cl.StartingChoices
	} else {
		// A repeatable feature's instance id looks like "<id>@<feature>:<choice>": look up the feature itself.
		base := baseFeatureID(featureID)
		f, err := lib.Feature(base)
		if err != nil {
			return Choice{}, notFound("feature %q not found", base)
		}
		choices = f.Choices
	}
	for _, ch := range choices {
		if ch.ID == choiceID {
			return ch, nil
		}
	}
	return Choice{}, notFound("%q has no choice %q", featureID, choiceID)
}

// prereqMet checks a prerequisite against a computed sheet. checkable is false when the prerequisite
// has no target the engine understands.
func prereqMet(sh Sheet, p Prerequisite) (met, checkable bool) {
	switch {
	case p.Target == "":
		return true, false
	case p.Target == "level":
		return sh.Level >= p.Min, true
	case p.Target == TargetProficiencyBonus:
		return sh.ProficiencyBonus >= p.Min, true
	case p.Target == TargetAC:
		return sh.AC >= p.Min, true
	case p.Target == TargetMaxHP:
		return sh.MaxHP >= p.Min, true
	case p.Target == TargetInitiative:
		return sh.Initiative >= p.Min, true
	case strings.HasPrefix(p.Target, "ability."):
		a, ok := sh.Abilities[Ability(strings.TrimPrefix(p.Target, "ability."))]
		if !ok {
			return true, false
		}
		return a.Score >= p.Min, true
	case strings.HasPrefix(p.Target, "skill."):
		sk, ok := sh.Skills[strings.TrimPrefix(p.Target, "skill.")]
		if !ok {
			return true, false
		}
		return sk.Bonus >= p.Min, true
	}
	return true, false
}

// ChoiceOptions lists what a choice can be filled with and whether each option suits this character.
// Eligibility is judged as if the choice itself were not yet made, so current picks aren't marked
// "already have it". The API only reports; a frontend decides whether to hide ineligible options.
//
// Explicit options are always eligible. Filter pools: skills are ineligible if already held at the
// granted level; feats need to come from a library that can list its features, must not already be
// held, and must meet their prerequisites. Tool, language and feature_tag pools can only offer their
// allowed list.
func ChoiceOptions(c Character, lib Library, rules Ruleset, featureID, choiceID string) ([]EligibleOption, error) {
	choice, err := findChoice(lib, featureID, choiceID)
	if err != nil {
		return nil, err
	}

	without := c
	without.Selections = nil
	held := map[string]bool{}
	for _, id := range c.FeatureIDs {
		held[id] = true
	}
	for _, s := range c.Selections {
		if s.FeatureID == featureID && s.ChoiceID == choiceID {
			continue
		}
		without.Selections = append(without.Selections, s)
		for _, id := range s.Picked {
			held[id] = true
		}
	}
	sh, err := Compute(without, lib, rules)
	if err != nil {
		return nil, err
	}

	out := []EligibleOption{}
	switch {
	case len(choice.Options) > 0:
		for _, o := range choice.Options {
			out = append(out, EligibleOption{OptionID: o.ID, Name: o.Name, Eligible: true})
		}
	case choice.Filter != nil:
		f := choice.Filter
		switch f.Type {
		case "skill":
			level := choice.GrantLevel
			if rank(level) == 0 {
				level = ProfTrained
			}
			names := map[string]string{}
			ids := []string{}
			for _, sd := range lib.Skills() {
				names[sd.ID] = sd.Name
				ids = append(ids, sd.ID)
			}
			if len(f.Allowed) > 0 {
				ids = f.Allowed
			}
			for _, id := range ids {
				name := names[id]
				if name == "" {
					name = id
				}
				o := EligibleOption{OptionID: id, Name: name, Eligible: true}
				if sk, ok := sh.Skills[id]; ok && rank(sk.Level) >= rank(level) {
					o.Eligible, o.Reason = false, fmt.Sprintf("already %s", sk.Level)
				}
				out = append(out, o)
			}
		case "feat", "feature_tag":
			lister, ok := lib.(interface{ AllFeatures() []Feature })
			if !ok {
				break
			}
			allowed := map[string]bool{}
			for _, id := range f.Allowed {
				allowed[id] = true
			}
			// A feat pool is every feature with origin "feat" (or just the allowed list). A feature_tag pool
			// is every feature carrying the tag, so homebrew adds an option by tagging a feature.
			include := func(feat Feature) bool {
				if len(allowed) > 0 && !allowed[feat.ID] {
					return false
				}
				if f.Type == "feature_tag" {
					if f.Tag != "" {
						return contains(feat.Tags, f.Tag)
					}
					return len(allowed) > 0
				}
				return len(allowed) > 0 || feat.Origin == OriginFeat
			}
			for _, feat := range lister.AllFeatures() {
				if !include(feat) {
					continue
				}
				o := EligibleOption{OptionID: feat.ID, Name: feat.Name, Eligible: true}
				if held[feat.ID] && !feat.Repeatable {
					o.Eligible, o.Reason = false, "already taken"
				} else {
					for _, p := range feat.Prerequisites {
						if met, checkable := prereqMet(sh, p); checkable && !met {
							o.Eligible = false
							o.Reason = p.Description
							if o.Reason == "" {
								o.Reason = fmt.Sprintf("%s %d required", p.Target, p.Min)
							}
							break
						}
					}
				}
				out = append(out, o)
			}
			sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		default:
			for _, id := range f.Allowed {
				out = append(out, EligibleOption{OptionID: id, Name: id, Eligible: true})
			}
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Merge patch
// ---------------------------------------------------------------------------

var ErrBadPatch = errors.New("invalid patch")

// mergePatch is RFC 7386: objects merge, null deletes a key, everything else replaces.
func mergePatch(target, patch interface{}) interface{} {
	pm, ok := patch.(map[string]interface{})
	if !ok {
		return patch
	}
	tm, ok := target.(map[string]interface{})
	if !ok {
		tm = map[string]interface{}{}
	}
	for k, v := range pm {
		if v == nil {
			delete(tm, k)
			continue
		}
		tm[k] = mergePatch(tm[k], v)
	}
	return tm
}

// MergePatchCharacter applies a JSON merge patch to a character. The result must still be a valid
// character (unknown fields are rejected, so a typo can't silently do nothing) and keep its id.
func MergePatchCharacter(current Character, patch []byte) (Character, error) {
	var p interface{}
	if err := json.Unmarshal(patch, &p); err != nil {
		return Character{}, fmt.Errorf("%w: %v", ErrBadPatch, err)
	}
	if _, ok := p.(map[string]interface{}); !ok {
		return Character{}, fmt.Errorf("%w: the patch must be a JSON object", ErrBadPatch)
	}

	cur, err := json.Marshal(current)
	if err != nil {
		return Character{}, err
	}
	var target interface{}
	if err := json.Unmarshal(cur, &target); err != nil {
		return Character{}, err
	}
	merged, err := json.Marshal(mergePatch(target, p))
	if err != nil {
		return Character{}, err
	}

	var out Character
	dec := json.NewDecoder(bytes.NewReader(merged))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return Character{}, fmt.Errorf("%w: %v", ErrBadPatch, err)
	}
	if out.ID != current.ID {
		return Character{}, fmt.Errorf("%w: a character's id cannot be changed", ErrBadPatch)
	}
	return out, nil
}
