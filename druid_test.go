package character

// druid_test.go - tests for the Druid in content/classes. It needs only that pack and content/example.

import "testing"

func loadDruidLib(t *testing.T) *ContentLibrary {
	t.Helper()
	return loadWarlockLib(t)
}

func TestDruidSpellList(t *testing.T) {
	lib := loadDruidLib(t)
	list, err := lib.SpellList("druid")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.SpellIDs) != 134 {
		t.Errorf("druid list has %d spells, want 134", len(list.SpellIDs))
	}
	have := map[string]bool{}
	for _, id := range list.SpellIDs {
		have[id] = true
		if _, err := lib.Spell(id); err != nil {
			t.Errorf("druid spell %q does not resolve", id)
		}
	}
	for _, id := range []string{"cure_wounds_2024", "moonbeam", "call_lightning", "shapechange"} {
		if !have[id] {
			t.Errorf("druid list missing %q", id)
		}
	}
}
