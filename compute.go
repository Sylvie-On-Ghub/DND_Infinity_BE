package character

// compute.go - first pass at the internals of Compute. NOT yet compiled or run.
//
// Shape: three phases, then a set of derived values that call each other.
//
//   1. gather   Walk the character's features (and nested ones), resolve their choices,
//               and flatten everything into one list of modifiers + merged grants.
//               Unknown ids, bad picks and pending choices become warnings/pending items.
//   2. dedupe   Apply the "same named source doesn't stack" rule once, up front.
//   3. derive   Ability scores -> mods -> proficiency bonus -> saves, skills, HP, etc.
//               Each value is a function that calls the ones it depends on, so evaluation
//               order falls out of the code (skill -> ability mod -> ability score) rather
//               than needing a hand-maintained pipeline order.
//
// Order of operations for any single target (see calc.apply):
//   Set (best wins)  ->  sum of Add  ->  Multiply  ->  Min as a floor.
// Conditional modifiers (Condition != "") and Notes never change numbers; they are surfaced
// in Sheet.Notes so nothing is silently dropped.

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Extra target and warning code introduced here; add to the valid-target list in character.go
// if you want them next to the others.
const (
	// A modifier with this target and Kind ModSet means "Jack of All Trades": untrained skills
	// (and initiative) get half proficiency. Data-driven, so any feature can grant it.
	TargetJackOfAllTrades = "jack_of_all_trades"

	WarnRolledHPMismatch WarningCode = "rolled_hp_mismatch" // RolledHP entries != class level
	WarnWrongKind        WarningCode = "wrong_kind"         // background_id / race_id names a feature that isn't tagged as one
)

var allAbilities = []Ability{Str, Dex, Con, Int, Wis, Cha}
var allSpeeds = []SpeedType{SpeedWalk, SpeedFly, SpeedSwim, SpeedClimb, SpeedBurrow}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

func floorMod(score int) int { return (score - 10) >> 1 } // arithmetic shift floors, so 9 -> -1

func rank(l ProficiencyLevel) int {
	switch l {
	case ProfHalf:
		return 1
	case ProfTrained:
		return 2
	case ProfExpert:
		return 3
	}
	return 0
}

func maxLevel(a, b ProficiencyLevel) ProficiencyLevel {
	if rank(b) > rank(a) {
		return b
	}
	return a
}

// profPart is the proficiency portion of a bonus at a given level.
func profPart(l ProficiencyLevel, pb int) int {
	switch l {
	case ProfHalf:
		return pb / 2
	case ProfTrained:
		return pb
	case ProfExpert:
		return pb * 2
	}
	return 0
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func addUnique(dst []string, src ...string) []string {
	for _, s := range src {
		if !contains(dst, s) {
			dst = append(dst, s)
		}
	}
	return dst
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func mkWarning(code WarningCode, featureID, choiceID, target, value, msg string) Warning {
	return Warning{
		Key:       WarningKey(code, featureID, choiceID, value),
		Code:      code,
		Message:   msg,
		FeatureID: featureID,
		ChoiceID:  choiceID,
		Target:    target,
		Value:     value,
	}
}

func withSource(m Modifier, defaultSource string) Modifier {
	if m.SourceID == "" {
		m.SourceID = defaultSource
	}
	return m
}

// ---------------------------------------------------------------------------
// Phase 1: gather
// ---------------------------------------------------------------------------

type gathered struct {
	mods      []Modifier
	features  []Feature // every feature that resolved, for prerequisite checks
	skills    map[string]ProficiencyLevel
	tools     map[string]ProficiencyLevel
	languages []string
	armour    []string
	weapons   []string
	actions   []Action
	views     []FeatureView     // every feature that resolved, for Sheet.Features
	resOf     map[string]string // feature id -> its resource's id in Sheet.Resources
	pending   []PendingChoice
	warnings  []Warning

	acFormulas []ACFormula // from features and worn armour
	armourWorn bool
	shieldWorn bool

	weaponItems []weaponEntry

	saves       map[Ability]bool // save proficiencies granted by class or features
	classLevels map[string]int   // class id -> level, for class_level.<id> formula terms
	casters     []casterEntry
	resources   []resourceEntry
	spellGrants []spellGrantEntry
}

func (g *gathered) addGrants(gr Grants) {
	for id, l := range gr.Skills {
		g.skills[id] = maxLevel(g.skills[id], l)
	}
	for id, l := range gr.Tools {
		g.tools[id] = maxLevel(g.tools[id], l)
	}
	g.languages = addUnique(g.languages, gr.Languages...)
	g.armour = addUnique(g.armour, gr.ArmourProfs...)
	g.weapons = addUnique(g.weapons, gr.WeaponProfs...)
	for _, a := range gr.Saves {
		g.saves[a] = true
	}
	// gr.FeatureIDs are queued by the caller.
}

// conditionModifiers is a starter table. Longer term, conditions are better as library data.
// Exhaustion is left out because its effects differ between rules editions.
func conditionModifiers(cond Condition) []Modifier {
	src := "condition:" + string(cond)
	switch cond {
	case "poisoned":
		return []Modifier{
			{Target: TargetAttackAll, Kind: ModDisadvantage, SourceID: src},
			{Target: TargetCheckAll, Kind: ModDisadvantage, SourceID: src},
		}
	case "frightened":
		when := "while the source of fear is in sight"
		return []Modifier{
			{Target: TargetAttackAll, Kind: ModDisadvantage, Condition: when, SourceID: src},
			{Target: TargetCheckAll, Kind: ModDisadvantage, Condition: when, SourceID: src},
		}
	case "blinded":
		return []Modifier{{Target: TargetAttackAll, Kind: ModDisadvantage, SourceID: src}}
	}
	return nil
}

// queued is a feature waiting to be visited. classLevel is the level in the class that granted it
// (0 when it didn't come from a class); it selects the scaling tier. The rest describe where it came
// from for Sheet.Features: gained is the class level it arrived at, origin and sourceID the class, race
// or background it was reached through, grantedBy the feature whose grant or choice queued it.
type queued struct {
	id         string
	classLevel int
	gained     int
	origin     Origin
	sourceID   string
	grantedBy  string
}

// child is a feature queued by q's feature: same class context, granted by that feature.
func (q queued) child(id, grantedBy string) queued {
	q.id, q.grantedBy = id, grantedBy
	return q
}

// featureView describes a resolved feature. The feature's own origin and source win; otherwise they
// come from how it was reached. A feat with no source of its own is its own source.
func featureView(f Feature, q queued) FeatureView {
	v := FeatureView{
		FeatureID: f.ID, Name: f.Name, Description: f.Description,
		Source: f.Origin, SourceID: f.SourceID, GrantedBy: q.grantedBy, Level: q.gained,
	}
	if v.Source == "" {
		v.Source = q.origin
		if v.Source == "" {
			v.Source = OriginOther
		}
	}
	if v.SourceID == "" {
		switch {
		case v.Source == q.origin:
			v.SourceID = q.sourceID
		case v.Source == OriginFeat:
			v.SourceID = baseFeatureID(f.ID)
		}
	}
	if v.Level == 0 {
		v.Level = f.LevelGained
	}
	return v
}

// scaleLevel picks the level used for tiers: the granting class's level by default,
// the character level when the feature says so or didn't come from a class.
func scaleLevel(f Feature, classLevel, total int) int {
	if f.ScaleBy == "character" || classLevel <= 0 {
		return total
	}
	return classLevel
}

// bestTier returns the highest tier at or below level, or nil.
func bestTier(tiers []Tier, level int) *Tier {
	var best *Tier
	for i := range tiers {
		t := &tiers[i]
		if t.MinLevel <= level && (best == nil || t.MinLevel > best.MinLevel) {
			best = t
		}
	}
	return best
}

func gather(c Character, lib Library, totalLevel int) *gathered {
	g := &gathered{
		skills:      map[string]ProficiencyLevel{},
		tools:       map[string]ProficiencyLevel{},
		saves:       map[Ability]bool{},
		classLevels: map[string]int{},
		resOf:       map[string]string{},
	}

	g.languages = addUnique(g.languages, c.Languages...)
	g.armour = addUnique(g.armour, c.ArmourProfs...)
	g.weapons = addUnique(g.weapons, c.WeaponProfs...)

	g.mods = append(g.mods, c.Modifiers...)
	for _, cond := range c.Conditions {
		g.mods = append(g.mods, conditionModifiers(cond)...)
	}

	// Race and background are treated as features that grant nested features for now.
	var queue []queued
	for _, id := range c.FeatureIDs {
		queue = append(queue, queued{id: id})
	}
	if c.RaceID != "" {
		queue = append(queue, queued{id: c.RaceID, origin: OriginRace, sourceID: c.RaceID})
	}
	if c.BackgroundID != "" {
		queue = append(queue, queued{id: c.BackgroundID, origin: OriginBackground, sourceID: c.BackgroundID})
	}

	// Backgrounds and races are features tagged "background" / "race". The ids are checked, and when the
	// loaded content offers any of either, a character with none gets a pending item to choose one.
	g.checkSelectable(lib, "race_id", c.RaceID, "race")
	g.checkSelectable(lib, "background_id", c.BackgroundID, "background")
	if c.RaceID == "" && libraryHasTagged(lib, "race") {
		g.pending = append(g.pending, PendingChoice{
			FeatureID: "character", ChoiceID: "race", Prompt: "Choose a race", Remaining: 1,
		})
	}
	if c.BackgroundID == "" && libraryHasTagged(lib, "background") {
		g.pending = append(g.pending, PendingChoice{
			FeatureID: "character", ChoiceID: "background", Prompt: "Choose a background", Remaining: 1,
		})
	}

	seen := map[string]bool{} // a feature reached twice counts once (also guards cycles)

	queue = g.expandClasses(c, lib, queue, seen)

	for len(queue) > 0 {
		q := queue[0]
		queue = queue[1:]

		// A feature picked from a choice arrives as "<id>@<granting feature>:<choice>". Ordinary
		// features count once however they are reached. A repeatable feature (Ability Score
		// Improvement as a feat) keeps the whole string as its instance id, so each pick is separate:
		// its own selections, its own pending choices, its own modifier sources.
		baseID := baseFeatureID(q.id)
		f, err := lib.Feature(baseID)
		if err != nil {
			if !seen["missing:"+baseID] {
				seen["missing:"+baseID] = true
				g.warnings = append(g.warnings, mkWarning(WarnUnknownID, baseID, "", "", baseID,
					fmt.Sprintf("feature %q was not found in the library", baseID)))
			}
			continue
		}
		key := baseID
		if f.Repeatable {
			key = q.id
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		f.ID = key
		g.views = append(g.views, featureView(f, q))

		for _, id := range g.applyFeature(c, f, scaleLevel(f, q.classLevel, totalLevel)) {
			queue = append(queue, q.child(id, key)) // nested features inherit the class context
		}
	}

	g.applyInventory(c, lib)
	for i := range g.views {
		g.views[i].ResourceID = g.resOf[g.views[i].FeatureID]
	}
	return g
}

func findSelection(sels []Selection, featureID, choiceID string) (Selection, bool) {
	for _, s := range sels {
		if s.FeatureID == featureID && s.ChoiceID == choiceID {
			return s, true
		}
	}
	return Selection{}, false
}

func findOption(opts []Option, id string) (Option, bool) {
	for _, o := range opts {
		if o.ID == id {
			return o, true
		}
	}
	return Option{}, false
}

func choiceSize(ch Choice) int {
	if ch.Kind == ChoiceAllocate {
		return ch.Points
	}
	return ch.Count
}

// applyFeature records what a feature gives directly (including its active tier), resolves its
// choices, and returns further feature ids to visit. level is the already-resolved tier level.
func (g *gathered) applyFeature(c Character, f Feature, level int) []string {
	g.features = append(g.features, f)

	res := f.Resource
	for _, m := range f.Modifiers {
		g.mods = append(g.mods, withSource(m, f.ID))
	}
	acts := append([]Action{}, f.Actions...)
	g.acFormulas = append(g.acFormulas, f.ACFormulas...)
	spells := f.Spells // the active tier may add to this
	g.addGrants(f.Grants)
	next := append([]string{}, f.Grants.FeatureIDs...)

	// Scaling: exactly one tier applies (the highest at or below level), on top of the base.
	if t := bestTier(f.Tiers, level); t != nil {
		for _, m := range t.Modifiers {
			g.mods = append(g.mods, withSource(m, f.ID))
		}
		acts = append(acts, t.Actions...)
		g.addGrants(t.Grants)
		next = append(next, t.Grants.FeatureIDs...)
		if t.Resource != nil {
			res = t.Resource
		}
		if t.Spells != nil {
			spells = mergeSpellGrants(spells, t.Spells)
		}
	}
	if res != nil {
		g.resources = append(g.resources, resourceEntry{featureID: f.ID, res: *res})
		g.resOf[f.ID] = f.ID
	}
	if spells != nil {
		before := len(g.resources)
		g.addSpellGrant("feature:"+f.ID, f.Name, *spells)
		if len(g.resources) > before && g.resOf[f.ID] == "" {
			g.resOf[f.ID] = g.resources[before].featureID // uses live on the spell grant ("feature:<id>")
		}
	}
	g.addActions(f.ID, g.resOf[f.ID], acts)

	// Choices. Choices sharing a Group are alternatives: if any one is complete, the unfinished
	// others stop showing as pending; filling in more than one warns.
	start := len(g.pending)
	groupOf := map[string]string{}
	satisfied := map[string]int{}
	for _, ch := range f.Choices {
		before := len(g.pending)
		sel, ok := findSelection(c.Selections, f.ID, ch.ID)
		if !ok {
			if n := choiceSize(ch); n > 0 {
				g.pending = append(g.pending, PendingChoice{
					FeatureID: f.ID, ChoiceID: ch.ID, Prompt: ch.Prompt, Remaining: n,
				})
			}
		} else {
			next = append(next, g.applyChoice(f, ch, sel)...)
		}
		if ch.Group != "" {
			groupOf[ch.ID] = ch.Group
			if ok && len(g.pending) == before {
				satisfied[ch.Group]++
			}
		}
	}
	if len(satisfied) > 0 {
		kept := g.pending[:start]
		for _, p := range g.pending[start:] {
			if grp, isGrouped := groupOf[p.ChoiceID]; isGrouped && p.FeatureID == f.ID && satisfied[grp] > 0 {
				continue
			}
			kept = append(kept, p)
		}
		g.pending = kept
		for _, grp := range sortedKeys(satisfied) {
			if satisfied[grp] > 1 {
				g.warnings = append(g.warnings, mkWarning(WarnOverCount, f.ID, "", "", "group:"+grp,
					fmt.Sprintf("more than one choice in group %q is filled in on %s", grp, f.Name)))
			}
		}
	}
	return next
}

// addActions records a feature's actions, tagged with the feature's id and its resource's id.
// acts holds copies, so tagging them leaves the library untouched.
func (g *gathered) addActions(featureID, resourceID string, acts []Action) {
	for _, a := range acts {
		a.FeatureID, a.ResourceID = featureID, resourceID
		g.actions = append(g.actions, a)
	}
}

func (g *gathered) applyOption(f Feature, opt Option, scale int, scope string) []string {
	g.addGrants(opt.Grants)
	for _, m := range opt.Modifiers {
		if scale != 1 {
			m.Value *= scale // allocate: N points => N times the modifier
		}
		// Per-option default source, so two different options from one feature don't dedupe each other.
		g.mods = append(g.mods, withSource(m, f.ID+"/"+opt.ID))
	}
	return scopeIDs(opt.Grants.FeatureIDs, scope)
}

func (g *gathered) applyChoice(f Feature, ch Choice, sel Selection) []string {
	var next []string

	switch ch.Kind {
	case ChoiceAllocate:
		total := 0
		for _, optID := range sortedKeys(sel.Allocated) {
			pts := sel.Allocated[optID]
			total += pts
			opt, ok := findOption(ch.Options, optID)
			if !ok {
				g.warnings = append(g.warnings, mkWarning(WarnNotInPool, f.ID, ch.ID, "", optID,
					fmt.Sprintf("%q is not an option for %q", optID, ch.Prompt)))
				continue
			}
			if ch.MaxPerOption > 0 && pts > ch.MaxPerOption {
				g.warnings = append(g.warnings, mkWarning(WarnOverCount, f.ID, ch.ID, "", optID,
					fmt.Sprintf("%d points in %q exceeds the per-option maximum of %d", pts, optID, ch.MaxPerOption)))
			}
			next = append(next, g.applyOption(f, opt, pts, f.ID+":"+ch.ID)...)
		}
		if total > ch.Points {
			g.warnings = append(g.warnings, mkWarning(WarnOverCount, f.ID, ch.ID, "", "points",
				fmt.Sprintf("%d points allocated but only %d available", total, ch.Points)))
		} else if total < ch.Points {
			g.pending = append(g.pending, PendingChoice{
				FeatureID: f.ID, ChoiceID: ch.ID, Prompt: ch.Prompt, Remaining: ch.Points - total,
			})
		}

	default: // ChoicePick
		for _, id := range sel.Picked {
			switch {
			case len(ch.Options) > 0:
				opt, ok := findOption(ch.Options, id)
				if !ok {
					g.warnings = append(g.warnings, mkWarning(WarnNotInPool, f.ID, ch.ID, "", id,
						fmt.Sprintf("%q is not an option for %q", id, ch.Prompt)))
					continue // nothing to apply: the option's data doesn't exist
				}
				next = append(next, g.applyOption(f, opt, 1, f.ID+":"+ch.ID)...)

			case ch.Filter != nil:
				// Permissive: out-of-pool picks are warned about but still applied.
				if len(ch.Filter.Allowed) > 0 && !contains(ch.Filter.Allowed, id) {
					g.warnings = append(g.warnings, mkWarning(WarnNotInPool, f.ID, ch.ID, "", id,
						fmt.Sprintf("%q is outside the allowed list for %q", id, ch.Prompt)))
				}
				next = append(next, g.applyFilterPick(ch, id, f.ID+":"+ch.ID)...)
			}
		}
		n := len(sel.Picked)
		if n > ch.Count {
			g.warnings = append(g.warnings, mkWarning(WarnOverCount, f.ID, ch.ID, "",
				fmt.Sprintf("%d/%d", n, ch.Count),
				fmt.Sprintf("%d picks made but %q allows %d", n, ch.Prompt, ch.Count)))
		} else if n < ch.Count {
			g.pending = append(g.pending, PendingChoice{
				FeatureID: f.ID, ChoiceID: ch.ID, Prompt: ch.Prompt, Remaining: ch.Count - n,
			})
		}
	}
	return next
}

func (g *gathered) applyFilterPick(ch Choice, id, scope string) []string {
	lvl := ch.GrantLevel
	if rank(lvl) == 0 {
		lvl = ProfTrained // default for "pick a skill/tool"
	}
	switch ch.Filter.Type {
	case "skill":
		g.addGrants(Grants{Skills: map[string]ProficiencyLevel{id: lvl}})
	case "tool":
		g.addGrants(Grants{Tools: map[string]ProficiencyLevel{id: lvl}})
	case "language":
		g.addGrants(Grants{Languages: []string{id}})
	case "feat", "feature_tag":
		return scopeIDs([]string{id}, scope) // the pick is itself a feature; the main loop resolves it (and its own choices)
	}
	return nil
}

// libraryHasTagged reports whether the library can list its features and any carries the tag.
func libraryHasTagged(lib Library, tag string) bool {
	lister, ok := lib.(interface{ AllFeatures() []Feature })
	if !ok {
		return false
	}
	for _, f := range lister.AllFeatures() {
		if contains(f.Tags, tag) {
			return true
		}
	}
	return false
}

// checkSelectable warns when a character's race_id or background_id names a feature that isn't tagged
// as one (a racial trait, a feat, a class feature). Unknown ids are reported by the main gather loop.
func (g *gathered) checkSelectable(lib Library, field, id, tag string) {
	if id == "" {
		return
	}
	f, err := lib.Feature(id)
	if err != nil {
		return
	}
	if !contains(f.Tags, tag) {
		g.warnings = append(g.warnings, mkWarning(WarnWrongKind, id, "", field, id,
			fmt.Sprintf("%s %q is %q, which isn't tagged %q, so it isn't a %s", field, id, f.Name, tag, tag)))
	}
}

// mergeSpellGrants combines a feature's own spells with those of its active tier. They are one spell
// source: the spells are joined, the tier's ability and resource win when it sets them, and a tier
// with no base grant simply becomes the grant. Tiers replace each other, so a higher tier lists
// every tier spell that should still apply at that level.
func mergeSpellGrants(base, tier *SpellGrant) *SpellGrant {
	if base == nil {
		cp := *tier
		return &cp
	}
	out := SpellGrant{
		SpellIDs: addUnique(append([]string(nil), base.SpellIDs...), tier.SpellIDs...),
		Ability:  base.Ability,
		Resource: base.Resource,
	}
	if tier.Ability != "" {
		out.Ability = tier.Ability
	}
	if tier.Resource != nil {
		out.Resource = tier.Resource
	}
	return &out
}

// scopeIDs tags features granted by a choice with where they came from ("<id>@<feature>:<choice>").
// Only repeatable features use the tag; for everything else it is stripped again by baseFeatureID.
func scopeIDs(ids []string, scope string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id+"@"+scope)
	}
	return out
}

// baseFeatureID drops the "@..." scope from a feature instance id.
func baseFeatureID(id string) string {
	if i := strings.Index(id, "@"); i >= 0 {
		return id[:i]
	}
	return id
}

// ---------------------------------------------------------------------------
// Phase 2: stacking
// ---------------------------------------------------------------------------

// dedupe implements "same named source doesn't stack". Modifiers sharing
// (Target, Kind, SourceID, Condition, Note) collapse to one, keeping the highest Value.
// Empty SourceID or AllowStack opts out. (Open question: for penalties, "best" arguably means
// the worst; revisit if it matters.)
func dedupe(mods []Modifier) []Modifier {
	type key struct {
		target, source, condition, note string
		kind                            ModKind
	}
	idx := map[key]int{}
	out := make([]Modifier, 0, len(mods))
	for _, m := range mods {
		if m.SourceID == "" || m.AllowStack {
			out = append(out, m)
			continue
		}
		k := key{m.Target, m.SourceID, m.Condition, m.Note, m.Kind}
		if i, ok := idx[k]; ok {
			if m.Value > out[i].Value {
				out[i] = m
			}
			continue
		}
		idx[k] = len(out)
		out = append(out, m)
	}
	return out
}

// ---------------------------------------------------------------------------
// Target validation
// ---------------------------------------------------------------------------

var fixedTargets = map[string]bool{
	TargetAC: true, TargetInitiative: true, TargetMaxHP: true, TargetProficiencyBonus: true,
	TargetSpellSaveDC: true, TargetSpellAttack: true, TargetAttackAll: true, TargetAttackMelee: true,
	TargetAttackRanged: true, TargetCheckAll: true, TargetSaveAll: true, TargetDeathSave: true,
	TargetCarryCapacity: true, TargetJackOfAllTrades: true,
	TargetDamageAll: true, TargetDamageMelee: true, TargetDamageRanged: true, TargetDamageSpell: true,
}

func isAbility(s string) bool {
	for _, a := range allAbilities {
		if string(a) == s {
			return true
		}
	}
	return false
}

func isSpeed(s string) bool {
	for _, t := range allSpeeds {
		if string(t) == s {
			return true
		}
	}
	return false
}

func validTarget(t string, skillIDs map[string]bool) bool {
	if fixedTargets[t] {
		return true
	}
	i := strings.Index(t, ".")
	if i < 0 {
		return false
	}
	prefix, rest := t[:i], t[i+1:]
	switch prefix {
	case "ability":
		return isAbility(rest)
	case "check":
		return rest == "all" || isAbility(rest)
	case "save":
		return rest == "all" || rest == "death" || isAbility(rest)
	case "skill", "passive":
		return skillIDs[rest]
	case "speed":
		return isSpeed(rest)
	case "sense", "resist", "immune", "vuln":
		return rest != ""
	}
	return false
}

// ---------------------------------------------------------------------------
// Phase 3: calculation helpers
// ---------------------------------------------------------------------------

type calc struct {
	byTarget map[string][]Modifier
}

func newCalc(mods []Modifier) *calc {
	k := &calc{byTarget: map[string][]Modifier{}}
	for _, m := range mods {
		k.byTarget[m.Target] = append(k.byTarget[m.Target], m)
	}
	return k
}

// apply resolves one target: Set (highest wins) -> sum of Add -> Min as a floor.
// Only unconditional modifiers affect the number.
func (k *calc) apply(target string, base int) int {
	v := base
	hasSet, setVal := false, 0
	for _, m := range k.byTarget[target] {
		if m.Kind == ModSet && m.Condition == "" && (!hasSet || m.Value > setVal) {
			hasSet, setVal = true, m.Value
		}
	}
	if hasSet {
		v = setVal
	}
	for _, m := range k.byTarget[target] {
		if m.Kind == ModAdd && m.Condition == "" && m.Dice == "" {
			v += m.Value
		}
	}
	for _, m := range k.byTarget[target] {
		if m.Kind == ModMultiply && m.Condition == "" && m.Value > 0 {
			v *= m.Value
		}
	}
	for _, m := range k.byTarget[target] {
		if m.Kind == ModMin && m.Condition == "" && v < m.Value {
			v = m.Value
		}
	}
	return v
}

// sum adds unconditional Add modifiers across several targets (e.g. skill.stealth + check.dex + check.all).
func (k *calc) sum(targets ...string) int {
	total := 0
	for _, t := range targets {
		for _, m := range k.byTarget[t] {
			if m.Kind == ModAdd && m.Condition == "" && m.Dice == "" {
				total += m.Value
			}
		}
	}
	return total
}

// flag reports whether any unconditional modifier of this kind exists on any of the targets.
func (k *calc) flag(kind ModKind, targets ...string) bool {
	for _, t := range targets {
		for _, m := range k.byTarget[t] {
			if m.Kind == kind && m.Condition == "" {
				return true
			}
		}
	}
	return false
}

// damageList returns damage types granted under a target prefix ("resist.", "immune.", "vuln.").
// Any unconditional non-note modifier counts. Conditional ones surface in Sheet.Notes.
// TODO: resistance + vulnerability to the same type cancel; immunity overrides both.
func (k *calc) damageList(prefix string) []DamageType {
	out := []DamageType{}
	for _, t := range sortedKeys(k.byTarget) {
		if !strings.HasPrefix(t, prefix) {
			continue
		}
		for _, m := range k.byTarget[t] {
			if m.Condition == "" && m.Kind != ModNote {
				out = append(out, DamageType(strings.TrimPrefix(t, prefix)))
				break
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Compute
// ---------------------------------------------------------------------------

// Compute turns stored state plus the library into a Sheet. Pure: same inputs, same output.
func Compute(c Character, lib Library, rules Ruleset) (Sheet, error) {
	if lib == nil {
		return Sheet{}, fmt.Errorf("compute: library is required")
	}

	totalLevel := 0
	for _, cl := range c.Classes {
		totalLevel += cl.Level
	}
	g := gather(c, lib, totalLevel)
	mods := dedupe(g.mods)

	// Inventory and carrying are worked out first: variant encumbrance comes back as ordinary modifiers.
	invRes := computeInventory(c, lib, rules, newCalc(mods))
	mods = dedupe(append(mods, invRes.mods...))
	k := newCalc(mods)

	skillDefs := lib.Skills()
	skillIDs := map[string]bool{}
	for _, sd := range skillDefs {
		skillIDs[sd.ID] = true
	}

	warnings := append([]Warning{}, g.warnings...)
	warnings = append(warnings, invRes.warnings...)

	// Unknown modifier targets (typos in homebrew data), once per distinct target.
	seenTarget := map[string]bool{}
	for _, m := range mods {
		if seenTarget[m.Target] || validTarget(m.Target, skillIDs) {
			continue
		}
		seenTarget[m.Target] = true
		warnings = append(warnings, mkWarning(WarnUnknownTarget, m.SourceID, "", m.Target, m.Target,
			fmt.Sprintf("modifier target %q is not recognised, so it has no effect", m.Target)))
	}

	// Dice must look like "1d6" and only make sense as additive riders.
	for _, m := range mods {
		if m.Dice == "" {
			continue
		}
		if m.Kind != ModAdd || !diceRe.MatchString(m.Dice) {
			warnings = append(warnings, mkWarning(WarnBadDice, m.SourceID, "", m.Target, m.Dice,
				fmt.Sprintf("dice %q on %q must be like \"1d6\" and use kind add, so it is ignored", m.Dice, m.Target)))
		}
	}

	// Level, proficiency bonus.
	level, rolled := 0, 0
	for _, cl := range c.Classes {
		level += cl.Level
		for _, hp := range cl.RolledHP {
			rolled += hp
		}
		if len(cl.RolledHP) > cl.Level {
			warnings = append(warnings, mkWarning(WarnRolledHPMismatch, "", "", "", cl.ClassID,
				fmt.Sprintf("%s has %d levels but %d rolled HP entries", cl.ClassID, cl.Level, len(cl.RolledHP))))
		}
	}
	lvlForPB := level
	if lvlForPB < 1 {
		lvlForPB = 1
	}
	pb := k.apply(TargetProficiencyBonus, 2+(lvlForPB-1)/4)

	jack := k.flag(ModSet, TargetJackOfAllTrades)

	// Abilities, saves.
	abilities := map[Ability]AbilityView{}
	modOf := map[Ability]int{}
	scoreOf := map[Ability]int{}
	for _, a := range allAbilities {
		score, saveProf := 10, false // missing ability defaults to 10 rather than 0
		if st, ok := c.Abilities[a]; ok {
			score, saveProf = st.Score, st.SaveProficient
		}
		if g.saves[a] {
			saveProf = true // from class or a feature; the stored flag stays as a manual extra
		}
		score = k.apply(AbilityTarget(a), score)
		mod := floorMod(score)
		save := mod + k.sum(SaveTarget(a), TargetSaveAll)
		if saveProf {
			save += pb
		}
		abilities[a] = AbilityView{Score: score, Mod: mod, SaveBonus: save, SaveProf: saveProf}
		modOf[a], scoreOf[a] = mod, score
	}

	// Skills and passives.
	skills := map[string]SkillView{}
	skillBonus := map[string]int{}
	for _, sd := range skillDefs {
		ab := sd.DefaultAbility
		if o, ok := c.SkillAbility[sd.ID]; ok {
			ab = o
		}
		lvl := maxLevel(c.Skills[sd.ID], g.skills[sd.ID])
		if rank(lvl) == 0 && jack {
			lvl = ProfHalf
		}
		targets := []string{SkillTarget(sd.ID), CheckTarget(ab), TargetCheckAll}
		bonus := modOf[ab] + profPart(lvl, pb) + k.sum(targets...)

		adv := k.flag(ModAdvantage, targets...)
		dis := k.flag(ModDisadvantage, targets...)
		advNet, disNet := adv && !dis, dis && !adv // both present cancel out

		passive := 10 + bonus + k.sum(PassiveTarget(sd.ID))
		if advNet {
			passive += 5
		} else if disNet {
			passive -= 5
		}
		skills[sd.ID] = SkillView{
			Ability: ab, Level: lvl, Bonus: bonus, Passive: passive,
			Advantage: advNet, Disadvantage: disNet,
		}
		skillBonus[sd.ID] = bonus
	}

	// Tools: only trained or better are shown; the view is the proficiency part ("Mod+<n>").
	toolLevels := map[string]ProficiencyLevel{}
	for id, l := range c.Tools {
		toolLevels[id] = maxLevel(toolLevels[id], l)
	}
	for id, l := range g.tools {
		toolLevels[id] = maxLevel(toolLevels[id], l)
	}
	tools := map[string]ToolView{}
	for id, l := range toolLevels {
		if rank(l) > 0 {
			tools[id] = ToolView{Level: l, ProfBonus: profPart(l, pb)}
		}
	}

	// HP, AC, initiative.
	maxHP := k.apply(TargetMaxHP, rolled+level*modOf[Con])
	acDetail, acWarnings := computeAC(c, g, lib, rules, k, modOf, pb, level)
	warnings = append(warnings, acWarnings...)
	ac := acDetail.Total
	init := k.apply(TargetInitiative, modOf[Dex])
	if jack {
		init += pb / 2
	}

	// Speeds: all from modifiers, so the race/feature must grant its base (e.g. speed.walk Add 30).
	speeds := map[SpeedType]int{}
	for _, t := range allSpeeds {
		if v := k.apply(SpeedTarget(t), 0); v > 0 {
			speeds[t] = v
		}
	}

	// Senses: any "sense.<name>" target with a positive resolved range.
	senses := []Sense{}
	for _, t := range sortedKeys(k.byTarget) {
		if !strings.HasPrefix(t, "sense.") {
			continue
		}
		if v := k.apply(t, 0); v > 0 {
			senses = append(senses, Sense{Name: strings.TrimPrefix(t, "sense."), Range: v})
		}
	}

	// Notes: nothing is silently dropped. Notes, conditional modifiers, and every advantage or
	// disadvantage appear here (skills also show the net flag on their own row).
	notes := []Modifier{}
	for _, m := range mods {
		if isRider(m) {
			continue // listed on the matching attack entries instead
		}
		if m.Kind == ModNote || m.Condition != "" || m.Dice != "" || m.Kind == ModAdvantage || m.Kind == ModDisadvantage {
			notes = append(notes, m)
		}
	}

	attacks := computeAttacks(g, rules, k, modOf, pb)
	casterLevel := computeCasterLevel(g)

	resources := []ResourceView{}
	for _, re := range g.resources {
		used := c.ResourceUsed[re.featureID]
		if re.res.MaxAbility != "" && modOf[re.res.MaxAbility] > re.res.Max {
			re.res.Max = modOf[re.res.MaxAbility]
		}
		remaining := re.res.Max - used
		if remaining < 0 {
			remaining = 0
			warnings = append(warnings, mkWarning(WarnOverCap, re.featureID, "", "resource."+re.featureID, fmt.Sprint(used),
				fmt.Sprintf("%s: %d used but the maximum is %d", re.res.Name, used, re.res.Max)))
		}
		resources = append(resources, ResourceView{
			FeatureID: re.featureID, Name: re.res.Name, Max: re.res.Max,
			Used: used, Remaining: remaining, Recharge: re.res.Recharge,
		})
	}

	spellSources, spellSlots, pactSlots, spellWarns := computeSpells(c, g, lib, rules, k, modOf, pb, totalLevel)
	warnings = append(warnings, spellWarns...)

	// Prerequisites (permissive: warn only).
	lookup := func(target string) (int, bool) {
		switch {
		case target == "level":
			return level, true
		case target == TargetProficiencyBonus:
			return pb, true
		case target == TargetAC:
			return ac, true
		case target == TargetMaxHP:
			return maxHP, true
		case target == TargetInitiative:
			return init, true
		case strings.HasPrefix(target, "ability."):
			a := Ability(strings.TrimPrefix(target, "ability."))
			v, ok := scoreOf[a]
			return v, ok
		case strings.HasPrefix(target, "skill."):
			v, ok := skillBonus[strings.TrimPrefix(target, "skill.")]
			return v, ok
		}
		return 0, false
	}
	for _, f := range g.features {
		for _, p := range f.Prerequisites {
			desc := p.Description
			if p.Target == "" {
				warnings = append(warnings, mkWarning(WarnPrereqUnverifiable, f.ID, "", "", desc,
					fmt.Sprintf("%s: prerequisite %q can't be checked automatically", f.Name, desc)))
				continue
			}
			v, ok := lookup(p.Target)
			val := fmt.Sprintf("%s>=%d", p.Target, p.Min)
			switch {
			case !ok:
				warnings = append(warnings, mkWarning(WarnPrereqUnverifiable, f.ID, "", p.Target, val,
					fmt.Sprintf("%s: can't evaluate prerequisite on %q", f.Name, p.Target)))
			case v < p.Min:
				warnings = append(warnings, mkWarning(WarnPrereqUnmet, f.ID, "", p.Target, val,
					fmt.Sprintf("%s requires %s %d (currently %d)", f.Name, p.Target, p.Min, v)))
			}
		}
	}

	// Table-level cap (0 means no cap).
	if rules.MaxInspiration > 0 && c.Inspiration > rules.MaxInspiration {
		warnings = append(warnings, mkWarning(WarnOverCap, "", "", "inspiration", fmt.Sprint(c.Inspiration),
			fmt.Sprintf("inspiration is %d but the cap is %d", c.Inspiration, rules.MaxInspiration)))
	}

	// Drop duplicate warnings (same key), then mark acknowledged ones.
	acks := map[string]Acknowledgement{}
	for _, a := range c.Acknowledgements {
		acks[a.Key] = a
	}
	seenKey := map[string]bool{}
	finalWarnings := []Warning{}
	for _, w := range warnings {
		if seenKey[w.Key] {
			continue
		}
		seenKey[w.Key] = true
		if a, ok := acks[w.Key]; ok {
			w.Acknowledged, w.AckNote = true, a.Note
		}
		finalWarnings = append(finalWarnings, w)
	}

	features := g.views
	if features == nil {
		features = []FeatureView{}
	}
	pending := g.pending
	if pending == nil {
		pending = []PendingChoice{}
	}

	return Sheet{
		Level:            level,
		ProficiencyBonus: pb,
		MaxHP:            maxHP,
		AC:               ac,
		ACDetail:         acDetail,
		Initiative:       init,
		Abilities:        abilities,
		Skills:           skills,
		Tools:            tools,
		Speeds:           speeds,
		Senses:           senses,
		Resistances:      k.damageList("resist."),
		Immunities:       k.damageList("immune."),
		Vulnerabilities:  k.damageList("vuln."),
		Features:         features,
		Actions:          g.actions,
		Notes:            notes,
		PendingChoices:   pending,
		Warnings:         finalWarnings,
		Languages:        g.languages,
		Attacks:          attacks,
		Resources:        resources,
		CasterLevel:      casterLevel,
		Spellcasting:     spellSources,
		Inventory:        invRes.views,
		Wallet:           invRes.wallet,
		Carry:            invRes.carry,
		SpellSlots:       spellSlots,
		PactSlots:        pactSlots,
		ArmourProfs:      g.armour,
		WeaponProfs:      g.weapons,
	}, nil
}

// ---------------------------------------------------------------------------
// Items and AC
// ---------------------------------------------------------------------------

const (
	WarnUnknownTerm        WarningCode = "unknown_formula_term"
	WarnNotAttuned         WarningCode = "not_attuned"
	WarnMultipleEquipped   WarningCode = "multiple_equipped" // more than one armour or shield
	WarnACRequirementUnmet WarningCode = "ac_requirement_unmet"
)

// applyInventory handles equipped items. Armour and shields always count as worn (for AC formulas
// and NoArmour/NoShield checks). An item's Modifiers and Actions need attunement when the item
// requires it; without it they are skipped and a warning explains why.
func (g *gathered) applyInventory(c Character, lib Library) {
	armourCount, shieldCount := 0, 0
	for i, inv := range c.Inventory {
		if !inv.Equipped {
			continue
		}
		it, err := resolveInvItem(lib, inv, i)
		if err != nil {
			g.warnings = append(g.warnings, mkWarning(WarnUnknownID, inv.ItemID, "", "", inv.ItemID,
				fmt.Sprintf("item %q was not found in the library", inv.ItemID)))
			continue
		}

		switch it.Kind {
		case ItemArmour:
			armourCount++
			g.armourWorn = true
			if it.ACFormula != nil {
				g.acFormulas = append(g.acFormulas, *it.ACFormula)
			}
		case ItemShield:
			shieldCount++
			g.shieldWorn = true
		case ItemWeapon:
			if it.Weapon != nil {
				g.weaponItems = append(g.weaponItems, weaponEntry{
					item:            it,
					abilityOverride: inv.AbilityOverride,
					proficient:      inv.Proficient,
					active:          !it.RequiresAttunement || inv.Attuned,
				})
			}
		}

		if it.RequiresAttunement && !inv.Attuned {
			if len(it.Modifiers) > 0 || len(it.Actions) > 0 || (it.Weapon != nil && it.Weapon.MagicBonus != 0) || it.Spells != nil {
				g.warnings = append(g.warnings, mkWarning(WarnNotAttuned, it.ID, "", "", it.ID,
					fmt.Sprintf("%s is equipped but not attuned, so its magical effects are not applied", it.Name)))
			}
			continue
		}
		for _, m := range it.Modifiers {
			g.mods = append(g.mods, withSource(m, it.ID)) // item id as source: two copies don't stack
		}
		g.actions = append(g.actions, it.Actions...)
		if it.Spells != nil {
			g.addSpellGrant("item:"+it.ID, it.Name, *it.Spells)
		}
	}

	if armourCount > 1 {
		g.warnings = append(g.warnings, mkWarning(WarnMultipleEquipped, "", "", "", "armour",
			"more than one suit of armour is equipped"))
	}
	if shieldCount > 1 {
		g.warnings = append(g.warnings, mkWarning(WarnMultipleEquipped, "", "", "", "shield",
			"more than one shield is equipped"))
	}
}

var defaultBaseAC = ACFormula{
	ID:       "base",
	Name:     "Unarmoured (10 + Dex)",
	NoArmour: true,
	Formula:  Formula{Base: 10, Terms: []Term{{Source: "mod.dex"}}},
}

// evalFormula returns the value and any term sources it couldn't understand (counted as 0).
func evalFormula(f Formula, modOf map[Ability]int, pb, level int, classLevels map[string]int) (int, []string) {
	v := f.Base
	var bad []string
	for _, t := range f.Terms {
		x := 0
		switch {
		case t.Source == "pb":
			x = pb
		case t.Source == "level":
			x = level
		case strings.HasPrefix(t.Source, "class_level."):
			x = classLevels[strings.TrimPrefix(t.Source, "class_level.")]
		case strings.HasPrefix(t.Source, "mod.") && isAbility(strings.TrimPrefix(t.Source, "mod.")):
			x = modOf[Ability(strings.TrimPrefix(t.Source, "mod."))]
		default:
			bad = append(bad, t.Source)
			continue
		}
		if t.Div > 1 {
			x /= t.Div
		}
		if t.Max != nil && x > *t.Max {
			x = *t.Max
		}
		v += x
	}
	return v, bad
}

// spellACFormulas returns the AC formulas of spells the character can cast: each class source's
// cantrips and known (or prepared) spells, plus spells granted by features and items.
func spellACFormulas(c Character, g *gathered, lib Library) []ACFormula {
	var ids []string
	for _, ce := range g.casters {
		if ce.level < startLevel(ce.def) {
			continue
		}
		state := c.Spellbooks["class:"+ce.classID]
		ids = append(ids, state.Cantrips...)
		switch ce.def.Prep {
		case PrepPrepared, PrepBook:
			ids = append(ids, state.Prepared...)
		default:
			ids = append(ids, state.Known...)
		}
	}
	for _, ge := range g.spellGrants {
		ids = append(ids, ge.grant.SpellIDs...)
	}
	var out []ACFormula
	for _, id := range ids {
		if sp, err := lib.Spell(id); err == nil && sp.ACFormula != nil {
			out = append(out, *sp.ACFormula)
		}
	}
	return out
}

// computeAC evaluates every available formula, picks one, then lets the normal modifier step
// (shield +2, magic bonuses, Set/Min effects) apply on top.
//
// Pick order: the character's ACFormulaOverride if it names a known formula (even an ineligible
// one: that only warns); otherwise the highest eligible formula, ties going to the lowest ID;
// if nothing is eligible, the baseline with a warning. Spell formulas (Mage Armor) are only
// available while the spell is castable, and only used through the override: the sheet can't
// know whether the spell is currently active.
func computeAC(c Character, g *gathered, lib Library, rules Ruleset, k *calc, modOf map[Ability]int, pb, level int) (ACBreakdown, []Warning) {
	var warns []Warning

	base := defaultBaseAC
	if rules.BaseAC != nil {
		base = *rules.BaseAC
	}

	// Candidates: baseline + feature formulas + armour formulas, de-duplicated by ID, sorted by ID
	// so results are deterministic.
	candidates := []ACFormula{base}
	seen := map[string]bool{base.ID: true}
	for _, f := range g.acFormulas {
		if seen[f.ID] {
			continue
		}
		seen[f.ID] = true
		candidates = append(candidates, f)
	}
	fromSpell := map[string]bool{}
	for _, f := range spellACFormulas(c, g, lib) {
		if seen[f.ID] {
			continue
		}
		seen[f.ID] = true
		fromSpell[f.ID] = true
		candidates = append(candidates, f)
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })

	type scored struct {
		f        ACFormula
		value    int
		eligible bool
		reason   string
	}
	all := make([]scored, 0, len(candidates))
	for _, f := range candidates {
		v, bad := evalFormula(f.Formula, modOf, pb, level, g.classLevels)
		for _, src := range bad {
			warns = append(warns, mkWarning(WarnUnknownTerm, f.ID, "", "", src,
				fmt.Sprintf("AC formula %q has an unknown term %q (counted as 0)", f.Name, src)))
		}
		s := scored{f: f, value: v, eligible: true}
		switch {
		case f.NoArmour && g.armourWorn:
			s.eligible, s.reason = false, "requires no armour"
		case f.NoShield && g.shieldWorn:
			s.eligible, s.reason = false, "requires no shield"
		}
		all = append(all, s)
	}

	chosen, overridden := -1, false
	if c.ACFormulaOverride != "" {
		for i, s := range all {
			if s.f.ID == c.ACFormulaOverride {
				chosen, overridden = i, true
				break
			}
		}
		switch {
		case chosen < 0:
			warns = append(warns, mkWarning(WarnUnknownID, c.ACFormulaOverride, "", "ac_formula_override", c.ACFormulaOverride,
				fmt.Sprintf("AC formula override %q isn't available to this character; using the best eligible formula", c.ACFormulaOverride)))
		case !all[chosen].eligible:
			warns = append(warns, mkWarning(WarnACRequirementUnmet, all[chosen].f.ID, "", "", all[chosen].f.ID,
				fmt.Sprintf("AC formula %q is forced but %s", all[chosen].f.Name, all[chosen].reason)))
		}
	}
	if chosen < 0 {
		for i, s := range all {
			if s.eligible && !fromSpell[s.f.ID] && (chosen < 0 || s.value > all[chosen].value) {
				chosen = i
			}
		}
	}
	if chosen < 0 { // nothing eligible (e.g. armour worn with no formula): fall back to the baseline
		for i, s := range all {
			if s.f.ID == base.ID {
				chosen = i
			}
		}
		warns = append(warns, mkWarning(WarnACRequirementUnmet, base.ID, "", "", "none_eligible",
			"no AC formula is eligible; falling back to the baseline"))
	}

	win := all[chosen]
	total := k.apply(TargetAC, win.value)

	options := make([]ACOption, 0, len(all))
	for _, s := range all {
		options = append(options, ACOption{ID: s.f.ID, Name: s.f.Name, Value: s.value, Eligible: s.eligible, Reason: s.reason, FromSpell: fromSpell[s.f.ID]})
	}
	sort.SliceStable(options, func(i, j int) bool {
		if options[i].Eligible != options[j].Eligible {
			return options[i].Eligible
		}
		return options[i].Value > options[j].Value
	})

	return ACBreakdown{
		Total:        total,
		FormulaID:    win.f.ID,
		Formula:      win.f.Name,
		FormulaValue: win.value,
		Bonus:        total - win.value,
		Overridden:   overridden,
		Options:      options,
	}, warns
}

// ---------------------------------------------------------------------------
// Weapons, attacks and dice riders
// ---------------------------------------------------------------------------

const WarnBadDice WarningCode = "bad_dice"

var diceRe = regexp.MustCompile(`^[1-9][0-9]*d[1-9][0-9]*$`)

var riderTargets = map[string]bool{
	TargetAttackAll: true, TargetAttackMelee: true, TargetAttackRanged: true,
	TargetDamageAll: true, TargetDamageMelee: true, TargetDamageRanged: true, TargetDamageSpell: true,
}

// isRider: a dice modifier on an attack or damage target. These appear on attack entries.
func isRider(m Modifier) bool {
	return m.Kind == ModAdd && m.Dice != "" && riderTargets[m.Target]
}

// riders lists dice riders (valid dice only) across the given targets.
func (k *calc) riders(targets ...string) []Rider {
	var out []Rider
	for _, t := range targets {
		for _, m := range k.byTarget[t] {
			if m.Kind == ModAdd && m.Dice != "" && diceRe.MatchString(m.Dice) {
				out = append(out, Rider{Dice: m.Dice, Condition: m.Condition, Note: m.Note, Source: m.SourceID})
			}
		}
	}
	return out
}

type weaponEntry struct {
	item            Item
	abilityOverride Ability
	proficient      bool // forced on this inventory entry
	active          bool // false when attunement is required but missing: magic bonus is not applied
}

type attackSpec struct {
	name            string
	itemID          string
	d               AttackDetails
	versatile       string
	magic           int
	proficient      bool
	abilityOverride Ability
}

var defaultUnarmed = WeaponStats{
	Category: "unarmed",
	Attack:   AttackDetails{FlatDamage: 1, DamageType: "bludgeoning", RangeNormal: 5},
}

// attackAbility: explicit override, then the attack's own ability, then automatic:
// ranged uses Dex, finesse takes the better of Str and Dex, everything else Str.
func attackAbility(d AttackDetails, override Ability, modOf map[Ability]int) Ability {
	if override != "" {
		return override
	}
	if d.Ability != "" {
		return d.Ability
	}
	switch {
	case d.Ranged:
		return Dex
	case contains(d.Properties, "finesse"):
		if modOf[Dex] > modOf[Str] {
			return Dex
		}
	}
	return Str
}

func buildAttack(sp attackSpec, k *calc, modOf map[Ability]int, pb int) AttackView {
	atkTarget, dmgTarget := TargetAttackMelee, TargetDamageMelee
	if sp.d.Ranged {
		atkTarget, dmgTarget = TargetAttackRanged, TargetDamageRanged
	}
	atkTargets := []string{TargetAttackAll, atkTarget}
	dmgTargets := []string{TargetDamageAll, dmgTarget}

	ab := attackAbility(sp.d, sp.abilityOverride, modOf)

	toHit := modOf[ab] + sp.magic + k.sum(atkTargets...)
	if sp.proficient {
		toHit += pb
	}
	damage := modOf[ab] + sp.magic + sp.d.FlatDamage + k.sum(dmgTargets...)

	adv := k.flag(ModAdvantage, atkTargets...)
	dis := k.flag(ModDisadvantage, atkTargets...)

	return AttackView{
		Name:          sp.name,
		ItemID:        sp.itemID,
		Ability:       ab,
		Proficient:    sp.proficient,
		ToHit:         toHit,
		Advantage:     adv && !dis, // both present cancel out
		Disadvantage:  dis && !adv,
		DamageDice:    sp.d.DamageDice,
		DamageBonus:   damage,
		DamageType:    sp.d.DamageType,
		VersatileDice: sp.versatile,
		RangeNormal:   sp.d.RangeNormal,
		RangeLong:     sp.d.RangeLong,
		Properties:    sp.d.Properties,
		AttackRiders:  k.riders(atkTargets...),
		DamageRiders:  k.riders(dmgTargets...),
	}
}

// computeAttacks builds attack entries for equipped weapons, then feature-granted attack actions,
// then unarmed strike (always available).
func computeAttacks(g *gathered, rules Ruleset, k *calc, modOf map[Ability]int, pb int) []AttackView {
	out := []AttackView{}

	for _, w := range g.weaponItems {
		ws := *w.item.Weapon
		magic := 0
		if w.active {
			magic = ws.MagicBonus
		}
		out = append(out, buildAttack(attackSpec{
			name:            w.item.Name,
			itemID:          w.item.ID,
			d:               ws.Attack,
			versatile:       ws.VersatileDice,
			magic:           magic,
			proficient:      w.proficient || contains(g.weapons, ws.Category) || contains(g.weapons, w.item.ID),
			abilityOverride: w.abilityOverride,
		}, k, modOf, pb))
	}

	for _, a := range g.actions {
		if a.IsAttack && a.Attack != nil {
			// Feature-granted attacks (natural weapons, class features) are treated as proficient.
			out = append(out, buildAttack(attackSpec{name: a.Name, d: *a.Attack, proficient: true}, k, modOf, pb))
		}
	}

	unarmed := defaultUnarmed
	if rules.Unarmed != nil {
		unarmed = *rules.Unarmed
	}
	out = append(out, buildAttack(attackSpec{
		name: "Unarmed Strike", d: unarmed.Attack, versatile: unarmed.VersatileDice,
		magic: unarmed.MagicBonus, proficient: true,
	}, k, modOf, pb))

	return out
}

// ---------------------------------------------------------------------------
// Classes: expansion, caster level, resources
// ---------------------------------------------------------------------------

const (
	WarnMissingLevelData WarningCode = "missing_level_data" // class/subclass table shorter than the character's level
	WarnSubclassEarly    WarningCode = "subclass_too_early"
)

type casterEntry struct {
	classID string
	name    string
	level   int
	def     SpellcastingDef
	extra   []string // spells the chosen subclass adds to the class list
}

type resourceEntry struct {
	featureID string
	res       Resource
}

// expandClasses turns each ClassLevel into grants, features and pending items:
//   - first class: StartingGrants (incl. save proficiencies) and starting choices;
//     later classes: MulticlassGrants and a prerequisite check
//   - every feature in the level table up to the current level, scaled by that class level
//   - the chosen subclass's table; or a pending "choose a subclass" once it unlocks
//   - a pending "roll hit points" item for each level without a rolled HP entry
func (g *gathered) expandClasses(c Character, lib Library, queue []queued, seen map[string]bool) []queued {
	for i, cl := range c.Classes {
		class, err := lib.Class(cl.ClassID)
		if err != nil {
			g.warnings = append(g.warnings, mkWarning(WarnUnknownID, cl.ClassID, "", "", cl.ClassID,
				fmt.Sprintf("class %q was not found in the library", cl.ClassID)))
			continue
		}
		g.classLevels[cl.ClassID] += cl.Level

		maxLevel := class.MaxLevel
		if maxLevel == 0 {
			maxLevel = 20
		}
		if cl.Level > maxLevel {
			g.warnings = append(g.warnings, mkWarning(WarnOverCap, class.ID, "", "class."+class.ID, fmt.Sprint(cl.Level),
				fmt.Sprintf("%s level %d is above the maximum of %d", class.Name, cl.Level, maxLevel)))
		}

		at := func(id string, gained int) queued {
			return queued{id: id, classLevel: cl.Level, gained: gained, origin: OriginClass, sourceID: class.ID}
		}

		grants := class.MulticlassGrants
		if i == 0 {
			grants = class.StartingGrants
			if len(class.StartingChoices) > 0 {
				start := Feature{
					ID:      "class:" + class.ID + ":start",
					Name:    class.Name + " (starting choices)",
					Choices: class.StartingChoices,
				}
				seen[start.ID] = true
				for _, id := range g.applyFeature(c, start, cl.Level) {
					queue = append(queue, at(id, 1))
				}
			}
		} else if len(class.MulticlassPrereq) > 0 {
			// A feature with no effects, so the normal prerequisite check reports on it.
			g.features = append(g.features, Feature{
				ID:            "class:" + class.ID + ":multiclass",
				Name:          class.Name + " multiclassing",
				Prerequisites: class.MulticlassPrereq,
			})
		}
		g.addGrants(grants)
		for _, id := range grants.FeatureIDs {
			queue = append(queue, at(id, 1))
		}

		for lvl := 1; lvl <= cl.Level && lvl-1 < len(class.Levels); lvl++ {
			for _, id := range class.Levels[lvl-1] {
				queue = append(queue, at(id, lvl))
			}
		}
		if cl.Level > len(class.Levels) {
			g.warnings = append(g.warnings, mkWarning(WarnMissingLevelData, class.ID, "", "", fmt.Sprintf("%s:%d", class.ID, cl.Level),
				fmt.Sprintf("%s has no feature table for levels above %d", class.Name, len(class.Levels))))
		}

		var subExtra []string
		if cl.SubclassID != "" {
			var sub *Subclass
			for j := range class.Subclasses {
				if class.Subclasses[j].ID == cl.SubclassID {
					sub = &class.Subclasses[j]
				}
			}
			if sub == nil {
				g.warnings = append(g.warnings, mkWarning(WarnUnknownID, class.ID, "", "", cl.SubclassID,
					fmt.Sprintf("subclass %q was not found for %s", cl.SubclassID, class.Name)))
			} else {
				if class.SubclassLevel > 0 && cl.Level < class.SubclassLevel {
					g.warnings = append(g.warnings, mkWarning(WarnSubclassEarly, class.ID, "", "", cl.SubclassID,
						fmt.Sprintf("%s subclasses are normally chosen at level %d", class.Name, class.SubclassLevel)))
				}
				for lvl := 1; lvl <= cl.Level && lvl-1 < len(sub.Levels); lvl++ {
					for _, id := range sub.Levels[lvl-1] {
						queue = append(queue, at(id, lvl))
					}
				}
				subExtra = sub.ExpandedSpells
			}
		} else if class.SubclassLevel > 0 && cl.Level >= class.SubclassLevel && len(class.Subclasses) > 0 {
			g.pending = append(g.pending, PendingChoice{
				FeatureID: "class:" + class.ID, ChoiceID: "subclass",
				Prompt: "Choose a subclass for " + class.Name, Remaining: 1,
			})
		}

		if missing := cl.Level - len(cl.RolledHP); missing > 0 {
			g.pending = append(g.pending, PendingChoice{
				FeatureID: "class:" + class.ID, ChoiceID: "rolled_hp",
				Prompt: "Roll hit points for " + class.Name, Remaining: missing,
			})
		}

		if class.Spellcasting != nil {
			g.casters = append(g.casters, casterEntry{classID: class.ID, name: class.Name, level: cl.Level, def: *class.Spellcasting, extra: subExtra})
		}
	}
	return queue
}

// ---------------------------------------------------------------------------
// Spells
// ---------------------------------------------------------------------------

const (
	WarnSpellTooHigh     WarningCode = "spell_too_high"
	WarnNoCastingAbility WarningCode = "no_casting_ability"
)

// Standard slot table: row = caster level, entries = slots for spell levels 1..9.
var defaultSlotTable = [][]int{
	{2},
	{3},
	{4, 2},
	{4, 3},
	{4, 3, 2},
	{4, 3, 3},
	{4, 3, 3, 1},
	{4, 3, 3, 2},
	{4, 3, 3, 3, 1},
	{4, 3, 3, 3, 2},
	{4, 3, 3, 3, 2, 1},
	{4, 3, 3, 3, 2, 1},
	{4, 3, 3, 3, 2, 1, 1},
	{4, 3, 3, 3, 2, 1, 1},
	{4, 3, 3, 3, 2, 1, 1, 1},
	{4, 3, 3, 3, 2, 1, 1, 1},
	{4, 3, 3, 3, 2, 1, 1, 1, 1},
	{4, 3, 3, 3, 3, 1, 1, 1, 1},
	{4, 3, 3, 3, 3, 2, 1, 1, 1},
	{4, 3, 3, 3, 3, 2, 2, 1, 1},
}

// Standard pact table: row = warlock level.
var defaultPactTable = []PactRow{
	{1, 1}, {2, 1}, {2, 2}, {2, 2}, {2, 3}, {2, 3}, {2, 4}, {2, 4}, {2, 5}, {2, 5},
	{3, 5}, {3, 5}, {3, 5}, {3, 5}, {3, 5}, {3, 5}, {4, 5}, {4, 5}, {4, 5}, {4, 5},
}

var diceParts = regexp.MustCompile(`^([1-9][0-9]*)d([1-9][0-9]*)$`)

// addDice returns base plus extra repeated times: addDice("8d6", "1d6", 2) = "10d6".
// Different die sizes are listed side by side. Unparseable input returns base unchanged.
func addDice(base, extra string, times int) string {
	b := diceParts.FindStringSubmatch(base)
	x := diceParts.FindStringSubmatch(extra)
	if b == nil || x == nil {
		return base
	}
	bc, _ := strconv.Atoi(b[1])
	xc, _ := strconv.Atoi(x[1])
	if b[2] == x[2] {
		return fmt.Sprintf("%dd%s", bc+xc*times, b[2])
	}
	return fmt.Sprintf("%s+%dd%s", base, xc*times, x[2])
}

func bestDiceTier(tiers []DiceTier, level int) *DiceTier {
	var best *DiceTier
	for i := range tiers {
		t := &tiers[i]
		if t.MinLevel <= level && (best == nil || t.MinLevel > best.MinLevel) {
			best = t
		}
	}
	return best
}

func startLevel(def SpellcastingDef) int {
	if def.StartLevel <= 0 {
		return 1
	}
	return def.StartLevel
}

// tableAt reads a per-class-level table. An empty table means "not specified" (-1).
func tableAt(tbl []int, level int) int {
	if len(tbl) == 0 {
		return -1
	}
	i := level - 1
	if i < 0 {
		i = 0
	}
	if i >= len(tbl) {
		i = len(tbl) - 1
	}
	return tbl[i]
}

// computeCasterLevel is the combined level used for multiclass slot tables.
// Each non-pact caster that has started casting adds level/divisor (rounded down, or up if the class says so).
func computeCasterLevel(g *gathered) int {
	total := 0
	for _, ce := range g.casters {
		d := ce.def.Divisor
		if ce.def.Pact || d <= 0 || ce.level < startLevel(ce.def) {
			continue
		}
		if ce.def.RoundUp {
			total += (ce.level + d - 1) / d
		} else {
			total += ce.level / d
		}
	}
	return total
}

type spellGrantEntry struct {
	id, name string
	grant    SpellGrant
}

func (g *gathered) addSpellGrant(id, name string, grant SpellGrant) {
	for _, e := range g.spellGrants {
		if e.id == id {
			return // the same feature or item twice counts once
		}
	}
	g.spellGrants = append(g.spellGrants, spellGrantEntry{id: id, name: name, grant: grant})
	if grant.Resource != nil {
		g.resources = append(g.resources, resourceEntry{featureID: id, res: *grant.Resource})
	}
}

// computeSlots works out the shared slot pool and the pact pool.
// One casting class uses its own table (row = level rounded up by its divisor, which reproduces
// the Paladin, Ranger, third-caster and Artificer tables); several use the combined caster level.
func computeSlots(c Character, g *gathered, rules Ruleset) (slots []SlotView, pact *SlotView, maxSlotLevel, pactLevel int, warns []Warning) {
	var eligible, pacts []casterEntry
	for _, ce := range g.casters {
		if ce.level < startLevel(ce.def) {
			continue
		}
		switch {
		case ce.def.Pact:
			pacts = append(pacts, ce)
		case ce.def.Divisor > 0:
			eligible = append(eligible, ce)
		}
	}

	row := 0
	switch len(eligible) {
	case 0:
	case 1:
		ce := eligible[0]
		row = (ce.level + ce.def.Divisor - 1) / ce.def.Divisor
	default:
		row = computeCasterLevel(g)
	}

	table := rules.SlotTable
	if table == nil {
		table = defaultSlotTable
	}
	if row > len(table) {
		row = len(table)
	}
	var maxes [9]int
	if row > 0 {
		for i, n := range table[row-1] {
			if i < 9 {
				maxes[i] = n
			}
		}
	}

	slots = []SlotView{}
	for lvl := 1; lvl <= 9; lvl++ {
		mx := maxes[lvl-1]
		used := c.SpellSlotsUsed[lvl]
		if mx > 0 {
			maxSlotLevel = lvl
		}
		if mx == 0 && used == 0 {
			continue
		}
		rem := mx - used
		if rem < 0 {
			rem = 0
			warns = append(warns, mkWarning(WarnOverCap, "", "", fmt.Sprintf("spell_slots.%d", lvl), fmt.Sprint(used),
				fmt.Sprintf("%d level %d slots used but only %d available", used, lvl, mx)))
		}
		slots = append(slots, SlotView{Level: lvl, Max: mx, Used: used, Remaining: rem})
	}

	if len(pacts) > 0 {
		ptable := rules.PactTable
		if ptable == nil {
			ptable = defaultPactTable
		}
		i := pacts[0].level - 1
		if i >= len(ptable) {
			i = len(ptable) - 1
		}
		if i >= 0 {
			pr := ptable[i]
			pactLevel = pr.Level
			used := c.PactSlotsUsed
			rem := pr.Slots - used
			if rem < 0 {
				rem = 0
				warns = append(warns, mkWarning(WarnOverCap, "", "", "pact_slots", fmt.Sprint(used),
					fmt.Sprintf("%d pact slots used but only %d available", used, pr.Slots)))
			}
			pact = &SlotView{Level: pr.Level, Max: pr.Slots, Used: used, Remaining: rem}
		}
	}
	return slots, pact, maxSlotLevel, pactLevel, warns
}

type castSource struct {
	ability      Ability
	mod          int
	dc           int
	attack       int
	maxSlotLevel int // highest slot level this source can upcast to; 0 for innate sources
}

// buildSpellView resolves one spell for a source: DC, attack bonus, scaled dice, upcasting, riders.
func buildSpellView(sp Spell, cs castSource, k *calc, totalLevel int, prepared bool) SpellView {
	v := SpellView{
		ID: sp.ID, Name: sp.Name, Level: sp.Level, School: sp.School,
		CastingTime: sp.CastingTime, Range: sp.Range, Components: sp.Components, Duration: sp.Duration,
		Ritual: sp.Ritual, Concentration: sp.Concentration, Prepared: prepared,
		Description: sp.Description, HigherLevels: sp.HigherLevels, Attack: sp.Attack,
	}
	if sp.Attack {
		v.AttackBonus = cs.attack
	}
	if sp.Save != "" {
		v.SaveAbility, v.SaveDC, v.SaveEffect = sp.Save, cs.dc, sp.SaveEffect
	}
	for _, e := range sp.Effects {
		dice := e.Dice
		if t := bestDiceTier(e.DiceTiers, totalLevel); t != nil {
			dice = t.Dice
		}
		bonus := 0
		if e.AddAbilityMod {
			bonus += cs.mod
		}
		var riders []Rider
		if e.Kind == "damage" {
			bonus += k.sum(TargetDamageSpell)
			riders = k.riders(TargetDamageSpell)
		}
		ev := SpellEffectView{Kind: e.Kind, Dice: dice, DamageType: e.DamageType, Bonus: bonus, Riders: riders, Note: e.Note}
		if e.UpcastDice != "" && sp.Level >= 1 {
			for lvl := sp.Level + 1; lvl <= cs.maxSlotLevel; lvl++ {
				ev.Upcast = append(ev.Upcast, UpcastEntry{SlotLevel: lvl, Dice: addDice(dice, e.UpcastDice, lvl-sp.Level)})
			}
		}
		v.Effects = append(v.Effects, ev)
	}
	return v
}

// computeSpells builds a view for every spell source (classes, then feature and item grants),
// the slot pools, and permissive warnings. Missing cantrip/spell picks are added to g.pending.
func computeSpells(c Character, g *gathered, lib Library, rules Ruleset, k *calc, modOf map[Ability]int, pb, totalLevel int) ([]SpellSourceView, []SlotView, *SlotView, []Warning) {
	slots, pact, maxSlot, pactLevel, warns := computeSlots(c, g, rules)
	sources := []SpellSourceView{}

	look := func(srcID, spellID string) (Spell, bool) {
		sp, err := lib.Spell(spellID)
		if err != nil {
			warns = append(warns, mkWarning(WarnUnknownID, srcID, "", "", spellID,
				fmt.Sprintf("spell %q was not found in the library", spellID)))
			return Spell{}, false
		}
		return sp, true
	}

	makeSource := func(ability Ability, srcID string) castSource {
		cs := castSource{ability: ability}
		if ability == "" {
			warns = append(warns, mkWarning(WarnNoCastingAbility, srcID, "", "", srcID,
				"no casting ability is set, so the modifier counts as +0"))
		} else {
			cs.mod = modOf[ability]
		}
		cs.dc = k.apply(TargetSpellSaveDC, 8+pb+cs.mod)
		cs.attack = k.apply(TargetSpellAttack, pb+cs.mod) + k.sum(TargetAttackAll)
		return cs
	}

	// Class sources.
	for _, ce := range g.casters {
		def := ce.def
		if ce.level < startLevel(def) {
			continue
		}
		srcID := "class:" + ce.classID
		pendID := "spells:" + srcID
		cs := makeSource(def.Ability, srcID)
		cs.maxSlotLevel = maxSlot
		if def.Pact {
			cs.maxSlotLevel = pactLevel
		}
		state := c.Spellbooks[srcID]

		mode := def.Prep
		if mode == "" {
			mode = PrepKnown
		}

		var onList map[string]bool
		if def.SpellListID != "" {
			sl, err := lib.SpellList(def.SpellListID)
			if err != nil {
				warns = append(warns, mkWarning(WarnUnknownID, srcID, "", "", def.SpellListID,
					fmt.Sprintf("spell list %q was not found in the library", def.SpellListID)))
			} else {
				onList = map[string]bool{}
				for _, id := range sl.SpellIDs {
					onList[id] = true
				}
				for _, id := range ce.extra {
					onList[id] = true
				}
			}
		}
		checkList := func(ids []string, choice string) {
			if onList == nil {
				return
			}
			for _, id := range ids {
				if !onList[id] {
					warns = append(warns, mkWarning(WarnNotInPool, pendID, choice, "", id,
						fmt.Sprintf("%q is not on the %s spell list", id, ce.name)))
				}
			}
		}
		countCheck := func(choice, prompt string, have, allowed int) {
			switch {
			case allowed < 0:
			case have > allowed:
				warns = append(warns, mkWarning(WarnOverCount, pendID, choice, "", fmt.Sprintf("%d/%d", have, allowed),
					fmt.Sprintf("%d %s chosen but %s allows %d", have, choice, ce.name, allowed)))
			case have < allowed:
				g.pending = append(g.pending, PendingChoice{
					FeatureID: pendID, ChoiceID: choice, Prompt: prompt, Remaining: allowed - have,
				})
			}
		}

		cantripsAllowed := tableAt(def.CantripsKnown, ce.level)
		spellsAllowed := -1
		var shown []string
		switch mode {
		case PrepPrepared, PrepBook:
			if def.PreparedFormula != nil {
				v, bad := evalFormula(*def.PreparedFormula, modOf, pb, totalLevel, g.classLevels)
				for _, src := range bad {
					warns = append(warns, mkWarning(WarnUnknownTerm, srcID, "", "", src,
						fmt.Sprintf("prepared-spell formula for %s has an unknown term %q (counted as 0)", ce.name, src)))
				}
				if v < 1 {
					v = 1 // a prepared caster always prepares at least one spell
				}
				spellsAllowed = v
			}
			shown = state.Prepared
			if mode == PrepPrepared {
				checkList(state.Prepared, "prepared")
			} else {
				checkList(state.Book, "book")
				for _, id := range state.Prepared {
					if !contains(state.Book, id) {
						warns = append(warns, mkWarning(WarnNotInPool, pendID, "prepared", "", id,
							fmt.Sprintf("%q is prepared but is not in the spellbook", id)))
					}
				}
			}
			countCheck("prepared", "Prepare spells for "+ce.name, len(state.Prepared), spellsAllowed)
		default:
			spellsAllowed = tableAt(def.SpellsKnown, ce.level)
			shown = state.Known
			checkList(state.Known, "known")
			countCheck("known", "Choose spells known for "+ce.name, len(state.Known), spellsAllowed)
		}
		checkList(state.Cantrips, "cantrips")
		countCheck("cantrips", "Choose cantrips for "+ce.name, len(state.Cantrips), cantripsAllowed)

		src := SpellSourceView{
			ID: srcID, Name: ce.name, Kind: SourceClass, Ability: def.Ability,
			SaveDC: cs.dc, AttackBonus: cs.attack, Prep: mode,
			CantripsAllowed: cantripsAllowed, SpellsAllowed: spellsAllowed,
			Cantrips: []SpellView{}, Spells: []SpellView{},
		}
		for _, id := range state.Cantrips {
			if sp, ok := look(srcID, id); ok {
				src.Cantrips = append(src.Cantrips, buildSpellView(sp, cs, k, totalLevel, true))
			}
		}
		for _, id := range shown {
			sp, ok := look(srcID, id)
			if !ok {
				continue
			}
			if sp.Level > cs.maxSlotLevel {
				warns = append(warns, mkWarning(WarnSpellTooHigh, pendID, "", "", id,
					fmt.Sprintf("%s is level %d but %s can only cast up to level %d right now", sp.Name, sp.Level, ce.name, cs.maxSlotLevel)))
			}
			src.Spells = append(src.Spells, buildSpellView(sp, cs, k, totalLevel, true))
		}
		sources = append(sources, src)
	}

	// Feature and item sources: innate casting, no slots, no upcasting.
	for _, ge := range g.spellGrants {
		cs := makeSource(ge.grant.Ability, ge.id)
		kind := SourceFeature
		if strings.HasPrefix(ge.id, "item:") {
			kind = SourceItem
		}
		src := SpellSourceView{
			ID: ge.id, Name: ge.name, Kind: kind, Ability: ge.grant.Ability,
			SaveDC: cs.dc, AttackBonus: cs.attack, Prep: "innate",
			CantripsAllowed: -1, SpellsAllowed: -1,
			Cantrips: []SpellView{}, Spells: []SpellView{},
		}
		for _, id := range ge.grant.SpellIDs {
			sp, ok := look(ge.id, id)
			if !ok {
				continue
			}
			v := buildSpellView(sp, cs, k, totalLevel, true)
			if sp.Level == 0 {
				src.Cantrips = append(src.Cantrips, v)
			} else {
				src.Spells = append(src.Spells, v)
			}
		}
		sources = append(sources, src)
	}

	if c.Concentrating != "" {
		look("concentration", c.Concentrating) // warns if the spell id is unknown
	}

	return sources, slots, pact, warns
}
