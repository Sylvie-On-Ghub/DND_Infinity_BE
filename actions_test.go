package character

// actions_test.go - tests for actions.go. Written without being able to run them: a failure may be
// a test mistake as much as a code bug.

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func ip(n int) *int { return &n }

func isBadRequest(err error) bool {
	var re *reqError
	return errors.As(err, &re) && re.Code == "bad_request"
}

func isNotFoundErr(err error) bool {
	var re *reqError
	return errors.As(err, &re) && re.Code == "not_found"
}

// ---------------------------------------------------------------------------
// Hit points
// ---------------------------------------------------------------------------

func TestApplyHPDamage(t *testing.T) {
	c := Character{CurrentHP: 10, TempHP: 5}
	if err := ApplyHP(&c, 20, HPAction{Damage: ip(8)}); err != nil {
		t.Fatal(err)
	}
	if c.TempHP != 0 || c.CurrentHP != 7 { // temp HP soaks up 5, the other 3 hits real HP
		t.Errorf("after 8 damage: temp %d, hp %d; want 0 and 7", c.TempHP, c.CurrentHP)
	}
	if err := ApplyHP(&c, 20, HPAction{Damage: ip(50)}); err != nil {
		t.Fatal(err)
	}
	if c.CurrentHP != 0 {
		t.Errorf("hp = %d, want it to stop at 0", c.CurrentHP)
	}
}

func TestApplyHPHeal(t *testing.T) {
	c := Character{CurrentHP: 10}
	ApplyHP(&c, 12, HPAction{Heal: ip(5)})
	if c.CurrentHP != 12 {
		t.Errorf("hp = %d, want 12 (capped at max)", c.CurrentHP)
	}

	over := Character{CurrentHP: 15}
	ApplyHP(&over, 12, HPAction{Heal: ip(3)})
	if over.CurrentHP != 15 {
		t.Errorf("hp = %d, want 15: healing never lowers HP that is already above max", over.CurrentHP)
	}

	down := Character{CurrentHP: 0, DeathSaves: [2]int{1, 2}}
	ApplyHP(&down, 12, HPAction{Heal: ip(4)})
	if down.CurrentHP != 4 || down.DeathSaves != [2]int{} {
		t.Errorf("healing from 0: hp %d, death saves %v; want 4 and cleared", down.CurrentHP, down.DeathSaves)
	}
}

func TestApplyHPTempAndSet(t *testing.T) {
	c := Character{CurrentHP: 5, TempHP: 3}
	ApplyHP(&c, 20, HPAction{Temp: ip(8)})
	if c.TempHP != 8 {
		t.Errorf("temp = %d, want 8 (it replaces)", c.TempHP)
	}
	ApplyHP(&c, 20, HPAction{Set: ip(99)})
	if c.CurrentHP != 99 {
		t.Errorf("hp = %d, want 99 (set bypasses the maximum)", c.CurrentHP)
	}
}

func TestApplyHPRejectsBadRequests(t *testing.T) {
	cases := map[string]HPAction{
		"nothing":  {},
		"two":      {Damage: ip(1), Heal: ip(1)},
		"negative": {Damage: ip(-1)},
	}
	for name, a := range cases {
		c := Character{CurrentHP: 5}
		if err := ApplyHP(&c, 10, a); !isBadRequest(err) {
			t.Errorf("%s: err = %v, want a bad request", name, err)
		}
		if c.CurrentHP != 5 {
			t.Errorf("%s: a rejected request changed hp to %d", name, c.CurrentHP)
		}
	}
}

// ---------------------------------------------------------------------------
// Slots, resources, rests, conditions
// ---------------------------------------------------------------------------

func TestApplySlots(t *testing.T) {
	var c Character
	if err := ApplySlots(&c, SlotAction{Level: 3, Spend: 2}); err != nil {
		t.Fatal(err)
	}
	if c.SpellSlotsUsed[3] != 2 {
		t.Errorf("used = %v, want 2 at level 3", c.SpellSlotsUsed)
	}
	ApplySlots(&c, SlotAction{Level: 3, Restore: 5}) // more than were used: stops at 0
	if _, present := c.SpellSlotsUsed[3]; present {
		t.Errorf("used = %v, want the level 3 entry gone", c.SpellSlotsUsed)
	}

	ApplySlots(&c, SlotAction{Pact: true, Spend: 1})
	if c.PactSlotsUsed != 1 {
		t.Errorf("pact used = %d, want 1", c.PactSlotsUsed)
	}
	ApplySlots(&c, SlotAction{Pact: true, Restore: 3})
	if c.PactSlotsUsed != 0 {
		t.Errorf("pact used = %d, want 0", c.PactSlotsUsed)
	}
}

func TestApplySlotsRejectsBadRequests(t *testing.T) {
	cases := map[string]SlotAction{
		"neither":    {Level: 1},
		"both":       {Level: 1, Spend: 1, Restore: 1},
		"level zero": {Level: 0, Spend: 1},
		"level ten":  {Level: 10, Spend: 1},
		"negative":   {Level: 1, Spend: -1},
	}
	for name, a := range cases {
		var c Character
		if err := ApplySlots(&c, a); !isBadRequest(err) {
			t.Errorf("%s: err = %v, want a bad request", name, err)
		}
	}
}

func TestApplyResource(t *testing.T) {
	var c Character
	ApplyResource(&c, "rage", ResourceAction{Spend: ip(1)})
	ApplyResource(&c, "rage", ResourceAction{Spend: ip(1)})
	if c.ResourceUsed["rage"] != 2 {
		t.Errorf("used = %v, want 2", c.ResourceUsed)
	}
	ApplyResource(&c, "rage", ResourceAction{Restore: ip(1)})
	if c.ResourceUsed["rage"] != 1 {
		t.Errorf("used = %v, want 1", c.ResourceUsed)
	}
	ApplyResource(&c, "rage", ResourceAction{Set: ip(0)})
	if _, present := c.ResourceUsed["rage"]; present {
		t.Errorf("used = %v, want the entry gone", c.ResourceUsed)
	}
	if err := ApplyResource(&c, "rage", ResourceAction{Spend: ip(1), Set: ip(2)}); !isBadRequest(err) {
		t.Errorf("two fields: err = %v, want a bad request", err)
	}
	if err := ApplyResource(&c, "", ResourceAction{Spend: ip(1)}); !isBadRequest(err) {
		t.Errorf("empty key: err = %v, want a bad request", err)
	}
}

func restSheet(level int) Sheet {
	return Sheet{
		MaxHP: 30, Level: level,
		Resources: []ResourceView{
			{FeatureID: "rage", Recharge: RechargeLongRest},
			{FeatureID: "second_wind", Recharge: RechargeShortRest},
			{FeatureID: "dawn_thing", Recharge: RechargeDawn},
			{FeatureID: "once_only", Recharge: RechargeNone},
		},
	}
}

func restChar() Character {
	return Character{
		CurrentHP: 4, TempHP: 6, HitDiceUsed: 5, Exhaustion: 2, Concentrating: "bless",
		DeathSaves:     [2]int{2, 1},
		SpellSlotsUsed: map[int]int{1: 2},
		PactSlotsUsed:  1,
		ResourceUsed:   map[string]int{"rage": 2, "second_wind": 1, "dawn_thing": 1, "once_only": 1},
	}
}

func TestShortRest(t *testing.T) {
	c := restChar()
	if err := ApplyRest(&c, restSheet(6), "short"); err != nil {
		t.Fatal(err)
	}
	if c.PactSlotsUsed != 0 {
		t.Error("pact slots should come back on a short rest")
	}
	if _, used := c.ResourceUsed["second_wind"]; used {
		t.Error("short-rest resources should be restored")
	}
	if c.ResourceUsed["rage"] != 2 || c.CurrentHP != 4 || c.SpellSlotsUsed[1] != 2 {
		t.Errorf("a short rest must not restore long-rest things: %+v", c)
	}
}

func TestLongRest(t *testing.T) {
	c := restChar()
	if err := ApplyRest(&c, restSheet(6), "long"); err != nil {
		t.Fatal(err)
	}
	if c.CurrentHP != 30 || c.TempHP != 0 {
		t.Errorf("hp %d, temp %d; want 30 and 0", c.CurrentHP, c.TempHP)
	}
	if c.SpellSlotsUsed != nil || c.PactSlotsUsed != 0 {
		t.Errorf("slots should all be back: %v, pact %d", c.SpellSlotsUsed, c.PactSlotsUsed)
	}
	for _, id := range []string{"rage", "second_wind", "dawn_thing"} {
		if _, used := c.ResourceUsed[id]; used {
			t.Errorf("%s should be restored by a long rest", id)
		}
	}
	if c.ResourceUsed["once_only"] != 1 {
		t.Error("a resource that never recharges stays spent")
	}
	if c.HitDiceUsed != 2 { // 5 used, half of 6 levels = 3 back
		t.Errorf("hit dice used = %d, want 2", c.HitDiceUsed)
	}
	if c.Exhaustion != 1 || c.Concentrating != "" || c.DeathSaves != [2]int{} {
		t.Errorf("exhaustion %d, concentrating %q, death saves %v; want 1, empty, cleared", c.Exhaustion, c.Concentrating, c.DeathSaves)
	}
}

func TestLongRestRecoversAtLeastOneHitDie(t *testing.T) {
	c := Character{HitDiceUsed: 1}
	ApplyRest(&c, Sheet{Level: 1, MaxHP: 8}, "long") // half of 1 rounds down to 0, but at least 1 comes back
	if c.HitDiceUsed != 0 {
		t.Errorf("hit dice used = %d, want 0", c.HitDiceUsed)
	}
}

func TestRestRejectsUnknownType(t *testing.T) {
	var c Character
	if err := ApplyRest(&c, Sheet{}, "nap"); !isBadRequest(err) {
		t.Errorf("err = %v, want a bad request", err)
	}
}

func TestApplyConditions(t *testing.T) {
	c := Character{Conditions: []Condition{"blinded", "prone"}}
	err := ApplyConditions(&c, ConditionsAction{Add: []Condition{"poisoned", "prone"}, Remove: []Condition{"blinded"}, Exhaustion: ip(3)})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Conditions) != 2 || c.Conditions[0] != "prone" || c.Conditions[1] != "poisoned" || c.Exhaustion != 3 {
		t.Errorf("conditions %v, exhaustion %d; want prone, poisoned and 3 (no duplicates)", c.Conditions, c.Exhaustion)
	}
	if err := ApplyConditions(&c, ConditionsAction{Add: []Condition{""}}); !isBadRequest(err) {
		t.Errorf("empty condition: err = %v, want a bad request", err)
	}
}

// ---------------------------------------------------------------------------
// Selections and acknowledgements
// ---------------------------------------------------------------------------

func TestSelectionsUpsertAndRemove(t *testing.T) {
	var c Character
	UpsertSelection(&c, Selection{FeatureID: "f", ChoiceID: "x", Picked: []string{"a"}})
	UpsertSelection(&c, Selection{FeatureID: "f", ChoiceID: "y", Picked: []string{"b"}})
	UpsertSelection(&c, Selection{FeatureID: "f", ChoiceID: "x", Picked: []string{"c"}}) // replaces
	if len(c.Selections) != 2 || c.Selections[0].Picked[0] != "c" {
		t.Errorf("selections = %+v, want x replaced and y kept", c.Selections)
	}
	if !RemoveSelection(&c, "f", "x") || len(c.Selections) != 1 {
		t.Errorf("remove failed: %+v", c.Selections)
	}
	if RemoveSelection(&c, "f", "nope") {
		t.Error("removing something absent should report false")
	}
	if err := UpsertSelection(&c, Selection{ChoiceID: "x"}); !isBadRequest(err) {
		t.Errorf("missing feature id: err = %v, want a bad request", err)
	}
}

func TestAcknowledgementsUpsertAndRemove(t *testing.T) {
	var c Character
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	UpsertAcknowledgement(&c, "k", "first", now)
	UpsertAcknowledgement(&c, "k", "second", now.Add(time.Hour))
	if len(c.Acknowledgements) != 1 || c.Acknowledgements[0].Note != "second" || !c.Acknowledgements[0].At.Equal(now.Add(time.Hour)) {
		t.Errorf("acknowledgements = %+v, want one updated entry", c.Acknowledgements)
	}
	if !RemoveAcknowledgement(&c, "k") || len(c.Acknowledgements) != 0 {
		t.Errorf("remove failed: %+v", c.Acknowledgements)
	}
	if err := UpsertAcknowledgement(&c, "", "x", now); !isBadRequest(err) {
		t.Errorf("empty key: err = %v, want a bad request", err)
	}
}

// ---------------------------------------------------------------------------
// Level up
// ---------------------------------------------------------------------------

func TestLevelUp(t *testing.T) {
	lib := rogueLib() // rogue d8, wizard d6
	rollThree := func(sides int) int { return 3 }

	var c Character
	// The very first level is always the maximum die, whatever the method.
	if err := LevelUp(&c, lib, LevelUpRequest{ClassID: "rogue"}, rollThree); err != nil {
		t.Fatal(err)
	}
	if len(c.Classes) != 1 || c.Classes[0].Level != 1 || c.Classes[0].RolledHP[0] != 8 {
		t.Fatalf("classes = %+v, want rogue 1 with 8 hp", c.Classes)
	}

	steps := []struct {
		req  LevelUpRequest
		want int
	}{
		{LevelUpRequest{ClassID: "rogue"}, 5},                                           // average of a d8
		{LevelUpRequest{ClassID: "rogue", HP: HPChoice{Method: "roll"}}, 3},             // the roll
		{LevelUpRequest{ClassID: "rogue", HP: HPChoice{Method: "max"}}, 8},              // the die
		{LevelUpRequest{ClassID: "rogue", HP: HPChoice{Method: "manual", Value: 7}}, 7}, // as given
	}
	for _, s := range steps {
		if err := LevelUp(&c, lib, s.req, rollThree); err != nil {
			t.Fatal(err)
		}
		hp := c.Classes[0].RolledHP
		if got := hp[len(hp)-1]; got != s.want {
			t.Errorf("method %q: hp %d, want %d", s.req.HP.Method, got, s.want)
		}
	}
	if c.Classes[0].Level != 5 || len(c.Classes[0].RolledHP) != 5 {
		t.Errorf("rogue = %+v, want level 5 with 5 hp entries", c.Classes[0])
	}

	// A new class starts at level 1 and is not the character's first level, so it gets the average.
	if err := LevelUp(&c, lib, LevelUpRequest{ClassID: "wizard"}, rollThree); err != nil {
		t.Fatal(err)
	}
	if len(c.Classes) != 2 || c.Classes[1].ClassID != "wizard" || c.Classes[1].RolledHP[0] != 4 { // d6 average
		t.Errorf("classes = %+v, want a wizard at level 1 with 4 hp", c.Classes)
	}
}

func TestLevelUpRejectsBadRequests(t *testing.T) {
	lib := rogueLib()
	cases := map[string]LevelUpRequest{
		"no class":       {},
		"unknown class":  {ClassID: "bard"},
		"unknown method": {ClassID: "rogue", HP: HPChoice{Method: "guess"}},
		"negative":       {ClassID: "rogue", HP: HPChoice{Method: "manual", Value: -1}},
	}
	for name, req := range cases {
		var c Character
		if err := LevelUp(&c, lib, req, nil); !isBadRequest(err) {
			t.Errorf("%s: err = %v, want a bad request", name, err)
		}
		if len(c.Classes) != 0 {
			t.Errorf("%s: a rejected request changed the character: %+v", name, c.Classes)
		}
	}
}

// ---------------------------------------------------------------------------
// Inventory
// ---------------------------------------------------------------------------

func TestAddItemMergesPlainStacks(t *testing.T) {
	lib := gearLib()
	var c Character
	first, err := AddItem(&c, lib, AddItemRequest{ItemID: "rations", Quantity: 2})
	if err != nil {
		t.Fatal(err)
	}
	again, _ := AddItem(&c, lib, AddItemRequest{ItemID: "rations"}) // quantity defaults to 1
	if again != first || len(c.Inventory) != 1 || c.Inventory[0].Quantity != 3 {
		t.Errorf("inventory = %+v, want one stack of 3", c.Inventory)
	}

	separate, _ := AddItem(&c, lib, AddItemRequest{ItemID: "rations", NewStack: true})
	if separate == first || len(c.Inventory) != 2 {
		t.Errorf("new_stack should create a separate entry: %+v", c.Inventory)
	}

	// An equipped stack is no longer "plain", so a new add starts another entry.
	c.Inventory[0].Equipped = true
	third, _ := AddItem(&c, lib, AddItemRequest{ItemID: "rations"})
	if third == first {
		t.Error("a stack with state shouldn't absorb new items")
	}
}

func TestAddItemCustomAndErrors(t *testing.T) {
	lib := gearLib()
	var c Character
	id, err := AddItem(&c, lib, AddItemRequest{Custom: &Item{Name: "Charm", Kind: ItemOther}, Quantity: 1})
	if err != nil || id == "" || c.Inventory[0].Custom == nil || c.Inventory[0].Custom.Name != "Charm" {
		t.Fatalf("custom add: id %q, err %v, inventory %+v", id, err, c.Inventory)
	}

	cases := map[string]AddItemRequest{
		"both":              {ItemID: "rope", Custom: &Item{Name: "x"}},
		"neither":           {},
		"unknown item":      {ItemID: "nope"},
		"negative quantity": {ItemID: "rope", Quantity: -1},
		"missing container": {ItemID: "rope", Container: "nope"},
	}
	for name, req := range cases {
		before := len(c.Inventory)
		if _, err := AddItem(&c, lib, req); !isBadRequest(err) {
			t.Errorf("%s: err = %v, want a bad request", name, err)
		}
		if len(c.Inventory) != before {
			t.Errorf("%s: a rejected add changed the inventory", name)
		}
	}
}

func TestChangeQuantity(t *testing.T) {
	lib := gearLib()
	var c Character
	id, _ := AddItem(&c, lib, AddItemRequest{ItemID: "rations", Quantity: 3})

	removed, err := ChangeQuantity(&c, id, -1)
	if err != nil || removed || c.Inventory[0].Quantity != 2 {
		t.Fatalf("after -1: removed %v, err %v, inventory %+v", removed, err, c.Inventory)
	}
	removed, _ = ChangeQuantity(&c, id, -5)
	if !removed || len(c.Inventory) != 0 {
		t.Errorf("using up the stack should remove it: removed %v, inventory %+v", removed, c.Inventory)
	}
	if _, err := ChangeQuantity(&c, "nope", 1); !isNotFoundErr(err) {
		t.Errorf("unknown entry: err = %v, want not found", err)
	}
	if _, err := ChangeQuantity(&c, id, 0); !isBadRequest(err) {
		t.Errorf("zero delta: err = %v, want a bad request", err)
	}
}

func TestMoveEntry(t *testing.T) {
	lib := gearLib()
	var c Character
	bp, _ := AddItem(&c, lib, AddItemRequest{ItemID: "backpack"})
	rations, _ := AddItem(&c, lib, AddItemRequest{ItemID: "rations", Quantity: 4})

	// Part of a stack: a new entry is created for the moved portion.
	movedID, err := MoveEntry(&c, rations, MoveRequest{Container: bp, Quantity: ip(3)})
	if err != nil {
		t.Fatal(err)
	}
	if movedID == rations || len(c.Inventory) != 3 {
		t.Fatalf("a partial move should create an entry: moved %q, inventory %+v", movedID, c.Inventory)
	}
	orig := c.Inventory[entryIndex(&c, rations)]
	moved := c.Inventory[entryIndex(&c, movedID)]
	if orig.Quantity != 1 || orig.Container != "" || moved.Quantity != 3 || moved.Container != bp {
		t.Errorf("orig %+v, moved %+v; want 1 outside and 3 inside", orig, moved)
	}

	// The whole stack: the same entry moves.
	whole, _ := MoveEntry(&c, rations, MoveRequest{Container: bp})
	if whole != rations || c.Inventory[entryIndex(&c, rations)].Container != bp {
		t.Errorf("a whole-stack move should reuse the entry")
	}

	// And back out to the top level.
	MoveEntry(&c, rations, MoveRequest{Container: ""})
	if c.Inventory[entryIndex(&c, rations)].Container != "" {
		t.Error("an empty container should move the entry to the top level")
	}

	if _, err := MoveEntry(&c, bp, MoveRequest{Container: bp}); !isBadRequest(err) {
		t.Errorf("moving into itself: err = %v, want a bad request", err)
	}
	if _, err := MoveEntry(&c, rations, MoveRequest{Container: "nope"}); !isBadRequest(err) {
		t.Errorf("unknown container: err = %v, want a bad request", err)
	}
	if _, err := MoveEntry(&c, rations, MoveRequest{Quantity: ip(0)}); !isBadRequest(err) {
		t.Errorf("zero quantity: err = %v, want a bad request", err)
	}
}

func TestMoveEntryRejectsLoops(t *testing.T) {
	lib := gearLib()
	var c Character
	outer, _ := AddItem(&c, lib, AddItemRequest{ItemID: "backpack"})
	inner, _ := AddItem(&c, lib, AddItemRequest{ItemID: "holding_bag", Container: outer})
	if _, err := MoveEntry(&c, outer, MoveRequest{Container: inner}); !isBadRequest(err) {
		t.Errorf("putting a bag inside something it holds: err = %v, want a bad request", err)
	}
}

func TestPatchEntry(t *testing.T) {
	lib := gearLib()
	var c Character
	bp, _ := AddItem(&c, lib, AddItemRequest{ItemID: "backpack"})
	rope, _ := AddItem(&c, lib, AddItemRequest{ItemID: "rope"})

	patch := func(s string) map[string]json.RawMessage {
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	if err := PatchEntry(&c, rope, patch(`{"equipped":true,"notes":"frayed","container":"`+bp+`"}`)); err != nil {
		t.Fatal(err)
	}
	e := c.Inventory[entryIndex(&c, rope)]
	if !e.Equipped || e.Notes != "frayed" || e.Container != bp {
		t.Errorf("entry = %+v, want equipped, noted and inside the backpack", e)
	}
	PatchEntry(&c, rope, patch(`{"container":null,"notes":null}`))
	e = c.Inventory[entryIndex(&c, rope)]
	if e.Container != "" || e.Notes != "" {
		t.Errorf("null should clear fields: %+v", e)
	}

	before := c.Inventory[entryIndex(&c, rope)]
	bad := map[string]string{
		"forbidden field":   `{"quantity":5}`,
		"wrong type":        `{"equipped":"yes"}`,
		"unknown container": `{"container":"nope"}`,
		"bad ability":       `{"ability_override":"luck"}`,
		"self container":    `{"container":"` + rope + `"}`,
	}
	for name, body := range bad {
		if err := PatchEntry(&c, rope, patch(body)); !isBadRequest(err) {
			t.Errorf("%s: err = %v, want a bad request", name, err)
		}
	}
	if after := c.Inventory[entryIndex(&c, rope)]; after != before {
		t.Errorf("rejected patches changed the entry: %+v -> %+v", before, after)
	}
	if err := PatchEntry(&c, "nope", patch(`{"notes":"x"}`)); !isNotFoundErr(err) {
		t.Errorf("unknown entry: err = %v, want not found", err)
	}
}

func TestPatchCustomEntry(t *testing.T) {
	lib := gearLib()
	var c Character
	box, _ := AddItem(&c, lib, AddItemRequest{Custom: &Item{Name: "Silvr Idol", Kind: ItemOther, Weight: 20,
		Cost: &Price{Amount: 5, Currency: "gp"}, Description: "Warm to the touch."}})
	rope, _ := AddItem(&c, lib, AddItemRequest{ItemID: "rope"})

	patch := func(s string) map[string]json.RawMessage {
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	if err := PatchEntry(&c, box, patch(`{"custom":{"name":"Silver Idol","weight":2,"cost":null},"notes":"from the crypt"}`)); err != nil {
		t.Fatal(err)
	}
	it := c.Inventory[entryIndex(&c, box)].Custom
	if it.Name != "Silver Idol" || it.Weight != 2 || it.Cost != nil || it.Description != "Warm to the touch." {
		t.Errorf("custom item = %+v, want the new name and weight, no cost, description kept", it)
	}

	before := *c.Inventory[entryIndex(&c, box)].Custom
	bad := map[string]string{
		"unknown item field": `{"custom":{"wieght":3}}`,
		"wrong item type":    `{"custom":{"weight":"heavy"}}`,
		"clearing custom":    `{"custom":null}`,
		"not an object":      `{"custom":"idol"}`,
	}
	for name, body := range bad {
		if err := PatchEntry(&c, box, patch(body)); !isBadRequest(err) {
			t.Errorf("%s: err = %v, want a bad request", name, err)
		}
	}
	if after := *c.Inventory[entryIndex(&c, box)].Custom; after.Name != before.Name || after.Weight != before.Weight {
		t.Errorf("rejected patches changed the item: %+v -> %+v", before, after)
	}
	if err := PatchEntry(&c, rope, patch(`{"custom":{"weight":1}}`)); !isBadRequest(err) {
		t.Errorf("patching custom on a library item: err = %v, want a bad request", err)
	}
}

func TestRemoveEntryAndContents(t *testing.T) {
	lib := gearLib()
	build := func() (Character, string, string, string) {
		var c Character
		bp, _ := AddItem(&c, lib, AddItemRequest{ItemID: "backpack"})
		rations, _ := AddItem(&c, lib, AddItemRequest{ItemID: "rations", Container: bp})
		bag, _ := AddItem(&c, lib, AddItemRequest{ItemID: "holding_bag", Container: bp})
		rope, _ := AddItem(&c, lib, AddItemRequest{ItemID: "rope", Container: bag})
		_ = rations
		return c, bp, bag, rope
	}

	c, bp, bag, rope := build()
	if err := RemoveEntry(&c, bp, false); err != nil {
		t.Fatal(err)
	}
	if len(c.Inventory) != 3 || c.Inventory[entryIndex(&c, bag)].Container != "" {
		t.Errorf("contents should move to the top level: %+v", c.Inventory)
	}
	if c.Inventory[entryIndex(&c, rope)].Container != bag {
		t.Error("deeper nesting should be preserved")
	}

	c, bp, _, _ = build()
	if err := RemoveEntry(&c, bp, true); err != nil {
		t.Fatal(err)
	}
	if len(c.Inventory) != 0 {
		t.Errorf("deleting with contents should remove everything inside: %+v", c.Inventory)
	}
	if err := RemoveEntry(&c, "nope", false); !isNotFoundErr(err) {
		t.Errorf("unknown entry: err = %v, want not found", err)
	}
}

func TestEntriesWithoutIDsGetThem(t *testing.T) {
	c := Character{Inventory: []InventoryItem{{ItemID: "rope", Quantity: 1}}}
	id, err := AddItem(&c, gearLib(), AddItemRequest{ItemID: "anvil"})
	if err != nil || id == "" {
		t.Fatal(err)
	}
	for _, e := range c.Inventory {
		if e.ID == "" {
			t.Errorf("entry %+v has no id after a mutation", e)
		}
	}
}

// ---------------------------------------------------------------------------
// Coins
// ---------------------------------------------------------------------------

func TestApplyCoins(t *testing.T) {
	var c Character
	rules := Ruleset{}
	if err := ApplyCoins(&c, rules, CoinsAction{Gain: map[string]int{"gp": 10}}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyCoins(&c, rules, CoinsAction{Spend: map[string]int{"gp": 3}}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyCoins(&c, rules, CoinsAction{Convert: &ConvertAction{From: "gp", To: "sp", Amount: 2}}); err != nil {
		t.Fatal(err)
	}
	if c.Coins["gp"] != 5 || c.Coins["sp"] != 20 {
		t.Errorf("coins = %v, want 5 gp and 20 sp", c.Coins)
	}

	// Spending more than held goes negative rather than failing.
	ApplyCoins(&c, rules, CoinsAction{Spend: map[string]int{"cp": 4}})
	if c.Coins["cp"] != -4 {
		t.Errorf("cp = %d, want -4", c.Coins["cp"])
	}
}

func TestApplyCoinsRejectsBadRequests(t *testing.T) {
	cases := map[string]CoinsAction{
		"nothing":          {},
		"two actions":      {Gain: map[string]int{"gp": 1}, Spend: map[string]int{"gp": 1}},
		"unknown currency": {Gain: map[string]int{"zorkmid": 1}},
		"negative":         {Spend: map[string]int{"gp": -1}},
	}
	for name, a := range cases {
		var c Character
		if err := ApplyCoins(&c, Ruleset{}, a); !isBadRequest(err) {
			t.Errorf("%s: err = %v, want a bad request", name, err)
		}
		if len(c.Coins) != 0 {
			t.Errorf("%s: a rejected request changed the coins: %v", name, c.Coins)
		}
	}

	var c Character
	c.AddCoins(map[string]int{"sp": 5})
	err := ApplyCoins(&c, Ruleset{}, CoinsAction{Convert: &ConvertAction{From: "sp", To: "gp", Amount: 5}})
	var re *reqError
	if !errors.As(err, &re) || re.Code != "bad_conversion" {
		t.Errorf("uneven conversion: err = %v, want code bad_conversion", err)
	}
}

// ---------------------------------------------------------------------------
// Choice options
// ---------------------------------------------------------------------------

func TestChoiceOptionsExplicit(t *testing.T) {
	opts, err := ChoiceOptions(styleChar(), stylesLib(), Ruleset{}, "fighting_style", "style")
	if err != nil {
		t.Fatal(err)
	}
	if len(opts) != 2 || !opts[0].Eligible || opts[0].OptionID != "dueling" {
		t.Errorf("options = %+v, want dueling and archery, both eligible", opts)
	}
}

func TestChoiceOptionsSkillsExcludeWhatYouAlreadyHave(t *testing.T) {
	lib := rogueLib() // class rogue: starting skills allowed are stealth and perception
	c := rogueChar(1)
	c.Skills = map[string]ProficiencyLevel{"stealth": ProfTrained}

	opts, err := ChoiceOptions(c, lib, Ruleset{}, "class:rogue:start", "skills")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]EligibleOption{}
	for _, o := range opts {
		byID[o.OptionID] = o
	}
	if len(byID) != 2 || byID["stealth"].Eligible || byID["stealth"].Reason == "" || !byID["perception"].Eligible {
		t.Errorf("options = %+v, want stealth ineligible with a reason and perception eligible", opts)
	}

	// The choice's own picks aren't counted against it, or every current pick would look ineligible.
	c2 := rogueChar(1)
	c2.Selections = []Selection{{FeatureID: "class:rogue:start", ChoiceID: "skills", Picked: []string{"stealth", "perception"}}}
	opts, _ = ChoiceOptions(c2, lib, Ruleset{}, "class:rogue:start", "skills")
	for _, o := range opts {
		if !o.Eligible {
			t.Errorf("%s should be eligible while editing the choice that picked it: %+v", o.OptionID, o)
		}
	}
}

func TestChoiceOptionsFeats(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/heavy.json": `{"id":"heavy","name":"Heavy Hitter","origin":"feat",
			"prerequisites":[{"description":"Strength 13","target":"ability.str","min":13}]}`,
		"features/plain.json": `{"id":"plain","name":"Plain Feat","origin":"feat"}`,
		"features/host.json": `{"id":"host","name":"Host","origin":"class","choices":[
			{"id":"feat","prompt":"Take a feat","kind":"pick","count":1,"filter":{"type":"feat"}}]}`,
	}))

	byID := func(c Character) map[string]EligibleOption {
		opts, err := ChoiceOptions(c, lib, Ruleset{}, "host", "feat")
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]EligibleOption{}
		for _, o := range opts {
			m[o.OptionID] = o
		}
		return m
	}

	weak := newChar(1, map[Ability]int{Str: 10})
	weak.FeatureIDs = []string{"host"}
	got := byID(weak)
	if len(got) != 2 || got["heavy"].Eligible || got["heavy"].Reason != "Strength 13" || !got["plain"].Eligible {
		t.Errorf("weak character: %+v, want heavy blocked by Strength 13 and plain eligible", got)
	}

	strong := newChar(1, map[Ability]int{Str: 14})
	strong.FeatureIDs = []string{"host", "plain"}
	got = byID(strong)
	if !got["heavy"].Eligible || got["plain"].Eligible || got["plain"].Reason != "already taken" {
		t.Errorf("strong character: %+v, want heavy eligible and plain already taken", got)
	}
}

func TestChoiceOptionsFeatureTagPool(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/a.json":    `{"id":"a","name":"Alpha","origin":"class","tags":["trick"]}`,
		"features/b.json":    `{"id":"b","name":"Beta","origin":"class","tags":["trick","other"]}`,
		"features/c.json":    `{"id":"c","name":"Gamma","origin":"class","tags":["other"]}`,
		"features/feat.json": `{"id":"feat1","name":"Some Feat","origin":"feat","tags":["trick"]}`,
		"features/host.json": `{"id":"host","name":"Host","origin":"class","choices":[
			{"id":"tricks","prompt":"Choose tricks","kind":"pick","count":2,"filter":{"type":"feature_tag","tag":"trick"}}]}`,
	}))
	if lib.HasErrors() {
		dumpIssues(t, lib)
		t.Fatal("the pack should load without errors")
	}

	c := newChar(1, nil)
	c.FeatureIDs = []string{"host"}
	opts, err := ChoiceOptions(c, lib, Ruleset{}, "host", "tricks")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, o := range opts {
		ids[o.OptionID] = true
	}
	if len(ids) != 3 || !ids["a"] || !ids["b"] || !ids["feat1"] || ids["c"] {
		t.Errorf("options = %+v, want exactly the features tagged trick (regardless of origin)", opts)
	}

	// Picking from the pool grants the picked features themselves.
	c.Selections = []Selection{{FeatureID: "host", ChoiceID: "tricks", Picked: []string{"a", "b"}}}
	opts, _ = ChoiceOptions(c, lib, Ruleset{}, "host", "tricks")
	for _, o := range opts {
		if !o.Eligible {
			t.Errorf("%s should stay eligible while editing the choice that picked it", o.OptionID)
		}
	}
	sh := mustCompute(t, c, lib, Ruleset{})
	if _, pending := findPending(sh, "host", "tricks"); pending {
		t.Error("two picks of two should leave nothing pending")
	}
}

func TestLoaderRequiresSomethingToPoolBy(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/f.json": `{"id":"f","name":"F","origin":"class","choices":[
			{"id":"c","prompt":"p","kind":"pick","count":1,"filter":{"type":"feature_tag"}}]}`,
	}))
	if !hasIssue(lib, SevError, "needs a tag or an allowed list") {
		t.Errorf("expected an error for an empty feature_tag filter, got %v", lib.Issues())
	}
}

func TestChoiceOptionsUnknownThings(t *testing.T) {
	lib := stylesLib()
	if _, err := ChoiceOptions(styleChar(), lib, Ruleset{}, "nope", "style"); !isNotFoundErr(err) {
		t.Errorf("unknown feature: err = %v, want not found", err)
	}
	if _, err := ChoiceOptions(styleChar(), lib, Ruleset{}, "fighting_style", "nope"); !isNotFoundErr(err) {
		t.Errorf("unknown choice: err = %v, want not found", err)
	}
}

// ---------------------------------------------------------------------------
// Merge patch
// ---------------------------------------------------------------------------

func TestMergePatchCharacter(t *testing.T) {
	current := Character{
		ID: "c1", Name: "Old", Inspiration: 1,
		Spellbooks: map[string]SpellState{"class:wizard": {Cantrips: []string{"fire_bolt"}, Prepared: []string{"fireball"}}},
		Inventory:  []InventoryItem{{ID: "a", ItemID: "rope", Quantity: 1}},
	}
	out, err := MergePatchCharacter(current, []byte(`{
		"inspiration": 2,
		"spellbooks": {"class:wizard": {"prepared": ["shield"]}},
		"inventory": [{"id":"b","item_id":"rations","quantity":3}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if out.Inspiration != 2 || out.Name != "Old" {
		t.Errorf("inspiration %d, name %q; want 2 and the untouched name", out.Inspiration, out.Name)
	}
	sb := out.Spellbooks["class:wizard"]
	if len(sb.Prepared) != 1 || sb.Prepared[0] != "shield" || len(sb.Cantrips) != 1 {
		t.Errorf("spellbook = %+v, want prepared replaced and cantrips kept (objects merge, arrays replace)", sb)
	}
	if len(out.Inventory) != 1 || out.Inventory[0].ID != "b" {
		t.Errorf("inventory = %+v, want the array replaced", out.Inventory)
	}

	cleared, err := MergePatchCharacter(current, []byte(`{"spellbooks": null, "name": null}`))
	if err != nil || cleared.Spellbooks != nil || cleared.Name != "" {
		t.Errorf("null should clear fields: %+v (err %v)", cleared, err)
	}
}

func TestMergePatchRejections(t *testing.T) {
	current := Character{ID: "c1", Name: "N"}
	cases := map[string]string{
		"unknown field":   `{"inspiraton": 2}`,
		"wrong type":      `{"inspiration": "lots"}`,
		"changing the id": `{"id": "other"}`,
		"not an object":   `[1, 2]`,
		"not json":        `{oops`,
	}
	for name, body := range cases {
		if _, err := MergePatchCharacter(current, []byte(body)); !errors.Is(err, ErrBadPatch) {
			t.Errorf("%s: err = %v, want ErrBadPatch", name, err)
		}
	}
}
