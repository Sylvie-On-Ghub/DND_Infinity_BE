package character

// item_spell_ability_test.go - an item's spells block may leave its ability blank: the item then casts
// with the wielder's class spellcasting ("using your spell save DC"), taking the best DC if several.

import "testing"

func wielderLib() fakeLib {
	lib := casterLib()
	lib.items = map[string]Item{
		"staff": {ID: "staff", Name: "Staff", Kind: ItemOther, Spells: &SpellGrant{SpellIDs: []string{"magic_missile"}}},
	}
	return lib
}

func TestItemSpellsUseTheWieldersCasting(t *testing.T) {
	for _, tc := range []struct {
		name    string
		classes []ClassLevel
		want    Ability
		wantDC  int // 8 + PB 2 + the ability's modifier
	}{
		{"wizard", []ClassLevel{cls("wizard", 1)}, Int, 13},
		{"warlock", []ClassLevel{cls("warlock", 1)}, Cha, 12},
		{"wizard and warlock take the better DC", []ClassLevel{cls("warlock", 1), cls("wizard", 1)}, Int, 13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newChar(1, map[Ability]int{Int: 16, Cha: 14})
			c.Classes = tc.classes
			c.Inventory = []InventoryItem{{ItemID: "staff", Quantity: 1, Equipped: true}}
			sh := mustCompute(t, c, wielderLib(), Ruleset{})
			src, ok := findSource(sh, "item:staff")
			if !ok {
				t.Fatalf("no staff spell source: %+v", sh.Spellcasting)
			}
			if src.Ability != tc.want || src.SaveDC != tc.wantDC {
				t.Errorf("staff source = %s DC %d, want %s DC %d", src.Ability, src.SaveDC, tc.want, tc.wantDC)
			}
			if hasWarning(sh, WarnNoCastingAbility) {
				t.Errorf("a caster's staff shouldn't warn: %+v", sh.Warnings)
			}
		})
	}
}

func TestItemSpellsWithoutAWielderCasterWarn(t *testing.T) {
	c := newChar(1, map[Ability]int{Int: 16})
	c.Inventory = []InventoryItem{{ItemID: "staff", Quantity: 1, Equipped: true}}
	sh := mustCompute(t, c, wielderLib(), Ruleset{})
	src, _ := findSource(sh, "item:staff")
	if src.SaveDC != 10 || !hasWarning(sh, WarnNoCastingAbility) {
		t.Errorf("non-caster staff = DC %d, warnings %+v; want DC 10 and a no_casting_ability warning", src.SaveDC, sh.Warnings)
	}
}

func TestLoaderAllowsBlankAbilityOnlyOnItems(t *testing.T) {
	lib, _ := LoadPacks(LoadOptions{}, memPack("p", map[string]string{
		"spells/s.json": `[{"id":"zap","name":"Zap","level":1}]`,
		"items/i.json":  `[{"id":"staff","name":"Staff","kind":"other","spells":{"spell_ids":["zap"]}}]`,
	}))
	if lib.HasErrors() {
		t.Errorf("an item may leave its spell ability blank: %+v", lib.Issues())
	}
	lib, _ = LoadPacks(LoadOptions{}, memPack("p", map[string]string{
		"spells/s.json":   `[{"id":"zap","name":"Zap","level":1}]`,
		"features/f.json": `[{"id":"f","name":"F","origin":"race","description":"x","spells":{"spell_ids":["zap"]}}]`,
	}))
	if !lib.HasErrors() {
		t.Error("a feature's spells still need an ability")
	}
}
