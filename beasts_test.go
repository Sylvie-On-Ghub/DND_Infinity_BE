package character

import "testing"

func TestWildShapeBeasts(t *testing.T) {
	lib := loadWarlockLib(t)
	count, flyLow, swim := 0, 0, 0
	byID := map[string]Creature{}
	for _, c := range lib.Creatures() {
		byID[c.ID] = c
		tagged := false
		for _, tg := range c.Tags {
			if tg == "wild_shape" {
				tagged = true
			}
		}
		if !tagged {
			continue
		}
		count++
		if c.Type != "beast" {
			t.Errorf("%s is tagged wild_shape but has type %q", c.ID, c.Type)
		}
		v, ok := parseCR(c.CR)
		if !ok {
			t.Errorf("%s has an unparseable cr %q", c.ID, c.CR)
		}
		if c.Speeds[SpeedFly] > 0 {
			if ok && v <= 1 {
				flyLow++
			}
		}
		if c.Speeds[SpeedSwim] > 0 {
			swim++
		}
	}
	if count < 40 {
		t.Errorf("want at least 40 wild_shape beasts, got %d", count)
	}
	if flyLow < 1 {
		t.Errorf("want a flying beast of CR 1 or less")
	}
	if swim < 3 {
		t.Errorf("want at least 3 swimming beasts, got %d", swim)
	}
	want := map[string]string{"rat": "0", "spider": "0", "riding_horse": "1/4", "wolf": "1/4", "brown_bear": "1",
		"giant_spider": "1", "polar_bear": "2", "killer_whale": "3", "elephant": "4", "giant_shark": "5", "mammoth": "6"}
	for id, cr := range want {
		c, ok := byID[id]
		if !ok {
			t.Errorf("creature %s missing", id)
			continue
		}
		if c.CR != cr {
			t.Errorf("%s cr = %q, want %q", id, c.CR, cr)
		}
	}
}
