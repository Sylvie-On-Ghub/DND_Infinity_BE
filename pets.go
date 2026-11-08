package character

// pets.go - computing pets onto the sheet.
//
// A pet's stat block comes from its library Creature. Creature formulas are evaluated with the owner's
// finished sheet as context (owner.* terms), so a Primal Companion grows with its ranger. Modifiers on a
// pet come from, in order: the creature itself, the owner's pet.<tag>.* modifiers for a tag the creature
// has, and the pet's own ad hoc modifiers. Rules problems are warnings on the pet, never errors.

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// petViews computes every pet, then warns where one source has more than one active pet
// (one familiar at a time).
func petViews(c Character, sh Sheet, lib Library, skillDefs []SkillDef) []PetView {
	out := []PetView{}
	active := map[string]int{}
	for _, p := range c.Pets {
		out = append(out, petView(p, c, sh, lib, skillDefs))
		if p.Status == PetActive {
			active[p.SourceID]++
		}
	}
	for i, v := range out {
		if v.Status == PetActive && active[v.SourceID] > 1 {
			out[i].Warnings = append(out[i].Warnings, mkWarning(WarnPetTooMany, v.ID, "", "", v.SourceID,
				fmt.Sprintf("%d pets from %q are active at once; it allows one", active[v.SourceID], v.SourceID)))
		}
	}
	return out
}

// petView computes one pet against its owner's finished sheet.
func petView(p Pet, c Character, sh Sheet, lib Library, skillDefs []SkillDef) PetView {
	v := PetView{
		ID: p.ID, Name: p.Name, CreatureID: p.CreatureID, SourceID: p.SourceID, Status: p.Status,
		CurrentHP: p.CurrentHP, TempHP: p.TempHP,
		Abilities: map[Ability]AbilityView{}, Speeds: map[SpeedType]int{}, Senses: []Sense{},
		Skills: map[string]SkillView{}, Resistances: []DamageType{}, Immunities: []DamageType{},
		Languages: []string{}, Traits: []Trait{}, Attacks: []PetAttackView{}, Actions: []Action{},
		Conditions: append([]Condition{}, p.Conditions...), Notes: []Modifier{}, Warnings: []Warning{},
	}
	cr, err := lib.Creature(p.CreatureID)
	if err != nil {
		v.Warnings = append(v.Warnings, mkWarning(WarnUnknownID, p.ID, "", "", p.CreatureID,
			fmt.Sprintf("pet %q is a %q, which isn't in the library", petLabel(p), p.CreatureID)))
		return v
	}
	v.CreatureName, v.Size, v.Type = cr.Name, cr.Size, cr.Type
	if !ownerHasSource(sh, p.SourceID) {
		v.Warnings = append(v.Warnings, mkWarning(WarnPetSourceMissing, p.ID, "", "", p.SourceID,
			fmt.Sprintf("%s comes from %q, which the character doesn't have", petLabel(p), p.SourceID)))
	} else if !petAllowed(petGrantOf(lib, p.SourceID), cr) {
		v.Warnings = append(v.Warnings, mkWarning(WarnPetNotAllowed, p.ID, "", "", p.CreatureID,
			fmt.Sprintf("%q doesn't allow a %s as a pet", p.SourceID, cr.Name)))
	}
	v.Languages = append(v.Languages, cr.Languages...)
	v.Traits = append(v.Traits, cr.Traits...)
	v.Actions = append(v.Actions, cr.Actions...)

	owner := ownerContext(c, sh, lib, p.SourceID)
	var badTerms []string
	eval := func(f Formula) int {
		n, bad := evalFormula(f, formulaCtx{modOf: map[Ability]int{}, owner: &owner})
		badTerms = append(badTerms, bad...)
		return n
	}

	// The pet's own scores and proficiency bonus come first: its other formulas read them.
	modOf := map[Ability]int{}
	for _, a := range []Ability{Str, Dex, Con, Int, Wis, Cha} {
		score, ok := cr.Abilities[a]
		if !ok {
			score = 10
		}
		modOf[a] = floorMod(score)
	}
	pb := 2
	if cr.PB != nil {
		pb = eval(*cr.PB)
	}
	ctx := formulaCtx{modOf: modOf, pb: pb, owner: &owner}
	eval = func(f Formula) int {
		n, bad := evalFormula(f, ctx)
		badTerms = append(badTerms, bad...)
		return n
	}
	v.PB = pb

	mods := append([]Modifier{}, cr.Modifiers...)
	for _, m := range sh.petMods {
		tag, inner, _ := splitPetTarget(m.Target)
		if slices.Contains(cr.Tags, tag) {
			m.Target = inner
			m.Note = fillOwnerPlaceholders(m.Note, owner)
			mods = append(mods, m)
		}
	}
	mods = dedupe(append(mods, p.Modifiers...))
	k := newCalc(mods)

	for _, a := range []Ability{Str, Dex, Con, Int, Wis, Cha} {
		score := cr.Abilities[a]
		if score == 0 {
			score = 10
		}
		prof := slices.Contains(cr.Saves, a)
		save := modOf[a] + k.sum(SaveTarget(a), TargetSaveAll)
		if prof {
			save += pb
		}
		v.Abilities[a] = AbilityView{Score: score, Mod: modOf[a], SaveBonus: save, SaveProf: prof}
	}

	v.AC = k.apply(TargetAC, eval(cr.AC))
	v.MaxHP = k.apply(TargetMaxHP, eval(cr.HP))

	for _, s := range []SpeedType{SpeedWalk, SpeedFly, SpeedSwim, SpeedClimb, SpeedBurrow} {
		if n := k.apply(SpeedTarget(s), cr.Speeds[s]); n > 0 {
			v.Speeds[s] = n
		}
	}
	for _, s := range cr.Senses {
		v.Senses = append(v.Senses, Sense{Name: s.Name, Range: k.apply(SenseTarget(s.Name), s.Range)})
	}

	abilityOf := map[string]Ability{}
	for _, sd := range skillDefs {
		abilityOf[sd.ID] = sd.DefaultAbility
	}
	for _, id := range sortedKeys(cr.Skills) {
		lvl := cr.Skills[id]
		a := abilityOf[id]
		bonus := modOf[a] + k.sum(SkillTarget(id), CheckTarget(a), TargetCheckAll)
		switch lvl {
		case ProfTrained:
			bonus += pb
		case ProfExpert:
			bonus += 2 * pb
		case ProfHalf:
			bonus += pb / 2
		}
		v.Skills[id] = SkillView{Ability: a, Level: lvl, Bonus: bonus, Passive: 10 + bonus,
			Advantage:    k.flag(ModAdvantage, SkillTarget(id), CheckTarget(a), TargetCheckAll),
			Disadvantage: k.flag(ModDisadvantage, SkillTarget(id), CheckTarget(a), TargetCheckAll)}
	}

	v.Resistances = mergeDamageTypes(cr.Resistances, k.damageList("resist."))
	v.Immunities = mergeDamageTypes(cr.Immunities, k.damageList("immune."))

	for _, a := range cr.Attacks {
		v.Attacks = append(v.Attacks, PetAttackView{
			Name: a.Name, Type: a.Type, Range: attackRange(a), Description: a.Description,
			ToHit:  eval(a.ToHit) + k.sum(TargetAttackAll),
			Damage: damageText(a.DamageDice, eval(a.Damage)+k.sum(TargetDamageAll), a.DamageType),
		})
	}

	for _, m := range mods {
		if m.Kind == ModNote || m.Condition != "" || m.Dice != "" || m.Kind == ModAdvantage || m.Kind == ModDisadvantage {
			v.Notes = append(v.Notes, m)
		}
	}

	if p.CurrentHP > v.MaxHP {
		v.Warnings = append(v.Warnings, mkWarning(WarnPetOverMaxHP, p.ID, "", "", fmt.Sprint(p.CurrentHP),
			fmt.Sprintf("%s has %d HP, above its maximum of %d", petLabel(p), p.CurrentHP, v.MaxHP)))
	}
	for _, src := range uniqueStrings(badTerms) {
		if src == "owner.spell_attack" || src == "owner.spell_save_dc" {
			v.Warnings = append(v.Warnings, mkWarning(WarnPetNoSpellcasting, p.ID, "", "", src,
				fmt.Sprintf("%s uses its owner's %s, but %q gives the character no spellcasting (counted as 0)",
					petLabel(p), strings.TrimPrefix(src, "owner."), p.SourceID)))
			continue
		}
		v.Warnings = append(v.Warnings, mkWarning(WarnUnknownTerm, p.ID, "", "", src,
			fmt.Sprintf("%s's stat block has a term %q that can't be worked out (counted as 0)", petLabel(p), src)))
	}
	return v
}

// ownerHasSource reports whether the character has the feature, or can cast the spell, that grants a pet.
func ownerHasSource(sh Sheet, sourceID string) bool {
	for _, fv := range sh.Features {
		if fv.FeatureID == sourceID {
			return true
		}
	}
	_, ok := sourceCasting(sh, sourceID)
	return ok
}

// sourceCasting is the first spellcasting source that knows or has prepared this spell.
func sourceCasting(sh Sheet, spellID string) (SpellSourceView, bool) {
	for _, s := range sh.Spellcasting {
		for _, sp := range append(append([]SpellView{}, s.Spells...), s.Cantrips...) {
			if sp.ID == spellID {
				return s, true
			}
		}
	}
	return SpellSourceView{}, false
}

// petGrantOf is the pets grant of the feature or spell with this id, or nil.
func petGrantOf(lib Library, sourceID string) *PetGrant {
	if f, err := lib.Feature(sourceID); err == nil && f.Pets != nil {
		return f.Pets
	}
	if s, err := lib.Spell(sourceID); err == nil {
		return s.Pets
	}
	return nil
}

// petAllowed reports whether a grant lets the character keep this creature: any shared tag, or listed by id.
func petAllowed(g *PetGrant, cr Creature) bool {
	if g == nil {
		return false
	}
	if slices.Contains(g.CreatureIDs, cr.ID) {
		return true
	}
	return slices.ContainsFunc(g.Tags, func(t string) bool { return slices.Contains(cr.Tags, t) })
}

// ownerContext is what a pet's owner.* terms read. The spell terms use the spellcasting of the
// feature or spell that grants the pet.
func ownerContext(c Character, sh Sheet, lib Library, sourceID string) ownerCtx {
	o := ownerCtx{modOf: map[Ability]int{}, pb: sh.ProficiencyBonus, level: sh.Level, classLevels: map[string]int{}}
	for a, av := range sh.Abilities {
		o.modOf[a] = av.Mod
	}
	for _, cl := range c.Classes {
		o.classLevels[cl.ClassID] += cl.Level
	}
	if src, ok := ownerSpellSource(sh, lib, sourceID); ok {
		o.spellAttack, o.spellSaveDC, o.hasSpellcasting = src.AttackBonus, src.SaveDC, true
	}
	return o
}

// ownerSpellSource finds the spellcasting a pet's source uses: a feature's own spell grant, its
// class's spellcasting, or whichever source can cast the granting spell.
func ownerSpellSource(sh Sheet, lib Library, sourceID string) (SpellSourceView, bool) {
	find := func(id string) (SpellSourceView, bool) {
		for _, s := range sh.Spellcasting {
			if s.ID == id {
				return s, true
			}
		}
		return SpellSourceView{}, false
	}
	for _, fv := range sh.Features {
		if fv.FeatureID != sourceID {
			continue
		}
		if f, err := lib.Feature(sourceID); err == nil && f.Spells != nil {
			return find("feature:" + sourceID)
		}
		if fv.Source == OriginClass {
			return find("class:" + fv.SourceID)
		}
		return SpellSourceView{}, false
	}
	return sourceCasting(sh, sourceID)
}

func fillOwnerPlaceholders(note string, o ownerCtx) string {
	if !strings.Contains(note, "{owner.") {
		return note
	}
	return strings.NewReplacer(
		"{owner.spell_save_dc}", fmt.Sprint(o.spellSaveDC),
		"{owner.spell_attack}", fmt.Sprintf("%+d", o.spellAttack),
	).Replace(note)
}

func attackRange(a CreatureAttack) string {
	switch {
	case a.Ranged && a.RangeLong > 0:
		return fmt.Sprintf("%d/%d ft.", a.RangeNormal, a.RangeLong)
	case a.Ranged:
		return fmt.Sprintf("%d ft.", a.RangeNormal)
	}
	reach := a.Reach
	if reach == 0 {
		reach = 5
	}
	return fmt.Sprintf("reach %d ft.", reach)
}

// damageText is "1d8+5 piercing", "1d4-1 piercing" or, with no dice, "1 slashing".
func damageText(dice string, flat int, typ DamageType) string {
	s := fmt.Sprint(flat)
	if dice != "" {
		s = dice
		if flat != 0 {
			s += fmt.Sprintf("%+d", flat)
		}
	}
	if typ != "" {
		s += " " + string(typ)
	}
	return s
}

func mergeDamageTypes(lists ...[]DamageType) []DamageType {
	out := []DamageType{}
	for _, l := range lists {
		for _, d := range l {
			if !slices.Contains(out, d) {
				out = append(out, d)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func uniqueStrings(in []string) []string {
	var out []string
	for _, s := range in {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func petLabel(p Pet) string {
	if p.Name != "" {
		return p.Name
	}
	return p.CreatureID
}
