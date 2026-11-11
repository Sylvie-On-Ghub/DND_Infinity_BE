package character

// shape.go - shape forms (Wild Shape): the Known Forms choice, the limits a class level sets, which known
// forms the character can take now, and the creature an active shape substitutes into the sheet.

import (
	"fmt"
	"slices"
	"strings"
)

// shapeLimit is the highest limit at or below the class level.
func shapeLimit(def ShapeDef, classLevel int) (ShapeLimit, bool) {
	var best ShapeLimit
	found := false
	for _, l := range def.Limits {
		if l.MinLevel <= classLevel && (!found || l.MinLevel > best.MinLevel) {
			best, found = l, true
		}
	}
	return best, found
}

// knownFormsChoice is the pick of creatures a shape form can become, sized by the class level. It is false
// below the first limit's level.
func knownFormsChoice(def ShapeDef, classLevel int) (Choice, bool) {
	limit, ok := shapeLimit(def, classLevel)
	if !ok {
		return Choice{}, false
	}
	return Choice{ID: KnownFormsChoice, Prompt: "Choose your Known Forms", Kind: ChoicePick, Count: limit.Known,
		Filter: &PoolFilter{Type: "creature", Tag: def.CreatureTag}}, true
}

// shapeFlyFrom is the lowest class level at which a shape allows forms with a fly speed, or 0 for never.
func shapeFlyFrom(def ShapeDef) int {
	from := 0
	for _, l := range def.Limits {
		if l.Fly && (from == 0 || l.MinLevel < from) {
			from = l.MinLevel
		}
	}
	return from
}

// shapeInfo is what a shape form allows the character right now.
type shapeInfo struct {
	entry   formEntry
	limit   ShapeLimit
	maxCR   float64
	flyFrom int // class level from which fly speeds are allowed; 0 for never
	tempHP  int
	known   []string // picked Known Forms, in pick order
}

// shapeInfo reads a shape form's limits at the character's class level. Modifiers on shape.max_cr and
// shape.temp_hp raise them: those tied to this form (or none) and with no condition count.
func (g *gathered) shapeInfo(c Character, e formEntry) (shapeInfo, bool) {
	def := e.def.Shape
	if def == nil {
		return shapeInfo{}, false
	}
	limit, ok := shapeLimit(*def, g.classLevels[def.ClassID])
	if !ok {
		return shapeInfo{}, false
	}
	// The temp HP formula rides through the same resolution as modifier values.
	mods := []Modifier{{Target: TargetShapeTempHP, Kind: ModSet, ValueFormula: &def.TempHP}}
	for _, m := range g.mods {
		if (m.Form == "" || m.Form == e.def.ID) && m.Condition == "" &&
			(m.Target == TargetShapeMaxCR || m.Target == TargetShapeTempHP) {
			mods = append(mods, m)
		}
	}
	mods = resolveFormulaValues(c, g, resolveAbilityValues(c, mods))

	info := shapeInfo{entry: e, limit: limit, flyFrom: shapeFlyFrom(*def), tempHP: mods[0].Value}
	info.maxCR, _ = parseCR(limit.MaxCR)
	for _, m := range mods[1:] {
		switch {
		case m.Target == TargetShapeMaxCR && m.Kind == ModMin:
			info.maxCR = max(info.maxCR, float64(m.Value))
		case m.Target == TargetShapeTempHP && m.Kind == ModSet:
			info.tempHP = max(info.tempHP, m.Value)
		}
	}
	for _, s := range c.Selections {
		if s.FeatureID == e.featureID && s.ChoiceID == KnownFormsChoice {
			info.known = s.Picked
			break
		}
	}
	return info, true
}

// shapeEligible says whether the character can take a creature as this shape now, and why not.
func shapeEligible(info shapeInfo, cr Creature, formName string) (bool, string) {
	if v, _ := parseCR(cr.CR); v > info.maxCR {
		return false, fmt.Sprintf("CR %s is above your max of %s", cr.CR, formatCR(info.maxCR))
	}
	if cr.Speeds[SpeedFly] > 0 && !info.limit.Fly {
		return false, fmt.Sprintf("has a fly speed, which %s allows from level %d", formName, info.flyFrom)
	}
	return true, ""
}

// shapeFormView fills a shape form's limits and known forms. Known ids missing from the library, or
// without the shape's tag, are left out.
func (g *gathered) shapeFormView(c Character, v *FormView, e formEntry) {
	info, ok := g.shapeInfo(c, e)
	if !ok {
		return
	}
	v.MaxCR, v.FlyAllowed, v.TempHP = formatCR(info.maxCR), info.limit.Fly, info.tempHP
	for _, id := range info.known {
		cr, err := g.lib.Creature(id)
		if err != nil || !slices.Contains(cr.Tags, e.def.Shape.CreatureTag) {
			continue
		}
		eligible, why := shapeEligible(info, cr, e.def.Name)
		v.KnownForms = append(v.KnownForms, ShapeOption{ID: cr.ID, Name: cr.Name, CR: cr.CR, Eligible: eligible, Reason: why})
	}
}

// shapeLimitsFor rebuilds a feature's shape info from the sheet's form view, for judging a pick before
// it is made. The form's def is returned too, for its name.
func shapeLimitsFor(sh Sheet, lib Library, featureID string) (shapeInfo, FormDef) {
	f, err := lib.Feature(baseFeatureID(featureID))
	if err != nil || f.Form == nil || f.Form.Shape == nil {
		return shapeInfo{}, FormDef{}
	}
	info := shapeInfo{flyFrom: shapeFlyFrom(*f.Form.Shape)}
	for _, fv := range sh.Forms {
		if fv.ID == f.Form.ID {
			info.maxCR, _ = parseCR(fv.MaxCR)
			info.limit.Fly = fv.FlyAllowed
		}
	}
	return info, *f.Form
}

// activeShape is the creature an active shape form has turned the character into, with the stat block's
// own ability modifiers and proficiency bonus.
type activeShape struct {
	info     shapeInfo
	creature Creature
	modOf    map[Ability]int
	pb       int
}

// resolveActiveShape finds the active shape form's creature. It is nil when no shape form is active or
// its creature isn't in the library; a creature that isn't known or isn't allowed now only warns.
func (g *gathered) resolveActiveShape(c Character, lib Library) (*activeShape, []Warning) {
	for _, e := range g.forms {
		if e.def.Shape == nil || !g.activeForms[e.def.ID] {
			continue
		}
		var af ActiveForm
		for _, a := range c.ActiveForms {
			if a.ID == e.def.ID {
				af = a
				break // first entry wins, as in resolveForms
			}
		}
		cr, err := lib.Creature(af.CreatureID)
		if af.CreatureID == "" || err != nil {
			return nil, []Warning{mkWarning(WarnShapeUnknownCreature, e.featureID, "", "", af.CreatureID,
				fmt.Sprintf("%s is active as %q, which isn't in the library, so the sheet isn't changed", e.def.Name, af.CreatureID))}
		}
		var warns []Warning
		info, ok := g.shapeInfo(c, e)
		if !ok {
			info.entry = e // below the first limit's level: no known forms and a max CR of 0
		}
		if !slices.Contains(info.known, cr.ID) {
			warns = append(warns, mkWarning(WarnShapeNotKnown, e.featureID, KnownFormsChoice, "", cr.ID,
				fmt.Sprintf("%s is active as %s, which isn't one of your Known Forms", e.def.Name, cr.Name)))
		}
		if ok, why := shapeEligible(info, cr, e.def.Name); !ok {
			warns = append(warns, mkWarning(WarnShapeIneligible, e.featureID, "", "", cr.ID,
				fmt.Sprintf("%s is active as %s, which isn't allowed now: %s", e.def.Name, cr.Name, why)))
		}

		// As for pets: the stat block's scores and proficiency bonus come first, its formulas read them.
		s := &activeShape{info: info, creature: cr, modOf: map[Ability]int{}, pb: 2}
		for _, a := range allAbilities {
			s.modOf[a] = floorMod(s.score(a))
		}
		if cr.PB != nil {
			s.pb, _ = evalFormula(*cr.PB, formulaCtx{modOf: s.modOf})
		}
		return s, warns
	}
	return nil, nil
}

// score is the stat block's score for an ability; a missing one is 10.
func (s *activeShape) score(a Ability) int {
	if v, ok := s.creature.Abilities[a]; ok {
		return v
	}
	return 10
}

// eval evaluates a stat block formula with the creature's own modifiers and proficiency bonus.
func (s *activeShape) eval(f Formula) int {
	v, _ := evalFormula(f, formulaCtx{modOf: s.modOf, pb: s.pb})
	return v
}

// replaces reports whether the beast's score replaces the character's: Str, Dex and Con do, the rest stay.
func (s *activeShape) replaces(a Ability) bool {
	return s != nil && (a == Str || a == Dex || a == Con)
}

// saveBase is the higher of the character's own save bonus and the stat block's, before save modifiers.
// The stat block only counts for saves it lists.
func (s *activeShape) saveBase(a Ability, own int) int {
	if s == nil || !slices.Contains(s.creature.Saves, a) {
		return own
	}
	return max(own, s.modOf[a]+s.pb)
}

// skillBase is the higher of the character's own skill bonus and the stat block's, before modifiers, with
// the better proficiency level. The stat block only counts for skills it lists.
func (s *activeShape) skillBase(sd SkillDef, own int, lvl ProficiencyLevel) (int, ProficiencyLevel) {
	if s == nil {
		return own, lvl
	}
	blockLvl, ok := s.creature.Skills[sd.ID]
	if !ok {
		return own, lvl
	}
	return max(own, s.modOf[sd.DefaultAbility]+profPart(blockLvl, s.pb)), maxLevel(lvl, blockLvl)
}

// acCandidates are the AC formulas open while shaped: the stat block's AC and formulas tied to an active
// form. Armour, shields, unarmoured and spell formulas don't count.
func (s *activeShape) acCandidates(g *gathered) []ACFormula {
	out := []ACFormula{{ID: "shape", Name: s.creature.Name + " natural armour", Formula: Formula{Base: s.eval(s.creature.AC)}}}
	seen := map[string]bool{"shape": true}
	for _, f := range g.acFormulas {
		if f.Form != "" && g.activeForms[f.Form] && !seen[f.ID] {
			seen[f.ID] = true
			out = append(out, f)
		}
	}
	return out
}

// acCalc is k's ac modifiers without those of worn armour and shields, which don't count while shaped.
func (s *activeShape) acCalc(g *gathered, k *calc) *calc {
	return newCalc(slices.DeleteFunc(slices.Clone(k.byTarget[TargetAC]), func(m Modifier) bool { return g.wornSources[m.SourceID] }))
}

// senses merges the beast's senses into the character's, keeping the longer range of each, sorted by name.
func (s *activeShape) senses(own []Sense) []Sense {
	best := map[string]int{}
	for _, sn := range append(slices.Clone(own), s.creature.Senses...) {
		best[sn.Name] = max(best[sn.Name], sn.Range)
	}
	out := []Sense{}
	for _, name := range sortedKeys(best) {
		out = append(out, Sense{Name: name, Range: best[name]})
	}
	return out
}

// beastAttack is one stat block attack. To-hit and the flat damage come from the stat block, as for pets;
// the character's attack and damage modifiers and riders apply on top. The attack's ability is the first
// mod.* term of its to-hit (Str if none), and a Str attack meets strength_attack.
func beastAttack(a CreatureAttack, s *activeShape, k *calc) AttackView {
	atkTarget, dmgTarget := TargetAttackMelee, TargetDamageMelee
	rangeNormal := a.Reach
	if a.Ranged {
		atkTarget, dmgTarget = TargetAttackRanged, TargetDamageRanged
		rangeNormal = a.RangeNormal
	}
	atkTargets := []string{TargetAttackAll, atkTarget}
	dmgTargets := []string{TargetDamageAll, dmgTarget}

	ab := Str
	for _, t := range a.ToHit.Terms {
		if m, ok := strings.CutPrefix(t.Source, "mod."); ok && isAbility(m) {
			ab = Ability(m)
			break
		}
	}
	var qualifies []string
	if ab == Str {
		qualifies = []string{RequiresStrengthAttack}
	}

	adv := k.flag(ModAdvantage, atkTargets...)
	dis := k.flag(ModDisadvantage, atkTargets...)
	return AttackView{
		Name:         a.Name,
		Ability:      ab,
		Proficient:   true, // the stat block's to-hit carries its own proficiency
		ToHit:        s.eval(a.ToHit) + k.sum(atkTargets...) + k.weaponSum(qualifies, atkTargets...),
		Advantage:    adv && !dis,
		Disadvantage: dis && !adv,
		DamageDice:   a.DamageDice,
		DamageBonus:  s.eval(a.Damage) + k.sum(dmgTargets...) + k.weaponSum(qualifies, dmgTargets...),
		DamageType:   a.DamageType,
		RangeNormal:  rangeNormal,
		RangeLong:    a.RangeLong,
		AttackRiders: append(k.riders(atkTargets...), k.weaponRiders(qualifies, atkTargets...)...),
		DamageRiders: append(k.riders(dmgTargets...), k.weaponRiders(qualifies, dmgTargets...)...),
	}
}

// view is the shape for Sheet.Shape; nil when not shaped.
func (s *activeShape) view(k *calc) *ShapeView {
	if s == nil {
		return nil
	}
	cr := s.creature
	return &ShapeView{FormID: s.info.entry.def.ID, CreatureID: cr.ID, Name: cr.Name, Size: cr.Size, CR: cr.CR,
		Traits: append([]Trait{}, cr.Traits...), CanCast: k.apply(TargetShapeCastSpells, 0) > 0}
}
