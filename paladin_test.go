package character

// paladin_test.go - tests for the Paladin in content/legacy-classes. Its spell list shares spells with
// the other packs, so they all load. Skips itself if the packs aren't there.

import (
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
)

func loadPaladinLib(t *testing.T) *ContentLibrary {
	t.Helper()
	loadElfLib(t)
	if _, err := os.Stat("content/legacy-classes"); err != nil {
		t.Skip("content/legacy-classes not found")
	}
	lib, err := LoadDirs(LoadOptions{Strict: true}, "content/example", "content/classes", "content/core-races", "content/legacy-classes")
	if err != nil {
		if lib != nil {
			dumpIssues(t, lib)
		}
		t.Fatalf("the packs failed to load together: %v", err)
	}
	return lib
}

// paladinAt is a level N Oath of Devotion paladin with Str 16 and Cha 16 (+3).
func paladinAt(level int) Character {
	return Character{
		ID: "p", Name: "Paladin",
		Abilities: map[Ability]AbilityScore{Str: {Score: 16}, Dex: {Score: 10}, Con: {Score: 14}, Int: {Score: 8}, Wis: {Score: 12}, Cha: {Score: 16}},
		Classes:   []ClassLevel{{ClassID: "paladin_legacy", SubclassID: "devotion", Level: level}},
	}
}

func paladinSource(t *testing.T, sh Sheet) SpellSourceView {
	t.Helper()
	for _, s := range sh.Spellcasting {
		if s.Kind == SourceClass {
			return s
		}
	}
	t.Fatalf("no class spell source in %+v", sh.Spellcasting)
	return SpellSourceView{}
}

func TestPaladinBasics(t *testing.T) {
	lib := loadPaladinLib(t)
	class, err := lib.Class("paladin_legacy")
	if err != nil || class.Name != "Paladin (Legacy)" {
		t.Errorf("class = %+v (err %v)", class, err)
	}
	sh := mustCompute(t, paladinAt(1), lib, Ruleset{})
	noContentWarnings(t, sh)
	for _, a := range []string{"light", "medium", "heavy", "shield"} {
		if !slices.Contains(sh.ArmourProfs, a) {
			t.Errorf("armour proficiencies = %v", sh.ArmourProfs)
		}
	}
	if !slices.Contains(sh.WeaponProfs, "simple") || !slices.Contains(sh.WeaponProfs, "martial") {
		t.Errorf("weapon proficiencies = %v", sh.WeaponProfs)
	}
	if !sh.Abilities[Wis].SaveProf || !sh.Abilities[Cha].SaveProf || sh.Abilities[Str].SaveProf {
		t.Errorf("saves = %+v", sh.Abilities)
	}
	opts, err := ChoiceOptions(paladinAt(1), lib, Ruleset{}, "class:paladin_legacy:start", "skills")
	if err != nil || len(opts) != 6 {
		t.Errorf("skill options = %+v (err %v)", opts, err)
	}
	if _, ok := findFeature(sh, "paladin_legacy_starting_equipment"); !ok {
		t.Error("a first-class paladin should see the starting equipment")
	}
}

// The slot and proficiency columns of the class table, level by level.
func TestPaladinTable(t *testing.T) {
	lib := loadPaladinLib(t)
	want := [][]int{
		{}, {2}, {3}, {3}, {4, 2}, {4, 2}, {4, 3}, {4, 3}, {4, 3, 2}, {4, 3, 2},
		{4, 3, 3}, {4, 3, 3}, {4, 3, 3, 1}, {4, 3, 3, 1}, {4, 3, 3, 2}, {4, 3, 3, 2},
		{4, 3, 3, 3, 1}, {4, 3, 3, 3, 1}, {4, 3, 3, 3, 2}, {4, 3, 3, 3, 2},
	}
	for i, slots := range want {
		level := i + 1
		sh := mustCompute(t, paladinAt(level), lib, Ruleset{})
		got := []int{}
		for _, s := range sh.SpellSlots {
			got = append(got, s.Max)
		}
		if !slices.Equal(got, slots) {
			t.Errorf("level %d: slots %v, want %v", level, got, slots)
		}
		if pb := 2 + (level-1)/4; sh.ProficiencyBonus != pb {
			t.Errorf("level %d: proficiency bonus %d, want %d", level, sh.ProficiencyBonus, pb)
		}
	}
}

func TestPaladinFeaturesByLevel(t *testing.T) {
	lib := loadPaladinLib(t)
	gained := map[string]int{
		"paladin_legacy_divine_sense": 1, "paladin_legacy_lay_on_hands": 1,
		"paladin_legacy_fighting_style": 2, "paladin_legacy_spellcasting": 2, "paladin_legacy_divine_smite": 2,
		"paladin_legacy_divine_health": 3, "paladin_legacy_harness_divine_power": 3, "devotion_legacy_tenets": 3, "devotion_legacy_channel_divinity": 3,
		"paladin_legacy_asi_4": 4, "paladin_legacy_extra_attack": 5, "paladin_legacy_aura_of_protection": 6, "devotion_legacy_aura": 7, "paladin_legacy_asi_8": 8,
		"paladin_legacy_aura_of_courage": 10, "paladin_legacy_improved_divine_smite": 11, "paladin_legacy_asi_12": 12, "paladin_legacy_cleansing_touch": 14,
		"devotion_legacy_purity_of_spirit": 15, "paladin_legacy_asi_16": 16, "paladin_legacy_asi_19": 19, "devotion_legacy_holy_nimbus": 20,
	}
	for level := 1; level <= 20; level++ {
		sh := mustCompute(t, paladinAt(level), lib, Ruleset{})
		noContentWarnings(t, sh)
		for id, at := range gained {
			if _, ok := findFeature(sh, id); ok != (level >= at) {
				t.Errorf("level %d: has %s = %v, but it is gained at %d", level, id, ok, at)
			}
		}
	}
}

func TestPaladinSpellcasting(t *testing.T) {
	lib := loadPaladinLib(t)
	for _, s := range mustCompute(t, paladinAt(1), lib, Ruleset{}).Spellcasting {
		if s.Kind == SourceClass {
			t.Errorf("no spellcasting until level 2: %+v", s)
		}
	}
	// Cha 16: 3 + half the level, rounded down. The rules' own example is Cha 14 at level 5: 4 spells.
	for level, want := range map[int]int{2: 4, 3: 4, 5: 5, 20: 13} {
		src := paladinSource(t, mustCompute(t, paladinAt(level), lib, Ruleset{}))
		if src.Ability != Cha || src.SpellsAllowed != want || src.SaveDC != 8+2+(level-1)/4+3 {
			t.Errorf("level %d: %+v, want %d spells", level, src, want)
		}
	}
	c := paladinAt(5)
	c.Abilities[Cha] = AbilityScore{Score: 14}
	if got := paladinSource(t, mustCompute(t, c, lib, Ruleset{})).SpellsAllowed; got != 4 {
		t.Errorf("Cha 14 at level 5 prepares %d", got)
	}
	c.Abilities[Cha] = AbilityScore{Score: 1}
	c.Classes[0].Level = 2
	if got := paladinSource(t, mustCompute(t, c, lib, Ruleset{})).SpellsAllowed; got != 1 {
		t.Errorf("the minimum is one spell, got %d", got)
	}
}

func TestDevotionOathSpells(t *testing.T) {
	lib := loadPaladinLib(t)
	has := func(level int, id string) bool {
		src := paladinSource(t, mustCompute(t, paladinAt(level), lib, Ruleset{}))
		for _, s := range src.Spells {
			if s.ID == id && s.Granted {
				return true
			}
		}
		return false
	}
	for id, at := range map[string]int{
		"protection_from_evil_and_good": 3, "sanctuary": 3, "lesser_restoration": 5, "zone_of_truth": 5,
		"beacon_of_hope": 9, "dispel_magic": 9, "freedom_of_movement": 13, "guardian_of_faith": 13, "commune": 17, "flame_strike": 17,
	} {
		if at > 2 && has(at-1, id) {
			t.Errorf("%s is too early at level %d", id, at-1)
		}
		if !has(at, id) || !has(20, id) {
			t.Errorf("%s should be always prepared from level %d", id, at)
		}
	}
}

func TestPaladinResources(t *testing.T) {
	lib := loadPaladinLib(t)
	check := func(level int, id string, max int, recharge RechargeType) {
		t.Helper()
		sh := mustCompute(t, paladinAt(level), lib, Ruleset{})
		r, ok := findResource(sh, id)
		if !ok || r.Max != max || r.Recharge != recharge {
			t.Errorf("level %d: %s = %+v, want %d per %s", level, id, r, max, recharge)
		}
	}
	check(1, "paladin_legacy_divine_sense", 4, RechargeLongRest) // 1 + Cha 3
	check(1, "paladin_legacy_lay_on_hands", 5, RechargeLongRest)
	check(7, "paladin_legacy_lay_on_hands", 35, RechargeLongRest)
	check(20, "paladin_legacy_lay_on_hands", 100, RechargeLongRest)
	check(3, "devotion_legacy_channel_divinity", 1, RechargeShortRest)
	check(3, "paladin_legacy_harness_divine_power", 1, RechargeLongRest)
	check(7, "paladin_legacy_harness_divine_power", 2, RechargeLongRest)
	check(15, "paladin_legacy_harness_divine_power", 3, RechargeLongRest)
	check(14, "paladin_legacy_cleansing_touch", 3, RechargeLongRest)
	check(20, "devotion_legacy_holy_nimbus", 1, RechargeLongRest)

	// Low Charisma: 1 + the modifier for Divine Sense, at least one for both.
	c := paladinAt(14)
	c.Abilities[Cha] = AbilityScore{Score: 10}
	sh := mustCompute(t, c, lib, Ruleset{})
	if r, _ := findResource(sh, "paladin_legacy_divine_sense"); r.Max != 1 {
		t.Errorf("Divine Sense at Cha 10 = %d", r.Max)
	}
	if r, _ := findResource(sh, "paladin_legacy_cleansing_touch"); r.Max != 1 {
		t.Errorf("Cleansing Touch at Cha 10 = %d", r.Max)
	}
}

func TestPaladinSmitesAreRiders(t *testing.T) {
	lib := loadPaladinLib(t)
	c := paladinAt(11)
	c.Inventory = []InventoryItem{{ID: "s", ItemID: "longsword", Quantity: 1, Equipped: true}}
	sh := mustCompute(t, c, lib, Ruleset{})
	var dice []string
	for _, a := range sh.Attacks {
		if a.Name != "Longsword" {
			continue
		}
		for _, r := range a.DamageRiders {
			dice = append(dice, r.Dice)
		}
		if a.DamageBonus != 3 || a.ToHit != 3+4 {
			t.Errorf("riders shouldn't change the numbers: %+v", a)
		}
	}
	slices.Sort(dice)
	if !slices.Equal(dice, []string{"1d8", "2d8"}) {
		t.Errorf("riders = %v", dice)
	}
}

func TestPaladinFightingStyles(t *testing.T) {
	lib := loadPaladinLib(t)
	opts, err := ChoiceOptions(paladinAt(2), lib, Ruleset{}, "paladin_legacy_fighting_style", "style")
	if err != nil || len(opts) != 12 {
		t.Errorf("fighting styles = %+v (err %v)", opts, err)
	}
	pick := func(style string, extra ...Selection) Sheet {
		c := paladinAt(2)
		c.Selections = append([]Selection{{FeatureID: "paladin_legacy_fighting_style", ChoiceID: "style", Picked: []string{style}}}, extra...)
		sh := mustCompute(t, c, lib, Ruleset{})
		noContentWarnings(t, sh)
		return sh
	}
	base := mustCompute(t, paladinAt(2), lib, Ruleset{})

	found := false
	for _, s := range pick("paladin_legacy_style_blind_fighting").Senses {
		found = found || (s.Name == "blindsight" && s.Range == 10)
	}
	if !found {
		t.Error("Blind Fighting should give blindsight 10")
	}
	if sh := pick("paladin_legacy_style_defense"); sh.AC != base.AC {
		t.Errorf("Defense needs armour: AC %d, was %d", sh.AC, base.AC)
	}

	sh := pick("paladin_legacy_style_blessed_warrior", Selection{FeatureID: "paladin_legacy_style_blessed_warrior", ChoiceID: "cantrips", Picked: []string{"guidance", "sacred_flame"}})
	names := []string{}
	for _, s := range sh.Spellcasting {
		if s.Kind == SourceFeature {
			if s.Ability != Cha {
				t.Errorf("Blessed Warrior uses %s", s.Ability)
			}
			for _, v := range s.Cantrips {
				names = append(names, v.ID)
			}
		}
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"guidance", "sacred_flame"}) {
		t.Errorf("Blessed Warrior cantrips = %v; pending %+v", names, sh.PendingChoices)
	}

	seen := false
	for _, a := range pick("paladin_legacy_style_unarmed").Attacks {
		seen = seen || (a.Name == "Unarmed Fighting" && a.DamageDice == "1d6" && a.DamageBonus == 3 && a.Ability == Str)
	}
	if !seen {
		t.Error("Unarmed Fighting should add a 1d6 + Strength attack")
	}
}

func TestPaladinAurasWidenAtEighteen(t *testing.T) {
	lib := loadPaladinLib(t)
	note := func(level int, prefix string) string {
		for _, n := range mustCompute(t, paladinAt(level), lib, Ruleset{}).Notes {
			if strings.HasPrefix(n.Note, prefix) {
				return n.Note
			}
		}
		return ""
	}
	for _, name := range []string{"Aura of Protection", "Aura of Courage", "Aura of Devotion"} {
		if n := note(17, name); !strings.Contains(n, "10 ft") {
			t.Errorf("level 17 %s: %q", name, n)
		}
		if n := note(18, name); !strings.Contains(n, "30 ft") {
			t.Errorf("level 18 %s: %q", name, n)
		}
	}
}

func TestPaladinMulticlassing(t *testing.T) {
	lib := loadPaladinLib(t)
	c := Character{
		ID:        "m",
		Abilities: map[Ability]AbilityScore{Str: {Score: 12}, Dex: {Score: 14}, Cha: {Score: 16}},
		Classes:   []ClassLevel{{ClassID: "rogue", Level: 3}, {ClassID: "paladin_legacy", Level: 2}},
	}
	sh := mustCompute(t, c, lib, Ruleset{})
	if !hasWarning(sh, WarnPrereqUnmet) {
		t.Errorf("Strength 12 should trip the multiclass prerequisite: %+v", sh.Warnings)
	}
	if _, ok := findFeature(sh, "paladin_legacy_starting_equipment"); ok {
		t.Error("starting equipment is for a first class only")
	}
	if slices.Contains(sh.ArmourProfs, "heavy") {
		t.Errorf("multiclassing shouldn't give heavy armour: %v", sh.ArmourProfs)
	}
	c.Abilities[Str] = AbilityScore{Score: 13}
	if sh := mustCompute(t, c, lib, Ruleset{}); hasWarning(sh, WarnPrereqUnmet) {
		t.Errorf("13s meet the prerequisite: %+v", sh.Warnings)
	}
}

func TestResourceMaxBonus(t *testing.T) {
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/f.json": `[{"id":"f","name":"F","resource":{"name":"F","max":1,"max_ability":"cha","max_bonus":1,"recharge":"long_rest"}}]`,
	}))
	uses := func(cha int) int {
		c := Character{ID: "c", Abilities: map[Ability]AbilityScore{Cha: {Score: cha}}, FeatureIDs: []string{"f"}}
		r, _ := findResource(mustCompute(t, c, lib, Ruleset{}), "f")
		return r.Max
	}
	if uses(16) != 4 || uses(10) != 1 || uses(4) != 1 {
		t.Errorf("uses: Cha 16 %d, Cha 10 %d, Cha 4 %d", uses(16), uses(10), uses(4))
	}
}

func TestAuraOfProtectionAddsCharismaToSaves(t *testing.T) {
	lib := loadPaladinLib(t)
	saves := func(c Character) map[Ability]int {
		sh := mustCompute(t, c, lib, Ruleset{})
		out := map[Ability]int{}
		for a, v := range sh.Abilities {
			out[a] = v.SaveBonus
		}
		return out
	}
	before, after := saves(paladinAt(5)), saves(paladinAt(6))
	for a := range before {
		if after[a] != before[a]+3 { // Cha 16; proficiency bonus is 3 at both levels
			t.Errorf("%s save: %d at level 5, %d at level 6", a, before[a], after[a])
		}
	}

	withCha := func(score int) Character {
		c := paladinAt(6)
		c.Abilities = maps.Clone(c.Abilities)
		c.Abilities[Cha] = AbilityScore{Score: score}
		return c
	}
	// The minimum is +1, even with a low or negative modifier.
	for score, bonus := range map[int]int{8: 1, 10: 1, 12: 1, 14: 2, 20: 5} {
		// Str isn't a proficient save and doesn't depend on Charisma: +3 mod plus the aura.
		if got := saves(withCha(score))[Str]; got != 3+bonus {
			t.Errorf("Cha %d: Str save %d, want %d", score, got, 3+bonus)
		}
	}

	// A raised Charisma counts, including from a feature.
	c := withCha(14)
	c.Modifiers = []Modifier{{Target: AbilityTarget(Cha), Kind: ModAdd, Value: 2}}
	if got := saves(c)[Str]; got != 3+3 {
		t.Errorf("Cha 14+2: Str save %d, want 6", got)
	}

	// Only the save numbers change, and only once however many saves are read.
	sh := mustCompute(t, paladinAt(6), lib, Ruleset{})
	if sh.Abilities[Cha].Mod != 3 || sh.Abilities[Cha].SaveBonus != 3+3+3 {
		t.Errorf("Cha: %+v, want mod 3 and save 9", sh.Abilities[Cha])
	}
}

func TestValueAbilityModifiers(t *testing.T) {
	bad := map[string]string{
		"not an ability": `{"target":"save.all","kind":"add","value_ability":"luck"}`,
		"wrong kind":     `{"target":"save.all","kind":"set","value_ability":"cha"}`,
		"dice":           `{"target":"damage.all","kind":"add","dice":"1d6","value_ability":"cha"}`,
		"ability target": `{"target":"ability.str","kind":"add","value_ability":"cha"}`,
	}
	for name, m := range bad {
		lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
			"features/f.json": `[{"id":"f","name":"F","modifiers":[` + m + `]}]`,
		}))
		if !lib.HasErrors() {
			t.Errorf("%s should be a content error", name)
		}
	}

	// Two features stack, and the same one twice doesn't.
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
		"features/f.json": `[{"id":"a","name":"A","modifiers":[{"target":"initiative","kind":"add","value_ability":"wis"}]},
			{"id":"b","name":"B","modifiers":[{"target":"initiative","kind":"add","value_ability":"cha"}]}]`,
	}))
	c := Character{ID: "c", FeatureIDs: []string{"a", "b"}, Abilities: map[Ability]AbilityScore{Wis: {Score: 14}, Cha: {Score: 16}}}
	if sh := mustCompute(t, c, lib, Ruleset{}); sh.Initiative != 5 {
		t.Errorf("initiative = %d, want 2 + 3", sh.Initiative)
	}
	c.FeatureIDs = []string{"a", "a"}
	if sh := mustCompute(t, c, lib, Ruleset{}); sh.Initiative != 2 {
		t.Errorf("initiative = %d, want 2", sh.Initiative)
	}
}

func TestDefenseFightingStyleNeedsArmour(t *testing.T) {
	lib := loadPaladinLib(t)
	ac := func(style bool, inv ...InventoryItem) int {
		c := paladinAt(2)
		c.Inventory = inv
		if style {
			c.Selections = []Selection{{FeatureID: "paladin_legacy_fighting_style", ChoiceID: "style", Picked: []string{"paladin_legacy_style_defense"}}}
		}
		return mustCompute(t, c, lib, Ruleset{}).AC
	}
	mail := InventoryItem{ID: "m", ItemID: "chain_mail", Quantity: 1, Equipped: true}
	shield := InventoryItem{ID: "s", ItemID: "shield", Quantity: 1, Equipped: true}
	carried := InventoryItem{ID: "c", ItemID: "chain_mail", Quantity: 1}

	if without, with := ac(false, mail), ac(true, mail); without != 16 || with != 17 {
		t.Errorf("chain mail: AC %d without Defense, %d with", without, with)
	}
	if with := ac(true, mail, shield); with != 19 {
		t.Errorf("chain mail and a shield with Defense: AC %d, want 19", with)
	}
	if without, with := ac(false, shield), ac(true, shield); with != without {
		t.Errorf("a shield isn't armour: AC %d without Defense, %d with", without, with)
	}
	if without, with := ac(false, carried), ac(true, carried); with != without {
		t.Errorf("unequipped armour doesn't count: AC %d without Defense, %d with", without, with)
	}
}

func styled(level int, style string, inv ...InventoryItem) Character {
	c := paladinAt(level)
	c.Inventory = inv
	c.Selections = []Selection{{FeatureID: "paladin_legacy_fighting_style", ChoiceID: "style", Picked: []string{style}}}
	return c
}

func equipped(id string) InventoryItem {
	return InventoryItem{ID: id, ItemID: id, Quantity: 1, Equipped: true}
}

func attackNamed(t *testing.T, sh Sheet, name string) AttackView {
	t.Helper()
	for _, a := range sh.Attacks {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no %q in %+v", name, sh.Attacks)
	return AttackView{}
}

func TestDuelingFightingStyle(t *testing.T) {
	lib := loadPaladinLib(t)
	const style = "paladin_legacy_style_dueling"
	const str = 3 // Str 16
	for name, tc := range map[string]struct {
		attack string
		inv    []InventoryItem
		want   int
	}{
		"one-handed":             {"Longsword", []InventoryItem{equipped("longsword")}, str + 2},
		"with a shield":          {"Longsword", []InventoryItem{equipped("longsword"), equipped("shield")}, str + 2},
		"two-handed":             {"Greatsword", []InventoryItem{equipped("greatsword")}, str},
		"with another weapon":    {"Longsword", []InventoryItem{equipped("longsword"), equipped("shortsword")}, str},
		"two in one stack":       {"Longsword", []InventoryItem{{ID: "l", ItemID: "longsword", Quantity: 2, Equipped: true}}, str},
		"a carried weapon is ok": {"Longsword", []InventoryItem{equipped("longsword"), {ID: "s", ItemID: "shortsword", Quantity: 1}}, str + 2},
	} {
		sh := mustCompute(t, styled(2, style, tc.inv...), lib, Ruleset{})
		if got := attackNamed(t, sh, tc.attack).DamageBonus; got != tc.want {
			t.Errorf("%s: damage bonus %d, want %d", name, got, tc.want)
		}
	}
	// Ranged weapons and unarmed strikes don't count, and to-hit doesn't change.
	sh := mustCompute(t, styled(2, style, equipped("light_crossbow")), lib, Ruleset{})
	if got := attackNamed(t, sh, "Light Crossbow").DamageBonus; got != 0 { // Dex 10
		t.Errorf("light crossbow damage bonus %d", got)
	}
	sh = mustCompute(t, styled(2, style, equipped("longsword")), lib, Ruleset{})
	if got := attackNamed(t, sh, "Longsword").ToHit; got != str+2 {
		t.Errorf("to hit %d", got)
	}
	if got := attackNamed(t, sh, "Unarmed Strike").DamageBonus; got != str+1 {
		t.Errorf("unarmed damage bonus %d", got)
	}
}

func TestThrownWeaponFightingStyle(t *testing.T) {
	lib := loadPaladinLib(t)
	const style = "paladin_legacy_style_thrown_weapon"
	sh := mustCompute(t, styled(2, style, equipped("javelin")), lib, Ruleset{})

	melee, thrown := attackNamed(t, sh, "Javelin"), attackNamed(t, sh, "Javelin (Thrown)")
	if melee.DamageBonus != 3 || thrown.DamageBonus != 4 {
		t.Errorf("melee damage %d, thrown damage %d; want 3 and 4", melee.DamageBonus, thrown.DamageBonus)
	}
	if thrown.Ability != Str || thrown.RangeNormal != 30 || thrown.RangeLong != 120 || thrown.ToHit != melee.ToHit {
		t.Errorf("thrown = %+v, melee = %+v", thrown, melee)
	}

	// A dart is already a ranged attack, so it gets the bonus on its only entry.
	sh = mustCompute(t, styled(2, style, equipped("dart")), lib, Ruleset{})
	if got := attackNamed(t, sh, "Dart").DamageBonus; got != 1 { // Dex 10
		t.Errorf("dart damage bonus %d", got)
	}
	for _, a := range sh.Attacks {
		if a.Name == "Dart (Thrown)" {
			t.Error("a dart doesn't need a second entry")
		}
	}

	// Throwing keeps a finesse weapon's better ability.
	c := styled(2, style, equipped("dagger"))
	c.Abilities = maps.Clone(c.Abilities)
	c.Abilities[Dex] = AbilityScore{Score: 18}
	if a := attackNamed(t, mustCompute(t, c, lib, Ruleset{}), "Dagger (Thrown)"); a.Ability != Dex || a.DamageBonus != 5 {
		t.Errorf("thrown dagger = %+v", a)
	}

	// Nothing that can't be thrown changes, and nobody else gets extra entries.
	sh = mustCompute(t, styled(2, style, equipped("longsword")), lib, Ruleset{})
	if got := attackNamed(t, sh, "Longsword").DamageBonus; got != 3 {
		t.Errorf("longsword damage bonus %d", got)
	}
	sh = mustCompute(t, styled(2, "paladin_legacy_style_dueling", equipped("javelin")), lib, Ruleset{})
	for _, a := range sh.Attacks {
		if a.Name == "Javelin (Thrown)" {
			t.Error("only Thrown Weapon Fighting adds a thrown entry")
		}
	}
}

func TestMarinerFightingStyle(t *testing.T) {
	lib := loadPaladinLib(t)
	const style = "paladin_legacy_style_mariner"
	sheet := func(inv ...InventoryItem) Sheet {
		c := styled(2, style, inv...)
		c.RaceID = "elf"
		c.Selections = append(c.Selections, Selection{FeatureID: "elf", ChoiceID: "lineage", Picked: []string{"wood"}})
		return mustCompute(t, c, lib, Ruleset{})
	}
	active := func(name string, sh Sheet, ac int) {
		t.Helper()
		if sh.AC != ac || sh.Speeds[SpeedSwim] != 35 || sh.Speeds[SpeedClimb] != 35 {
			t.Errorf("%s: AC %d, speeds %v; want AC %d and swim and climb 35", name, sh.AC, sh.Speeds, ac)
		}
	}
	inactive := func(name string, sh Sheet, ac int) {
		t.Helper()
		if sh.AC != ac || sh.Speeds[SpeedSwim] != 0 || sh.Speeds[SpeedClimb] != 0 {
			t.Errorf("%s: AC %d, speeds %v; want AC %d and no swim or climb", name, sh.AC, sh.Speeds, ac)
		}
	}
	active("no armour", sheet(), 11)
	active("light armour", sheet(equipped("leather_armor")), 12)
	active("medium armour", sheet(equipped("half_plate")), 16)
	inactive("heavy armour", sheet(equipped("chain_mail")), 16)
	inactive("a shield", sheet(equipped("shield")), 12)
}

func TestModifierRequirementsAreValidated(t *testing.T) {
	bad := map[string]string{
		"unknown requirement": `{"target":"ac","kind":"add","value":1,"requires":"moonlight"}`,
		"weapon on ac":        `{"target":"ac","kind":"add","value":1,"requires":"thrown_weapon"}`,
		"weapon with set":     `{"target":"damage.all","kind":"set","value":1,"requires":"melee_weapon"}`,
		"not a speed":         `{"target":"speed.swim","kind":"min","value_speed":"warp"}`,
		"speed on itself":     `{"target":"speed.walk","kind":"min","value_speed":"walk"}`,
		"speed on ac":         `{"target":"ac","kind":"min","value_speed":"walk"}`,
	}
	for name, m := range bad {
		lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{
			"features/f.json": `[{"id":"f","name":"F","modifiers":[` + m + `]}]`,
		}))
		if !lib.HasErrors() {
			t.Errorf("%s should be a content error", name)
		}
	}

	// On a character's own modifiers, an unknown requirement warns and does nothing.
	lib := mustLoad(t, LoadOptions{}, memPack("p", map[string]string{}))
	sh := mustCompute(t, Character{ID: "c", Modifiers: []Modifier{{Target: "ac", Kind: ModAdd, Value: 5, Requires: "moonlight"}}}, lib, Ruleset{})
	if !hasWarning(sh, WarnUnknownRequires) || sh.AC != 10 {
		t.Errorf("AC %d, warnings %+v", sh.AC, sh.Warnings)
	}
}

func TestDivineSmiteIsAnActionAndOnlyOnMeleeWeapons(t *testing.T) {
	lib := loadPaladinLib(t)
	smite := func(sh Sheet) (Action, bool) {
		for _, a := range sh.Actions {
			if a.Name == "Divine Smite" {
				return a, true
			}
		}
		return Action{}, false
	}
	if _, ok := smite(mustCompute(t, paladinAt(1), lib, Ruleset{})); ok {
		t.Error("Divine Smite starts at level 2")
	}
	a, ok := smite(mustCompute(t, paladinAt(2), lib, Ruleset{}))
	if !ok || a.Type != ActionOther || !strings.Contains(a.Description, "max 6d8 total") {
		t.Errorf("Divine Smite action = %+v, found %v", a, ok)
	}

	dice := func(a AttackView) []string {
		out := []string{}
		for _, r := range a.DamageRiders {
			out = append(out, r.Dice)
		}
		slices.Sort(out)
		return out
	}
	c := paladinAt(11)
	c.Inventory = []InventoryItem{equipped("longsword"), equipped("light_crossbow")}
	sh := mustCompute(t, c, lib, Ruleset{})
	if got := dice(attackNamed(t, sh, "Longsword")); !slices.Equal(got, []string{"1d8", "2d8"}) {
		t.Errorf("longsword riders = %v", got)
	}
	for _, name := range []string{"Unarmed Strike", "Light Crossbow"} {
		if got := dice(attackNamed(t, sh, name)); len(got) != 0 {
			t.Errorf("%s riders = %v, want none", name, got)
		}
	}
	// Not when thrown, either.
	styledThrow := styled(11, "paladin_legacy_style_thrown_weapon", equipped("javelin"))
	sh = mustCompute(t, styledThrow, lib, Ruleset{})
	if got := dice(attackNamed(t, sh, "Javelin (Thrown)")); len(got) != 0 {
		t.Errorf("thrown javelin riders = %v", got)
	}
	if got := dice(attackNamed(t, sh, "Javelin")); len(got) != 2 {
		t.Errorf("melee javelin riders = %v", got)
	}
	// Riders from elsewhere still reach every attack they did before.
	c = paladinAt(1)
	c.Modifiers = []Modifier{{Target: TargetDamageMelee, Kind: ModAdd, Dice: "1d4"}}
	if got := dice(attackNamed(t, mustCompute(t, c, lib, Ruleset{}), "Unarmed Strike")); !slices.Equal(got, []string{"1d4"}) {
		t.Errorf("unarmed riders = %v", got)
	}
}
