package character

// itemtags.go - tags on an inventory entry, which vary a weapon or armour without a separate library
// item: a "silvered" longsword or a "+1" shield is the plain item with tags on its entry.
//
//   - +X on a weapon adds X to hit and damage; on armour or a shield it adds X to AC.
//   - silvered has rules text, added to a weapon's description. Other words are just labels (mithral).
//   - resolveInvItem applies them, so the rest of the engine only ever sees the resulting item, and
//     they obey attunement like any other item bonus.

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const WarnBadItemTag WarningCode = "bad_item_tag"

const silveredRules = "Silvered. When you score a Critical Hit with this weapon against a creature that is shape-shifted, the weapon deals one additional die of damage."

var (
	bonusTagRe = regexp.MustCompile(`^\+[1-9][0-9]?$`)
	wordTagRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9_ -]{0,31}$`)
	plusIDRe   = regexp.MustCompile(`^(.+)_plus_([1-9][0-9]?)$`)
)

func takesItemTags(k ItemKind) bool {
	return k == ItemWeapon || k == ItemArmour || k == ItemShield
}

// parseItemTags normalises tags into sorted words and a bonus, and describes any it can't use. Of
// several +X tags the highest counts.
func parseItemTags(tags []string) (words []string, bonus int, problems []string) {
	seen := map[string]bool{}
	for _, raw := range tags {
		t := strings.ToLower(strings.TrimSpace(raw))
		switch {
		case seen[t]:
		case bonusTagRe.MatchString(t):
			n, _ := strconv.Atoi(t[1:])
			if bonus != 0 {
				problems = append(problems, "only one +X tag can apply")
			}
			bonus = max(bonus, n)
		case wordTagRe.MatchString(t):
			words = append(words, t)
		default:
			problems = append(problems, fmt.Sprintf("%q is not a valid tag", raw))
		}
		seen[t] = true
	}
	slices.Sort(words)
	return words, bonus, problems
}

// canonicalTags is the stored form: the bonus, then the words.
func canonicalTags(words []string, bonus int) []string {
	if bonus == 0 {
		return words
	}
	return append([]string{fmt.Sprintf("+%d", bonus)}, words...)
}

// cleanItemTags validates tags sent for an item, returning them in stored form.
func cleanItemTags(it Item, tags []string) ([]string, error) {
	if len(tags) == 0 {
		return nil, nil
	}
	if !takesItemTags(it.Kind) {
		return nil, badReq("only weapons, armour and shields can have tags")
	}
	words, bonus, problems := parseItemTags(tags)
	if len(problems) > 0 {
		return nil, badReq("tags: %s", strings.Join(problems, "; "))
	}
	return canonicalTags(words, bonus), nil
}

// applyItemTags returns the item as this entry's tags make it. The library's copy is left alone.
func applyItemTags(it Item, tags []string) Item {
	if len(tags) == 0 || !takesItemTags(it.Kind) {
		return it
	}
	words, bonus, _ := parseItemTags(tags)
	if it.Name == "" {
		it.Name = it.ID
	}
	it.Name = taggedName(it.Name, words, bonus)

	if bonus != 0 {
		if it.Weapon != nil {
			ws := *it.Weapon
			ws.MagicBonus += bonus
			it.Weapon = &ws
		}
		if it.Kind == ItemArmour || it.Kind == ItemShield {
			// Its own source: under the item's id it would collapse into a shield's own +2 AC.
			it.Modifiers = append(slices.Clone(it.Modifiers),
				Modifier{Target: TargetAC, Kind: ModAdd, Value: bonus, SourceID: "enhancement:" + it.ID})
		}
	}
	if it.Kind == ItemWeapon && slices.Contains(words, "silvered") {
		it.Description = strings.TrimSpace(it.Description + " " + silveredRules)
	}
	return it
}

// taggedName puts words before the name and the bonus after it: "Silvered Longsword +1".
func taggedName(name string, words []string, bonus int) string {
	var b strings.Builder
	for _, w := range words {
		b.WriteString(strings.ToUpper(w[:1]) + w[1:] + " ")
	}
	b.WriteString(name)
	if bonus != 0 {
		fmt.Fprintf(&b, " +%d", bonus)
	}
	return b.String()
}

// itemTagProblem describes what's wrong with an entry's tags, which only a direct edit can cause.
func itemTagProblem(it Item, tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	if !takesItemTags(it.Kind) {
		return fmt.Sprintf("%s has tags, but only weapons, armour and shields use them", it.Name)
	}
	if _, _, problems := parseItemTags(tags); len(problems) > 0 {
		return fmt.Sprintf("%s: %s", it.Name, strings.Join(problems, "; "))
	}
	return ""
}

// upgradeVariantItems turns entries saved when variants were separate library items (silvered_dagger,
// longsword_plus_1) into the plain item with tags.
func upgradeVariantItems(c *Character) {
	for i := range c.Inventory {
		e := &c.Inventory[i]
		if e.Custom != nil {
			continue
		}
		id, tags := e.ItemID, slices.Clone(e.Tags)
		if m := plusIDRe.FindStringSubmatch(id); m != nil {
			id, tags = m[1], append(tags, "+"+m[2])
		}
		if base, ok := strings.CutPrefix(id, "silvered_"); ok {
			id, tags = base, append(tags, "silvered")
		}
		if id != e.ItemID {
			words, bonus, _ := parseItemTags(tags)
			e.ItemID, e.Tags = id, canonicalTags(words, bonus)
		}
	}
}
