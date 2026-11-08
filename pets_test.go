package character

// pets_test.go - tests for pets: owner modifiers aimed at them and their computed stat blocks.

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestPetModifiersDontTouchTheOwner(t *testing.T) {
	lib := fakeLib{features: map[string]Feature{"invest": {ID: "invest", Name: "Invest", Modifiers: []Modifier{
		{Target: PetTarget("familiar", SpeedTarget(SpeedFly)), Kind: ModMin, Value: 40, SourceID: "invest"},
		{Target: PetTarget("familiar", TargetAC), Kind: ModAdd, Value: 1, SourceID: "invest"},
	}}}}
	c := newChar(1, nil)
	c.FeatureIDs = []string{"invest"}
	sh := mustCompute(t, c, lib, Ruleset{})
	if sh.Speeds[SpeedFly] != 0 || sh.AC != 10 {
		t.Errorf("owner fly %d, AC %d; want 0 and 10", sh.Speeds[SpeedFly], sh.AC)
	}
	if w, ok := findWarning(sh, WarnUnknownTarget); ok {
		t.Errorf("pet targets should be recognised: %+v", w)
	}
	if len(sh.petMods) != 2 {
		t.Errorf("petMods = %+v, want 2", sh.petMods)
	}
}

// petLib has an owl (a familiar), a Primal-style beast that scales with its owner, and a Wisdom
// caster class "test" whose level 1 feature grants the beast.
func petLib() fakeLib {
	owlTo := Formula{Terms: []Term{{Source: "mod.dex"}, {Source: "pb"}}}
	var fiveLevels []Term
	for i := 0; i < 5; i++ {
		fiveLevels = append(fiveLevels, Term{Source: "owner.class_level.test"})
	}
	levels := make([][]string, 20)
	levels[0] = []string{"companion_feat"}
	return fakeLib{
		classes: map[string]Class{"test": {ID: "test", Name: "Test", HitDie: 8, Levels: levels,
			Spellcasting: &SpellcastingDef{Ability: Wis, Divisor: 1}}},
		features: map[string]Feature{
			"companion_feat": {ID: "companion_feat", Name: "Companion", Origin: OriginClass, SourceID: "test",
				Pets: &PetGrant{Tags: []string{"primal_companion"}}},
		},
		spells: map[string]Spell{"find_familiar": {ID: "find_familiar", Name: "Find Familiar", Level: 1,
			Pets: &PetGrant{Tags: []string{"standard_familiar"}}}},
		creatures: map[string]Creature{
			"owl": {ID: "owl", Name: "Owl", Size: "Tiny", Type: "beast", Tags: []string{"familiar", "standard_familiar"},
				Abilities: map[Ability]int{Str: 3, Dex: 13, Con: 8, Int: 2, Wis: 12, Cha: 7},
				AC:        Formula{Base: 11}, HP: Formula{Base: 1},
				Speeds: map[SpeedType]int{SpeedWalk: 5, SpeedFly: 60},
				Skills: map[string]ProficiencyLevel{"perception": ProfTrained},
				Attacks: []CreatureAttack{{Name: "Talons", Type: ActionAction, Reach: 5, ToHit: owlTo,
					Damage: Formula{Base: 1}, DamageType: "slashing"}}},
			"beast_land": {ID: "beast_land", Name: "Beast of the Land", Size: "Medium", Type: "beast",
				Tags:      []string{"primal_companion"},
				Abilities: map[Ability]int{Str: 14, Dex: 14, Con: 15, Int: 8, Wis: 14, Cha: 11},
				AC:        Formula{Base: 13, Terms: []Term{{Source: "owner.mod.wis"}}},
				HP:        Formula{Base: 5, Terms: fiveLevels},
				PB:        &Formula{Terms: []Term{{Source: "owner.pb"}}},
				Speeds:    map[SpeedType]int{SpeedWalk: 40},
				Attacks: []CreatureAttack{{Name: "Beast's Strike", Type: ActionAction, Reach: 5,
					ToHit:      Formula{Terms: []Term{{Source: "owner.spell_attack"}}},
					DamageDice: "1d8", Damage: Formula{Base: 2, Terms: []Term{{Source: "owner.mod.wis"}}},
					DamageType: "piercing"}}},
		},
	}
}

// ranger builds a level n "test" class owner with Wisdom 16 and the given pets.
func ranger(n int, pets ...Pet) Character {
	c := newChar(n, map[Ability]int{Wis: 16})
	c.Pets = pets
	return c
}

func petOf(t *testing.T, sh Sheet, id string) PetView {
	t.Helper()
	for _, p := range sh.Pets {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("no pet %q on the sheet: %+v", id, sh.Pets)
	return PetView{}
}

func hasPetWarning(p PetView, code WarningCode) bool {
	for _, w := range p.Warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}

func findSourceView(sh Sheet, id string) SpellSourceView {
	for _, s := range sh.Spellcasting {
		if s.ID == id {
			return s
		}
	}
	return SpellSourceView{}
}

func TestPrimalCompanionScalesWithOwner(t *testing.T) {
	beast := Pet{ID: "b", CreatureID: "beast_land", SourceID: "companion_feat", Status: PetActive, CurrentHP: 20}
	sh := mustCompute(t, ranger(3, beast), petLib(), Ruleset{})
	p := petOf(t, sh, "b")
	if p.AC != 16 || p.MaxHP != 20 || p.PB != 2 {
		t.Errorf("level 3: AC %d, max HP %d, PB %d; want 16, 20, 2", p.AC, p.MaxHP, p.PB)
	}
	src := findSourceView(sh, "class:test")
	if len(p.Attacks) != 1 || p.Attacks[0].ToHit != src.AttackBonus || p.Attacks[0].Damage != "1d8+5 piercing" {
		t.Errorf("attacks = %+v, want to-hit %d and 1d8+5 piercing", p.Attacks, src.AttackBonus)
	}
	if p.Attacks[0].Range != "reach 5 ft." {
		t.Errorf("range = %q", p.Attacks[0].Range)
	}

	sh = mustCompute(t, ranger(7, beast), petLib(), Ruleset{})
	if p := petOf(t, sh, "b"); p.MaxHP != 40 || p.PB != 3 {
		t.Errorf("level 7: max HP %d, PB %d; want 40 and 3", p.MaxHP, p.PB)
	}
}

func TestOwlStatBlock(t *testing.T) {
	owl := Pet{ID: "o", Name: "Hoot", CreatureID: "owl", SourceID: "find_familiar", Status: PetActive, CurrentHP: 1}
	p := petOf(t, mustCompute(t, ranger(1, owl), petLib(), Ruleset{}), "o")
	if p.Name != "Hoot" || p.CreatureName != "Owl" || p.Size != "Tiny" || p.Type != "beast" {
		t.Errorf("identity = %+v", p)
	}
	if p.Attacks[0].ToHit != 3 || p.Attacks[0].Damage != "1 slashing" {
		t.Errorf("talons = %+v, want +3 and 1 slashing", p.Attacks[0])
	}
	if p.Speeds[SpeedWalk] != 5 || p.Speeds[SpeedFly] != 60 {
		t.Errorf("speeds = %v", p.Speeds)
	}
	if p.Skills["perception"].Bonus != 3 || len(p.Skills) != 1 {
		t.Errorf("skills = %+v, want perception +3 only", p.Skills)
	}
	if p.Abilities[Dex].Mod != 1 || p.Abilities[Str].Mod != -4 {
		t.Errorf("abilities = %+v", p.Abilities)
	}
}

func TestOwnerPetModifiersApplyByTag(t *testing.T) {
	lib := petLib()
	lib.features["invest"] = Feature{ID: "invest", Name: "Invest", Modifiers: []Modifier{
		{Target: PetTarget("familiar", SpeedTarget(SpeedSwim)), Kind: ModMin, Value: 40, SourceID: "invest"},
		{Target: PetTarget("familiar", "save.all"), Kind: ModNote, Note: "Uses your save DC ({owner.spell_save_dc})", SourceID: "invest"},
	}}
	c := ranger(3,
		Pet{ID: "o", CreatureID: "owl", SourceID: "find_familiar", Status: PetActive, CurrentHP: 1},
		Pet{ID: "b", CreatureID: "beast_land", SourceID: "companion_feat", Status: PetActive, CurrentHP: 20})
	c.FeatureIDs = []string{"invest"}
	c.Spellbooks = map[string]SpellState{"class:test": {Known: []string{"find_familiar"}}} // the familiar uses this source's DC
	sh := mustCompute(t, c, lib, Ruleset{})
	owl, beast := petOf(t, sh, "o"), petOf(t, sh, "b")
	if owl.Speeds[SpeedSwim] != 40 || len(owl.Notes) != 1 || owl.Notes[0].Note != "Uses your save DC (13)" {
		t.Errorf("owl speeds %v, notes %+v", owl.Speeds, owl.Notes)
	}
	if beast.Speeds[SpeedSwim] != 0 || len(beast.Notes) != 0 {
		t.Errorf("beast should be untouched: speeds %v, notes %+v", beast.Speeds, beast.Notes)
	}
}

func TestPetAdHocModifiers(t *testing.T) {
	owl := Pet{ID: "o", CreatureID: "owl", SourceID: "find_familiar", Status: PetActive, CurrentHP: 1,
		Modifiers: []Modifier{{Target: TargetAC, Kind: ModAdd, Value: 2}}}
	if p := petOf(t, mustCompute(t, ranger(1, owl), petLib(), Ruleset{}), "o"); p.AC != 13 {
		t.Errorf("AC = %d, want 13", p.AC)
	}
}

func TestPetWithUnknownCreatureStillShows(t *testing.T) {
	gone := Pet{ID: "g", Name: "Ghost", CreatureID: "gone", SourceID: "find_familiar", Status: PetActive,
		CurrentHP: 4, Conditions: []Condition{"prone"}}
	sh := mustCompute(t, ranger(1, gone), petLib(), Ruleset{})
	if len(sh.Pets) != 1 {
		t.Fatalf("pets = %+v, want 1", sh.Pets)
	}
	p := sh.Pets[0]
	if p.Name != "Ghost" || p.CurrentHP != 4 || len(p.Conditions) != 1 || !hasPetWarning(p, WarnUnknownID) {
		t.Errorf("pet = %+v, want its play state and an unknown_id warning", p)
	}
}

func TestSheetPetsIsEmptyNotNull(t *testing.T) {
	sh := mustCompute(t, ranger(1), petLib(), Ruleset{})
	data, _ := json.Marshal(sh)
	if !strings.Contains(string(data), `"pets":[]`) {
		t.Error(`a sheet with no pets should have "pets":[]`)
	}
}

// ---------------------------------------------------------------------------
// Warnings
// ---------------------------------------------------------------------------

var petWarningCodes = []WarningCode{WarnPetSourceMissing, WarnPetNotAllowed, WarnPetTooMany, WarnPetOverMaxHP, WarnPetNoSpellcasting}

// wantOnlyPetWarning checks the pet has this pet warning and none of the other pet warning codes.
func wantOnlyPetWarning(t *testing.T, p PetView, want WarningCode) {
	t.Helper()
	for _, code := range petWarningCodes {
		if has := hasPetWarning(p, code); has != (code == want) {
			t.Errorf("pet %s: %s present = %v; warnings %+v", p.ID, code, has, p.Warnings)
		}
	}
}

// caster is a level 3 "test" owner who knows Find Familiar.
func caster(pets ...Pet) Character {
	c := ranger(3, pets...)
	c.Spellbooks = map[string]SpellState{"class:test": {Known: []string{"find_familiar"}}}
	return c
}

func impLib() fakeLib {
	lib := petLib()
	lib.creatures["imp"] = Creature{ID: "imp", Name: "Imp", Tags: []string{"familiar", "chain_familiar"},
		Abilities: map[Ability]int{Dex: 17}, AC: Formula{Base: 13}, HP: Formula{Base: 21}}
	return lib
}

func TestPetWarnsWhenOwnerLacksItsSource(t *testing.T) {
	imp := Pet{ID: "i", CreatureID: "imp", SourceID: "pact_of_the_chain", Status: PetActive, CurrentHP: 21}
	p := petOf(t, mustCompute(t, caster(imp), impLib(), Ruleset{}), "i")
	wantOnlyPetWarning(t, p, WarnPetSourceMissing)

	// A companion whose owner has lost the feature and has no spellcasting at all still computes.
	c := newChar(3, map[Ability]int{Wis: 16})
	c.Classes = []ClassLevel{cls("other", 3)}
	c.Pets = []Pet{{ID: "b", CreatureID: "beast_land", SourceID: "companion_feat", Status: PetActive, CurrentHP: 5}}
	b := petOf(t, mustCompute(t, c, impLib(), Ruleset{}), "b")
	if !hasPetWarning(b, WarnPetSourceMissing) || !hasPetWarning(b, WarnPetNoSpellcasting) || b.Attacks[0].ToHit != 0 {
		t.Errorf("orphaned companion: to-hit %d, warnings %+v", b.Attacks[0].ToHit, b.Warnings)
	}
}

func TestPetWarnsWhenCreatureNotAllowed(t *testing.T) {
	imp := Pet{ID: "i", CreatureID: "imp", SourceID: "find_familiar", Status: PetActive, CurrentHP: 21}
	wantOnlyPetWarning(t, petOf(t, mustCompute(t, caster(imp), impLib(), Ruleset{}), "i"), WarnPetNotAllowed)

	lib := impLib()
	ff := lib.spells["find_familiar"]
	ff.Pets = &PetGrant{CreatureIDs: []string{"imp"}}
	lib.spells["find_familiar"] = ff
	wantOnlyPetWarning(t, petOf(t, mustCompute(t, caster(imp), lib, Ruleset{}), "i"), "")

	ff.Pets = nil
	lib.spells["find_familiar"] = ff
	wantOnlyPetWarning(t, petOf(t, mustCompute(t, caster(imp), lib, Ruleset{}), "i"), WarnPetNotAllowed)
}

func TestOneActivePetPerSource(t *testing.T) {
	owl := func(id string, s PetStatus) Pet {
		return Pet{ID: id, CreatureID: "owl", SourceID: "find_familiar", Status: s, CurrentHP: 1}
	}
	sh := mustCompute(t, caster(owl("a", PetActive), owl("b", PetActive)), petLib(), Ruleset{})
	wantOnlyPetWarning(t, petOf(t, sh, "a"), WarnPetTooMany)
	wantOnlyPetWarning(t, petOf(t, sh, "b"), WarnPetTooMany)

	sh = mustCompute(t, caster(owl("a", PetActive), owl("b", PetDismissed)), petLib(), Ruleset{})
	wantOnlyPetWarning(t, petOf(t, sh, "a"), "")
	wantOnlyPetWarning(t, petOf(t, sh, "b"), "")
}

func TestPetWarnsAboveMaxHP(t *testing.T) {
	owl := Pet{ID: "o", CreatureID: "owl", SourceID: "find_familiar", Status: PetActive, CurrentHP: 5}
	wantOnlyPetWarning(t, petOf(t, mustCompute(t, caster(owl), petLib(), Ruleset{}), "o"), WarnPetOverMaxHP)
}

func TestFamiliarInAWizardsBookCountsAsTheSource(t *testing.T) {
	lib := petLib()
	lib.classes["book"] = Class{ID: "book", Name: "Book", HitDie: 6, Levels: make([][]string, 20),
		Spellcasting: &SpellcastingDef{Ability: Int, Divisor: 1, Prep: PrepBook}}
	c := newChar(1, map[Ability]int{Int: 16})
	c.Classes = []ClassLevel{cls("book", 1)}
	c.Spellbooks = map[string]SpellState{"class:book": {Book: []string{"find_familiar"}}} // in the book, not prepared
	c.Pets = []Pet{{ID: "o", CreatureID: "owl", SourceID: "find_familiar", Status: PetActive, CurrentHP: 1}}
	wantOnlyPetWarning(t, petOf(t, mustCompute(t, c, lib, Ruleset{}), "o"), "")
}

func TestPetWarningsCanBeAcknowledged(t *testing.T) {
	c := caster(Pet{ID: "o", CreatureID: "owl", SourceID: "find_familiar", Status: PetActive, CurrentHP: 5})
	w := petOf(t, mustCompute(t, c, petLib(), Ruleset{}), "o").Warnings[0]
	c.Acknowledgements = []Acknowledgement{{Key: w.Key, Note: "DM approved"}}
	got := petOf(t, mustCompute(t, c, petLib(), Ruleset{}), "o").Warnings[0]
	if !got.Acknowledged || got.AckNote != "DM approved" {
		t.Errorf("pet warning = %+v, want acknowledged with the note", got)
	}
}

func TestOwnerNumbersFillCreatureText(t *testing.T) {
	lib := petLib()
	beast := lib.creatures["beast_land"]
	beast.Traits = []Trait{{Name: "Bond", Description: "Escape DC {owner.spell_save_dc}."}}
	beast.Attacks[0].Description = "Grappled (escape DC {owner.spell_save_dc})."
	beast.Actions = []Action{{Name: "Shove", Type: ActionBonusAction, Description: "Uses {owner.spell_attack}."}}
	lib.creatures["beast_land"] = beast
	c := ranger(3, Pet{ID: "b", CreatureID: "beast_land", SourceID: "companion_feat", Status: PetActive, CurrentHP: 20})
	p := petOf(t, mustCompute(t, c, lib, Ruleset{}), "b")
	if p.Traits[0].Description != "Escape DC 13." || p.Attacks[0].Description != "Grappled (escape DC 13)." || p.Actions[0].Description != "Uses +5." {
		t.Errorf("trait %q, attack %q, action %q", p.Traits[0].Description, p.Attacks[0].Description, p.Actions[0].Description)
	}
	if lib.creatures["beast_land"].Traits[0].Description != "Escape DC {owner.spell_save_dc}." {
		t.Error("filling in the sheet must not change the library's creature")
	}
}

// ---------------------------------------------------------------------------
// Pet sources
// ---------------------------------------------------------------------------

func petSourceOf(t *testing.T, sh Sheet, id string) PetSourceView {
	t.Helper()
	for _, s := range sh.PetSources {
		if s.SourceID == id {
			return s
		}
	}
	t.Fatalf("no pet source %q: %+v", id, sh.PetSources)
	return PetSourceView{}
}

func creatureIDs(s PetSourceView) []string {
	var out []string
	for _, c := range s.Creatures {
		out = append(out, c.ID)
	}
	return out
}

func TestPetSourcesListWhatTheCharacterCanSummon(t *testing.T) {
	lib := impLib()
	lib.classes["book"] = Class{ID: "book", Name: "Book", HitDie: 6, Levels: make([][]string, 20),
		Spellcasting: &SpellcastingDef{Ability: Int, Divisor: 1, Prep: PrepBook}}
	c := newChar(1, map[Ability]int{Int: 16})
	c.Classes = []ClassLevel{cls("book", 1)}
	c.Spellbooks = map[string]SpellState{"class:book": {Book: []string{"find_familiar"}}}
	c.Pets = []Pet{
		{ID: "a", CreatureID: "owl", SourceID: "find_familiar", Status: PetActive, CurrentHP: 1},
		{ID: "b", CreatureID: "owl", SourceID: "find_familiar", Status: PetDismissed, CurrentHP: 1},
	}
	sh := mustCompute(t, c, lib, Ruleset{})
	if len(sh.PetSources) != 1 {
		t.Fatalf("pet sources = %+v, want just Find Familiar", sh.PetSources)
	}
	ff := petSourceOf(t, sh, "find_familiar")
	if ff.Name != "Find Familiar" || ff.Kind != "spell" || ff.Active != 1 {
		t.Errorf("find familiar source = %+v, want a spell with 1 active pet", ff)
	}
	if ids := creatureIDs(ff); !slices.Equal(ids, []string{"owl"}) || ff.Creatures[0].Name != "Owl" {
		t.Errorf("find familiar creatures = %+v, want the owl only", ff.Creatures)
	}
}

func TestPetSourcesFromFeatures(t *testing.T) {
	lib := impLib()
	lib.features["chain"] = Feature{ID: "chain", Name: "Chain", Pets: &PetGrant{Tags: []string{"chain_familiar"}, CreatureIDs: []string{"owl"}}}
	c := caster()
	c.FeatureIDs = []string{"chain"}
	sh := mustCompute(t, c, lib, Ruleset{})

	chain := petSourceOf(t, sh, "chain")
	if chain.Kind != "feature" || !slices.Equal(creatureIDs(chain), []string{"imp", "owl"}) {
		t.Errorf("chain source = %+v, want a feature allowing imp and owl", chain)
	}
	if ids := creatureIDs(petSourceOf(t, sh, "find_familiar")); !slices.Equal(ids, []string{"owl"}) {
		t.Errorf("find familiar = %v, want owl", ids)
	}
	if ids := creatureIDs(petSourceOf(t, sh, "companion_feat")); !slices.Equal(ids, []string{"beast_land"}) {
		t.Errorf("companion = %v, want beast_land", ids)
	}
}

func TestPetSourcesIsEmptyNotNull(t *testing.T) {
	sh := mustCompute(t, newChar(1, nil), fakeLib{}, Ruleset{}) // petLib's test class grants a companion
	data, _ := json.Marshal(sh)
	if !strings.Contains(string(data), `"pet_sources":[]`) {
		t.Error(`a character with no pet sources should have "pet_sources":[]`)
	}
}
