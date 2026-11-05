package character

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func taggedAttack(t *testing.T, sh Sheet, name string) AttackView {
	t.Helper()
	for _, a := range sh.Attacks {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no attack named %q in %+v", name, sh.Attacks)
	return AttackView{}
}

func TestPlusWeaponTagRaisesToHitAndDamage(t *testing.T) {
	lib := loadExample(t)
	c := Character{ID: "w", Classes: []ClassLevel{{ClassID: "wizard", Level: 1}}, Inventory: []InventoryItem{
		{ID: "a", ItemID: "dagger", Quantity: 1, Equipped: true},
		{ID: "b", ItemID: "dagger", Quantity: 1, Equipped: true, Tags: []string{"+2"}},
	}}
	sh := mustCompute(t, c, lib, Ruleset{})
	noContentWarnings(t, sh)
	plain, magic := taggedAttack(t, sh, "Dagger"), taggedAttack(t, sh, "Dagger +2")
	if magic.ToHit != plain.ToHit+2 || magic.DamageBonus != plain.DamageBonus+2 {
		t.Errorf("+2 dagger %+v should be 2 better than %+v", magic, plain)
	}
	if !magic.Proficient {
		t.Error("proficiency with daggers should cover a tagged dagger")
	}
}

func TestPlusArmourAndShieldTagsRaiseAC(t *testing.T) {
	lib := loadExample(t)
	ac := func(armourTags, shieldTags []string) int {
		sh := mustCompute(t, Character{ID: "w", Inventory: []InventoryItem{
			{ID: "a", ItemID: "leather_armor", Quantity: 1, Equipped: true, Tags: armourTags},
			{ID: "s", ItemID: "shield", Quantity: 1, Equipped: true, Tags: shieldTags},
		}}, lib, Ruleset{})
		return sh.AC
	}
	base := ac(nil, nil)
	if got := ac([]string{"+1"}, nil); got != base+1 {
		t.Errorf("+1 armour: AC %d, want %d", got, base+1)
	}
	// The shield's own +2 still counts alongside its tag.
	if got := ac(nil, []string{"+1"}); got != base+1 {
		t.Errorf("+1 shield: AC %d, want %d", got, base+1)
	}
	if got := ac([]string{"+1"}, []string{"+2"}); got != base+3 {
		t.Errorf("+1 armour and +2 shield: AC %d, want %d", got, base+3)
	}
}

func TestUnequippedTaggedArmourDoesNothing(t *testing.T) {
	lib := loadExample(t)
	sh := mustCompute(t, Character{ID: "w", Inventory: []InventoryItem{
		{ID: "a", ItemID: "leather_armor", Quantity: 1, Tags: []string{"+3"}},
	}}, lib, Ruleset{})
	if sh.AC != 10 { // unarmoured, Dex 10
		t.Errorf("AC = %d with armour that isn't worn", sh.AC)
	}
}

func TestTagBonusNeedsAttunementIfTheItemDoes(t *testing.T) {
	lib := fakeLib{items: map[string]Item{
		"blade": {ID: "blade", Name: "Blade", Kind: ItemWeapon, RequiresAttunement: true, Weapon: &WeaponStats{
			Category: "martial", Attack: AttackDetails{DamageDice: "1d8", DamageType: "slashing"}}},
	}}
	attack := func(attuned bool) AttackView {
		sh := mustCompute(t, Character{ID: "w", Inventory: []InventoryItem{
			{ID: "b", ItemID: "blade", Quantity: 1, Equipped: true, Attuned: attuned, Tags: []string{"+1"}},
		}}, lib, Ruleset{})
		return taggedAttack(t, sh, "Blade +1")
	}
	if on, off := attack(true), attack(false); on.ToHit != off.ToHit+1 || on.DamageBonus != off.DamageBonus+1 {
		t.Errorf("attuned %+v should be 1 better than unattuned %+v", on, off)
	}
}

func TestTagsShowOnTheInventoryAndDontTouchTheLibrary(t *testing.T) {
	lib := loadExample(t)
	sh := mustCompute(t, Character{ID: "w", Inventory: []InventoryItem{
		{ID: "a", ItemID: "longsword", Quantity: 1, Tags: []string{"+1", "silvered"}},
	}}, lib, Ruleset{})
	v := sh.Inventory[0]
	if v.Name != "Silvered Longsword +1" || v.ItemID != "longsword" || !slices.Equal(v.Tags, []string{"+1", "silvered"}) {
		t.Errorf("view = %+v", v)
	}
	if !strings.Contains(v.Description, "shape-shifted") {
		t.Errorf("silvered rules missing from %q", v.Description)
	}
	it, err := lib.Item("longsword")
	if err != nil {
		t.Fatal(err)
	}
	if it.Name != "Longsword" || it.Weapon.MagicBonus != 0 || strings.Contains(it.Description, "shape-shifted") {
		t.Errorf("the library's longsword changed: %+v", it)
	}
}

func TestAddItemTags(t *testing.T) {
	lib := loadExample(t)
	c := Character{ID: "w"}
	add := func(req AddItemRequest) string {
		t.Helper()
		id, err := AddItem(&c, lib, req)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	plain := add(AddItemRequest{ItemID: "dagger"})
	magic := add(AddItemRequest{ItemID: "dagger", Tags: []string{" Silvered", "+1"}})
	if plain == magic {
		t.Fatal("a tagged dagger merged into the plain stack")
	}
	if got := add(AddItemRequest{ItemID: "dagger", Tags: []string{"+1", "silvered"}}); got != magic {
		t.Error("the same tags in another order should merge")
	}
	if got := c.Inventory[entryIndex(&c, magic)]; got.Quantity != 2 || !slices.Equal(got.Tags, []string{"+1", "silvered"}) {
		t.Errorf("entry = %+v", got)
	}
	if got := add(AddItemRequest{ItemID: "dagger"}); got != plain {
		t.Error("an untagged dagger should merge with the plain stack")
	}

	for name, req := range map[string]AddItemRequest{
		"not gear":    {ItemID: "rope_hempen", Tags: []string{"+1"}},
		"two +X":      {ItemID: "dagger", Tags: []string{"+1", "+2"}},
		"zero":        {ItemID: "dagger", Tags: []string{"+0"}},
		"negative":    {ItemID: "dagger", Tags: []string{"-1"}},
		"empty":       {ItemID: "dagger", Tags: []string{""}},
		"custom kind": {Custom: &Item{Name: "Idol", Kind: ItemOther}, Tags: []string{"silvered"}},
	} {
		if _, err := AddItem(&c, lib, req); !isBadRequest(err) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestPatchEntryTags(t *testing.T) {
	lib := loadExample(t)
	c := Character{ID: "w"}
	id, err := AddItem(&c, lib, AddItemRequest{ItemID: "dagger"})
	if err != nil {
		t.Fatal(err)
	}
	patch := func(body string) error {
		var p map[string]json.RawMessage
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatal(err)
		}
		return PatchEntry(&c, lib, id, p)
	}
	if err := patch(`{"tags":["SILVERED","+1"]}`); err != nil {
		t.Fatal(err)
	}
	if got := c.Inventory[0].Tags; !slices.Equal(got, []string{"+1", "silvered"}) {
		t.Errorf("tags = %v", got)
	}
	for _, body := range []string{`{"tags":"+1"}`, `{"tags":["+1","+2"]}`, `{"tags":["bad!"]}`} {
		if err := patch(body); !isBadRequest(err) {
			t.Errorf("%s: err = %v", body, err)
		}
	}
	if got := c.Inventory[0].Tags; len(got) != 2 {
		t.Errorf("a rejected patch changed the tags: %v", got)
	}
	if err := patch(`{"tags":null}`); err != nil {
		t.Fatal(err)
	}
	if got := c.Inventory[0].Tags; got != nil {
		t.Errorf("tags should be cleared: %v", got)
	}
}

func TestSplittingAStackKeepsTags(t *testing.T) {
	lib := loadExample(t)
	c := Character{ID: "w"}
	id, err := AddItem(&c, lib, AddItemRequest{ItemID: "dagger", Quantity: 3, Tags: []string{"silvered"}})
	if err != nil {
		t.Fatal(err)
	}
	one := 1
	moved, err := MoveEntry(&c, id, MoveRequest{Quantity: &one})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Inventory[entryIndex(&c, moved)].Tags; !slices.Equal(got, []string{"silvered"}) {
		t.Errorf("tags on the split stack = %v", got)
	}
}

func TestBadTagsFromADirectEditOnlyWarn(t *testing.T) {
	lib := loadExample(t)
	sh := mustCompute(t, Character{ID: "w", Inventory: []InventoryItem{
		{ID: "a", ItemID: "rope_hempen", Quantity: 1, Tags: []string{"+1"}},
		{ID: "b", ItemID: "dagger", Quantity: 1, Equipped: true, Tags: []string{"+1", "+3"}},
	}}, lib, Ruleset{})
	n := 0
	for _, w := range sh.Warnings {
		if w.Code == WarnBadItemTag {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d bad_item_tag warnings in %+v, want 2", n, sh.Warnings)
	}
	if strings.Contains(sh.Inventory[0].Name, "+1") {
		t.Errorf("rope name = %q", sh.Inventory[0].Name)
	}
	taggedAttack(t, sh, "Dagger +3") // the highest counts
}

func TestStoredVariantItemsBecomeTags(t *testing.T) {
	c, err := decodeCharacter([]byte(`{"id":"w","inventory":[
		{"item_id":"silvered_shortsword","quantity":1,"notes":"110 gp"},
		{"item_id":"longsword_plus_1","quantity":1,"equipped":true},
		{"item_id":"silvered_longsword_plus_2","quantity":1},
		{"item_id":"dagger","quantity":1},
		{"item_id":"silvered_idol","custom":{"id":"silvered_idol","name":"Idol","kind":"other"},"quantity":1}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		id   string
		tags []string
	}{
		{"shortsword", []string{"silvered"}},
		{"longsword", []string{"+1"}},
		{"longsword", []string{"+2", "silvered"}},
		{"dagger", nil},
		{"silvered_idol", nil},
	}
	for i, w := range want {
		e := c.Inventory[i]
		if e.ItemID != w.id || !slices.Equal(e.Tags, w.tags) {
			t.Errorf("entry %d = %q %v, want %q %v", i, e.ItemID, e.Tags, w.id, w.tags)
		}
	}
	if c.Inventory[0].Notes != "110 gp" || !c.Inventory[1].Equipped {
		t.Errorf("other fields should survive: %+v", c.Inventory[:2])
	}
}

func TestLibraryHasNoVariantItems(t *testing.T) {
	lib := loadExample(t)
	for id := range lib.items {
		if strings.HasPrefix(id, "silvered_") || plusIDRe.MatchString(id) {
			t.Errorf("%s should be the plain item with tags on its entry", id)
		}
	}
}
