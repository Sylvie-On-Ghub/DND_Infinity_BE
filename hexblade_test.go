package character

// hexblade_test.go - the Hexblade warlock subclass: its curse form, Hex Warrior proficiencies and
// patron spells, with the old 1st-level features moved to warlock level 3.

import (
	"slices"
	"testing"
)

func hexbladeFixture() Character {
	c := warlockFixture()
	c.Classes[0].SubclassID = "hexblade"
	return c
}

func hexbladeAt(level int) Character { return warlockAt(level, "hexblade") }
func hasFeature(sh Sheet, id string) bool {
	_, ok := findFeature(sh, id)
	return ok
}

func TestHexbladeFeaturesStartAtLevelThree(t *testing.T) {
	lib := loadWarlockLib(t)
	at2 := mustCompute(t, hexbladeAt(2), lib, Ruleset{})
	for _, id := range []string{"hexblades_curse", "hex_warrior"} {
		if hasFeature(at2, id) {
			t.Errorf("%s should not appear before warlock level 3", id)
		}
	}
	if slices.Contains(at2.ArmourProfs, "medium") || slices.Contains(at2.WeaponProfs, "martial") {
		t.Errorf("Hex Warrior proficiencies granted at level 2: armour %v, weapons %v", at2.ArmourProfs, at2.WeaponProfs)
	}

	sh := mustCompute(t, hexbladeFixture(), lib, Ruleset{})
	if _, ok := findPending(sh, "class:warlock", "subclass"); ok {
		t.Error("subclass should not be pending once hexblade is chosen")
	}
	for _, id := range []string{"hexblades_curse", "hex_warrior"} {
		if !hasFeature(sh, id) {
			t.Errorf("%s missing at warlock level 4", id)
		}
	}
	for _, p := range []string{"medium", "shield"} {
		if !slices.Contains(sh.ArmourProfs, p) {
			t.Errorf("armour profs %v lack %s", sh.ArmourProfs, p)
		}
	}
	if !slices.Contains(sh.WeaponProfs, "martial") {
		t.Errorf("weapon profs %v lack martial", sh.WeaponProfs)
	}

	at14 := mustCompute(t, hexbladeAt(14), lib, Ruleset{})
	for _, id := range []string{"accursed_specter", "armor_of_hexes", "master_of_hexes"} {
		if !hasFeature(at14, id) {
			t.Errorf("%s missing at warlock level 14", id)
		}
	}
}

func TestHexbladesCurse(t *testing.T) {
	lib := loadWarlockLib(t)
	c := hexbladeFixture()
	sh := mustCompute(t, c, lib, Ruleset{})

	res, _ := findResource(sh, "hexblades_curse")
	if res.Max != 1 || res.Recharge != RechargeShortRest {
		t.Errorf("curse resource = %+v, want 1 use per short rest", res)
	}

	curseNote := func(sh Sheet) (Modifier, bool) {
		for _, m := range sh.Notes {
			if m.Target == TargetDamageAll && m.Condition == "against the cursed target" {
				return m, true
			}
		}
		return Modifier{}, false
	}
	if m, ok := curseNote(sh); ok {
		t.Errorf("curse damage bonus shown while the curse is off: %+v", m)
	}

	// On: the bonus equals the proficiency bonus, shown as situational, and attacks don't absorb it.
	c.ActiveForms = []ActiveForm{{ID: "hexblades_curse"}}
	on := mustCompute(t, c, lib, Ruleset{})
	m, ok := curseNote(on)
	if !ok || m.Value != on.ProficiencyBonus {
		t.Errorf("curse note = %+v (ok=%v), want +%d damage against the cursed target", m, ok, on.ProficiencyBonus)
	}
	for i, a := range on.Attacks {
		if a.DamageBonus != sh.Attacks[i].DamageBonus {
			t.Errorf("%s damage bonus %d with the curse on, %d off: the bonus is situational", a.Name, a.DamageBonus, sh.Attacks[i].DamageBonus)
		}
	}
}

func TestHexbladePatronSpells(t *testing.T) {
	lib := loadWarlockLib(t)
	granted := func(level int) map[string]bool {
		sh := mustCompute(t, hexbladeAt(level), lib, Ruleset{})
		src, ok := findSource(sh, "class:warlock")
		if !ok {
			t.Fatalf("no warlock spell source at level %d", level)
		}
		out := map[string]bool{}
		for _, sp := range src.Spells {
			if sp.Granted {
				out[sp.ID] = true
			}
		}
		return out
	}

	if g := granted(2); len(g) != 0 {
		t.Errorf("hexblade patron spells granted at level 2: %v", g)
	}
	steps := []struct {
		level int
		ids   []string
	}{
		{3, []string{"shield", "wrathful_smite", "blur", "shining_smite"}},
		{5, []string{"blink", "elemental_weapon"}},
		{7, []string{"phantasmal_killer", "staggering_smite"}},
		{9, []string{"banishing_smite", "cone_of_cold"}},
	}
	for i, st := range steps {
		g := granted(st.level)
		for _, id := range st.ids {
			if !g[id] {
				t.Errorf("%s not granted at warlock level %d", id, st.level)
			}
		}
		if i+1 < len(steps) {
			if below := granted(st.level - 1); below[st.ids[0]] {
				t.Errorf("%s granted early, at level %d", st.ids[0], st.level-1)
			}
		}
	}
}
