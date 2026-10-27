package character

// loader.go - reads content packs (folders of JSON) into a Library and validates them.
//
// A pack is a directory:
//
//	pack.json                  optional: {"id": "...", "name": "...", "version": "..."}
//	skills.json                optional: an array of skill definitions
//	features/ items/ classes/ spells/ spell_lists/
//	                           any depth; every .json file holds one object or an array of them
//
// Packs load in the order given. A later pack replaces an earlier entry with the same id
// (reported as a warning), so homebrew can override core content just by reusing an id.
// Unknown JSON fields are errors: typos in homebrew are caught instead of silently ignored.
//
// Loading never stops at the first problem. Everything is collected as Issues (errors and
// warnings, with pack, file and id); bad entries are skipped, the rest still load.
// LoadOptions.Strict turns "any error issue" into a returned error, for CI or a validate command.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type Severity string

const (
	SevError   Severity = "error"
	SevWarning Severity = "warning"
)

// Issue is one problem found while loading or validating content.
type Issue struct {
	Severity Severity `json:"severity"`
	Pack     string   `json:"pack,omitempty"`
	File     string   `json:"file,omitempty"`
	Kind     string   `json:"kind,omitempty"` // feature, item, class, spell, spell_list, skill
	ID       string   `json:"id,omitempty"`
	Message  string   `json:"message"`
}

func (i Issue) String() string {
	loc := i.File
	if i.Pack != "" {
		loc = i.Pack + "/" + i.File
	}
	who := ""
	if i.Kind != "" && i.ID != "" {
		who = fmt.Sprintf(" [%s %s]", i.Kind, i.ID)
	}
	return fmt.Sprintf("%s: %s%s: %s", i.Severity, loc, who, i.Message)
}

// PackSource is one pack to load. Name is used in issue messages unless pack.json gives an id.
type PackSource struct {
	Name string
	FS   fs.FS
}

type LoadOptions struct {
	Strict bool // return an error if any error-severity issue was found
}

type srcInfo struct{ pack, file string }

// ContentLibrary is the Library implementation backed by loaded packs.
type ContentLibrary struct {
	features map[string]Feature
	items    map[string]Item
	classes  map[string]Class
	spells   map[string]Spell
	lists    map[string]SpellList
	skills   []SkillDef
	skillIdx map[string]int
	src      map[string]srcInfo // "kind:id" -> where it came from
	issues   []Issue
}

var _ Library = (*ContentLibrary)(nil)

func newContentLibrary() *ContentLibrary {
	return &ContentLibrary{
		features: map[string]Feature{},
		items:    map[string]Item{},
		classes:  map[string]Class{},
		spells:   map[string]Spell{},
		lists:    map[string]SpellList{},
		skillIdx: map[string]int{},
		src:      map[string]srcInfo{},
	}
}

// ---------------------------------------------------------------------------
// Library implementation and accessors
// ---------------------------------------------------------------------------

func (l *ContentLibrary) Feature(id string) (Feature, error) {
	if f, ok := l.features[id]; ok {
		return f, nil
	}
	return Feature{}, fmt.Errorf("feature %q not found", id)
}

func (l *ContentLibrary) Item(id string) (Item, error) {
	if it, ok := l.items[id]; ok {
		return it, nil
	}
	return Item{}, fmt.Errorf("item %q not found", id)
}

func (l *ContentLibrary) Class(id string) (Class, error) {
	if c, ok := l.classes[id]; ok {
		return c, nil
	}
	return Class{}, fmt.Errorf("class %q not found", id)
}

func (l *ContentLibrary) Spell(id string) (Spell, error) {
	if s, ok := l.spells[id]; ok {
		return s, nil
	}
	return Spell{}, fmt.Errorf("spell %q not found", id)
}

func (l *ContentLibrary) SpellList(id string) (SpellList, error) {
	if s, ok := l.lists[id]; ok {
		return s, nil
	}
	return SpellList{}, fmt.Errorf("spell list %q not found", id)
}

func (l *ContentLibrary) Skills() []SkillDef {
	return append([]SkillDef(nil), l.skills...)
}

// Issues returns everything found while loading and validating, in discovery order.
func (l *ContentLibrary) Issues() []Issue {
	return append([]Issue(nil), l.issues...)
}

func (l *ContentLibrary) HasErrors() bool {
	return l.errorCount() > 0
}

func (l *ContentLibrary) errorCount() int {
	n := 0
	for _, is := range l.issues {
		if is.Severity == SevError {
			n++
		}
	}
	return n
}

// Counts reports how much of each kind of content is loaded.
func (l *ContentLibrary) Counts() map[string]int {
	return map[string]int{
		"features":    len(l.features),
		"items":       len(l.items),
		"classes":     len(l.classes),
		"spells":      len(l.spells),
		"spell_lists": len(l.lists),
		"skills":      len(l.skills),
	}
}

func (l *ContentLibrary) addIssue(sev Severity, pack, file, kind, id, format string, args ...any) {
	l.issues = append(l.issues, Issue{
		Severity: sev, Pack: pack, File: file, Kind: kind, ID: id,
		Message: fmt.Sprintf(format, args...),
	})
}

// issueAt attaches the pack and file the entry was loaded from.
func (l *ContentLibrary) issueAt(sev Severity, kind, id, format string, args ...any) {
	s := l.src[kind+":"+id]
	l.addIssue(sev, s.pack, s.file, kind, id, format, args...)
}

// ---------------------------------------------------------------------------
// Loading
// ---------------------------------------------------------------------------

// LoadDirs loads packs from directories, in order.
func LoadDirs(opts LoadOptions, dirs ...string) (*ContentLibrary, error) {
	packs := make([]PackSource, 0, len(dirs))
	for _, d := range dirs {
		info, err := os.Stat(d)
		if err != nil {
			return nil, fmt.Errorf("content pack %q: %w", d, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("content pack %q is not a directory", d)
		}
		packs = append(packs, PackSource{Name: filepath.Base(d), FS: os.DirFS(d)})
	}
	return LoadPacks(opts, packs...)
}

// LoadPacks loads packs from any fs.FS (directories, embedded files, in-memory test data).
// The returned library is non-nil whenever loading got far enough to produce issues, even if
// Strict also returns an error.
func LoadPacks(opts LoadOptions, packs ...PackSource) (*ContentLibrary, error) {
	lib := newContentLibrary()
	for _, p := range packs {
		if err := lib.loadPack(p); err != nil {
			return nil, err
		}
	}
	lib.validate()

	if opts.Strict && lib.HasErrors() {
		first := ""
		for _, is := range lib.issues {
			if is.Severity == SevError {
				first = is.String()
				break
			}
		}
		return lib, fmt.Errorf("content has %d error(s); first: %s", lib.errorCount(), first)
	}
	return lib, nil
}

func (l *ContentLibrary) loadPack(p PackSource) error {
	name := p.Name

	if data, err := fs.ReadFile(p.FS, "pack.json"); err == nil {
		var meta struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&meta); err != nil {
			l.addIssue(SevError, name, "pack.json", "", "", "invalid pack.json: %v", err)
		} else if meta.ID != "" {
			name = meta.ID
		}
	}

	seen := map[string]srcInfo{} // duplicate detection inside this pack
	err := fs.WalkDir(p.FS, ".", func(fp string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(fp), ".json") || fp == "pack.json" {
			return nil
		}
		l.loadFile(p.FS, name, fp, seen)
		return nil
	})
	if err != nil {
		return fmt.Errorf("reading pack %q: %w", name, err)
	}
	return nil
}

func (l *ContentLibrary) loadFile(fsys fs.FS, pack, file string, seen map[string]srcInfo) {
	data, err := fs.ReadFile(fsys, file)
	if err != nil {
		l.addIssue(SevError, pack, file, "", "", "cannot read file: %v", err)
		return
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // editors sometimes add a BOM

	top := file
	if i := strings.Index(file, "/"); i >= 0 {
		top = file[:i]
	}
	switch top {
	case "features":
		loadEntities(l, pack, file, data, seen, "feature", func(x Feature) string { return x.ID }, l.features)
	case "items":
		loadEntities(l, pack, file, data, seen, "item", func(x Item) string { return x.ID }, l.items)
	case "classes":
		loadEntities(l, pack, file, data, seen, "class", func(x Class) string { return x.ID }, l.classes)
	case "spells":
		loadEntities(l, pack, file, data, seen, "spell", func(x Spell) string { return x.ID }, l.spells)
	case "spell_lists":
		loadEntities(l, pack, file, data, seen, "spell_list", func(x SpellList) string { return x.ID }, l.lists)
	case "skills.json":
		l.loadSkills(pack, file, data, seen)
	default:
		l.addIssue(SevWarning, pack, file, "", "", "ignored: not in features/, items/, classes/, spells/, spell_lists/ and not skills.json")
	}
}

// decodeEntities accepts one JSON object or an array of them, rejecting unknown fields.
// On failure it also returns the byte offset to report a line number for.
func decodeEntities[T any](data []byte) ([]T, int64, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, 0, errors.New("file is empty")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var out []T
	if bytes.HasPrefix(bytes.TrimLeft(data, " \t\r\n"), []byte("[")) {
		if err := dec.Decode(&out); err != nil {
			return nil, dec.InputOffset(), err
		}
	} else {
		var one T
		if err := dec.Decode(&one); err != nil {
			return nil, dec.InputOffset(), err
		}
		out = []T{one}
	}

	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, dec.InputOffset(), errors.New("unexpected content after the top-level value")
	}
	return out, 0, nil
}

func describeJSONError(data []byte, inputOffset int64, err error) string {
	off := inputOffset
	var se *json.SyntaxError
	var te *json.UnmarshalTypeError
	switch {
	case errors.As(err, &se):
		off = se.Offset
	case errors.As(err, &te):
		off = te.Offset
	}
	if off > int64(len(data)) {
		off = int64(len(data))
	}
	if off < 0 {
		off = 0
	}
	line := 1 + bytes.Count(data[:off], []byte("\n"))
	return fmt.Sprintf("%v (near line %d)", err, line)
}

func loadEntities[T any](l *ContentLibrary, pack, file string, data []byte, seen map[string]srcInfo,
	kind string, idOf func(T) string, dst map[string]T) {

	items, off, err := decodeEntities[T](data)
	if err != nil {
		l.addIssue(SevError, pack, file, kind, "", "%s", describeJSONError(data, off, err))
		return
	}
	for i, it := range items {
		id := idOf(it)
		if id == "" {
			l.addIssue(SevError, pack, file, kind, "", "entry %d has no id", i+1)
			continue
		}
		key := kind + ":" + id
		if prev, dup := seen[key]; dup {
			l.addIssue(SevError, pack, file, kind, id, "duplicate id (already defined in %s)", prev.file)
			continue
		}
		seen[key] = srcInfo{pack, file}
		if prev, ok := l.src[key]; ok {
			l.addIssue(SevWarning, pack, file, kind, id, "overrides the %s from pack %q (%s)", kind, prev.pack, prev.file)
		}
		dst[id] = it
		l.src[key] = srcInfo{pack, file}
	}
}

func (l *ContentLibrary) loadSkills(pack, file string, data []byte, seen map[string]srcInfo) {
	items, off, err := decodeEntities[SkillDef](data)
	if err != nil {
		l.addIssue(SevError, pack, file, "skill", "", "%s", describeJSONError(data, off, err))
		return
	}
	for i, sd := range items {
		if sd.ID == "" {
			l.addIssue(SevError, pack, file, "skill", "", "entry %d has no id", i+1)
			continue
		}
		key := "skill:" + sd.ID
		if prev, dup := seen[key]; dup {
			l.addIssue(SevError, pack, file, "skill", sd.ID, "duplicate id (already defined in %s)", prev.file)
			continue
		}
		seen[key] = srcInfo{pack, file}
		if prev, ok := l.src[key]; ok {
			l.addIssue(SevWarning, pack, file, "skill", sd.ID, "overrides the skill from pack %q (%s)", prev.pack, prev.file)
		}
		if idx, ok := l.skillIdx[sd.ID]; ok {
			l.skills[idx] = sd // keep the original position
		} else {
			l.skillIdx[sd.ID] = len(l.skills)
			l.skills = append(l.skills, sd)
		}
		l.src[key] = srcInfo{pack, file}
	}
}

// ---------------------------------------------------------------------------
// Validation (runs once, on the merged library)
// ---------------------------------------------------------------------------

var (
	modKinds = map[ModKind]bool{
		ModAdd: true, ModSet: true, ModMin: true, ModMultiply: true, ModAdvantage: true, ModDisadvantage: true, ModNote: true,
	}
	actionTypes = map[ActionType]bool{
		ActionAction: true, ActionBonusAction: true, ActionReaction: true, ActionFree: true, ActionOther: true,
	}
	origins = map[Origin]bool{
		OriginClass: true, OriginRace: true, OriginBackground: true, OriginFeat: true, OriginItem: true, OriginOther: true,
	}
	profLevels = map[ProficiencyLevel]bool{ProfNone: true, ProfHalf: true, ProfTrained: true, ProfExpert: true}
	recharges  = map[RechargeType]bool{
		RechargeShortRest: true, RechargeLongRest: true, RechargeDawn: true, RechargeNone: true,
	}
	itemKinds   = map[ItemKind]bool{ItemArmour: true, ItemShield: true, ItemWeapon: true, ItemOther: true}
	filterTypes = map[string]bool{"skill": true, "tool": true, "language": true, "feat": true, "feature_tag": true}
	prepModes   = map[string]bool{"": true, PrepKnown: true, PrepPrepared: true, PrepBook: true}
)

type validator struct {
	l      *ContentLibrary
	skills map[string]bool
}

func (v *validator) err(kind, id, format string, args ...any) {
	v.l.issueAt(SevError, kind, id, format, args...)
}

func (v *validator) warn(kind, id, format string, args ...any) {
	v.l.issueAt(SevWarning, kind, id, format, args...)
}

func (l *ContentLibrary) validate() {
	v := &validator{l: l, skills: map[string]bool{}}
	for _, s := range l.skills {
		v.skills[s.ID] = true
	}

	for _, s := range l.skills {
		if !isAbility(string(s.DefaultAbility)) {
			v.err("skill", s.ID, "default_ability %q is not one of str, dex, con, int, wis, cha", s.DefaultAbility)
		}
	}
	for _, id := range sortedKeys(l.features) {
		v.feature(l.features[id])
	}
	for _, id := range sortedKeys(l.items) {
		v.item(l.items[id])
	}
	for _, id := range sortedKeys(l.classes) {
		v.class(l.classes[id])
	}
	for _, id := range sortedKeys(l.spells) {
		v.spell(l.spells[id])
	}
	for _, id := range sortedKeys(l.lists) {
		v.spellList(l.lists[id])
	}
}

func (v *validator) featureRef(kind, id, where, ref string) {
	if _, ok := v.l.features[ref]; !ok {
		v.err(kind, id, "%s: unknown feature %q", where, ref)
	}
}

func (v *validator) spellRef(kind, id, where, ref string) {
	if _, ok := v.l.spells[ref]; !ok {
		v.err(kind, id, "%s: unknown spell %q", where, ref)
	}
}

func (v *validator) modifiers(kind, id, where string, mods []Modifier) {
	for i, m := range mods {
		at := fmt.Sprintf("%s modifier %d", where, i+1)
		if !validTarget(m.Target, v.skills) {
			v.err(kind, id, "%s: unknown target %q", at, m.Target)
		}
		if !modKinds[m.Kind] {
			v.err(kind, id, "%s: unknown kind %q (add, set, min, multiply, advantage, disadvantage, note)", at, m.Kind)
		}
		if m.Kind == ModMultiply && m.Value <= 0 {
			v.err(kind, id, "%s: multiply needs a value greater than 0", at)
		}
		if m.Dice != "" {
			if m.Kind != ModAdd {
				v.err(kind, id, "%s: dice only work with kind add", at)
			} else if !diceRe.MatchString(m.Dice) {
				v.err(kind, id, "%s: dice %q must look like \"1d6\"", at, m.Dice)
			}
		}
	}
}

func (v *validator) attack(kind, id, at string, d AttackDetails) {
	if d.Ability != "" && !isAbility(string(d.Ability)) {
		v.err(kind, id, "%s: ability %q is not an ability", at, d.Ability)
	}
	if d.DamageDice != "" && !diceRe.MatchString(d.DamageDice) {
		v.err(kind, id, "%s: damage_dice %q must look like \"1d8\"", at, d.DamageDice)
	}
}

func (v *validator) actions(kind, id, where string, acts []Action) {
	for i, a := range acts {
		at := fmt.Sprintf("%s action %d", where, i+1)
		if a.Name == "" {
			v.warn(kind, id, "%s has no name", at)
		}
		if !actionTypes[a.Type] {
			v.err(kind, id, "%s: unknown type %q (action, bonus_action, reaction, free, other)", at, a.Type)
		}
		if a.IsAttack && a.Attack == nil {
			v.err(kind, id, "%s is marked as an attack but has no attack details", at)
		}
		if a.Attack != nil {
			v.attack(kind, id, at, *a.Attack)
		}
	}
}

func (v *validator) resource(kind, id, where string, r *Resource) {
	if r == nil {
		return
	}
	if r.Max < 0 {
		v.err(kind, id, "%s resource: max cannot be negative", where)
	}
	if r.MaxAbility != "" && !isAbility(string(r.MaxAbility)) {
		v.err(kind, id, "%s resource: max_ability %q is not an ability", where, r.MaxAbility)
	}
	if !recharges[r.Recharge] {
		v.err(kind, id, "%s resource: recharge %q must be short_rest, long_rest, dawn or none", where, r.Recharge)
	}
}

func (v *validator) grants(kind, id, where string, g Grants) {
	for _, sid := range sortedKeys(g.Skills) {
		if !v.skills[sid] {
			v.err(kind, id, "%s: unknown skill %q", where, sid)
		}
		if !profLevels[g.Skills[sid]] {
			v.err(kind, id, "%s: skill %q has unknown proficiency level %q", where, sid, g.Skills[sid])
		}
	}
	for _, tid := range sortedKeys(g.Tools) {
		if !profLevels[g.Tools[tid]] {
			v.err(kind, id, "%s: tool %q has unknown proficiency level %q", where, tid, g.Tools[tid])
		}
	}
	for _, a := range g.Saves {
		if !isAbility(string(a)) {
			v.err(kind, id, "%s: save %q is not an ability", where, a)
		}
	}
	for _, fid := range g.FeatureIDs {
		v.featureRef(kind, id, where, fid)
	}
}

func (v *validator) termSource(src string) bool {
	switch {
	case src == "pb" || src == "level":
		return true
	case strings.HasPrefix(src, "mod."):
		return isAbility(strings.TrimPrefix(src, "mod."))
	case strings.HasPrefix(src, "class_level."):
		_, ok := v.l.classes[strings.TrimPrefix(src, "class_level.")]
		return ok
	}
	return false
}

func (v *validator) formula(kind, id, where string, f Formula) {
	for i, t := range f.Terms {
		if !v.termSource(t.Source) {
			v.err(kind, id, "%s term %d: unknown source %q (mod.<ability>, pb, level, class_level.<class id>)", where, i+1, t.Source)
		}
		if t.Div < 0 {
			v.err(kind, id, "%s term %d: div cannot be negative", where, i+1)
		}
	}
}

func (v *validator) acFormula(kind, id, where string, f ACFormula) {
	if f.ID == "" {
		v.err(kind, id, "%s: ac formula needs an id", where)
	}
	v.formula(kind, id, where+" ac formula "+f.ID, f.Formula)
}

func (v *validator) checkableTarget(t string) bool {
	switch {
	case t == "level", t == TargetProficiencyBonus, t == TargetAC, t == TargetMaxHP, t == TargetInitiative:
		return true
	case strings.HasPrefix(t, "ability."):
		return isAbility(strings.TrimPrefix(t, "ability."))
	case strings.HasPrefix(t, "skill."):
		return v.skills[strings.TrimPrefix(t, "skill.")]
	}
	return false
}

func (v *validator) prerequisites(kind, id, where string, ps []Prerequisite) {
	for i, p := range ps {
		if p.Description == "" {
			v.warn(kind, id, "%s prerequisite %d has no description", where, i+1)
		}
		if p.Target != "" && !v.checkableTarget(p.Target) {
			v.warn(kind, id, "%s prerequisite %q: target %q can't be checked automatically", where, p.Description, p.Target)
		}
	}
}

func (v *validator) spellGrant(kind, id, where string, g *SpellGrant) {
	v.checkSpellGrant(kind, id, where, g, false)
}

// checkSpellGrant validates a spell grant. A tier's grant may leave the ability empty when it inherits
// one from the feature's own spells block.
func (v *validator) checkSpellGrant(kind, id, where string, g *SpellGrant, abilityOptional bool) {
	if g == nil {
		return
	}
	if !(abilityOptional && g.Ability == "") && !isAbility(string(g.Ability)) {
		v.err(kind, id, "%s spells: ability %q is not an ability", where, g.Ability)
	}
	for _, sid := range g.SpellIDs {
		v.spellRef(kind, id, where+" spells", sid)
	}
	v.resource(kind, id, where+" spells", g.Resource)
}

func (v *validator) choices(kind, id, where string, chs []Choice) {
	seen := map[string]bool{}
	for i, ch := range chs {
		at := fmt.Sprintf("%s choice %q", where, ch.ID)
		if ch.ID == "" {
			at = fmt.Sprintf("%s choice %d", where, i+1)
			v.err(kind, id, "%s has no id", at)
		} else if seen[ch.ID] {
			v.err(kind, id, "%s: duplicate choice id", at)
		}
		seen[ch.ID] = true

		switch ch.Kind {
		case ChoicePick:
			if ch.Count < 1 {
				v.err(kind, id, "%s: a pick choice needs count of at least 1", at)
			}
		case ChoiceAllocate:
			if ch.Points < 1 {
				v.err(kind, id, "%s: an allocate choice needs points of at least 1", at)
			}
			if ch.Filter != nil {
				v.err(kind, id, "%s: allocate choices need explicit options, not a filter", at)
			}
		default:
			v.err(kind, id, "%s: unknown kind %q (pick or allocate)", at, ch.Kind)
		}

		hasOptions, hasFilter := len(ch.Options) > 0, ch.Filter != nil
		if hasOptions == hasFilter {
			v.err(kind, id, "%s: needs exactly one of options or filter", at)
		}
		if ch.GrantLevel != "" && !profLevels[ch.GrantLevel] {
			v.err(kind, id, "%s: unknown grant_level %q", at, ch.GrantLevel)
		}

		optSeen := map[string]bool{}
		for j, opt := range ch.Options {
			oat := fmt.Sprintf("%s option %q", at, opt.ID)
			if opt.ID == "" {
				oat = fmt.Sprintf("%s option %d", at, j+1)
				v.err(kind, id, "%s has no id", oat)
			} else if optSeen[opt.ID] {
				v.err(kind, id, "%s: duplicate option id", oat)
			}
			optSeen[opt.ID] = true
			v.grants(kind, id, oat, opt.Grants)
			v.modifiers(kind, id, oat, opt.Modifiers)
		}

		if ch.Filter != nil {
			if !filterTypes[ch.Filter.Type] {
				v.err(kind, id, "%s: unknown filter type %q (skill, tool, language, feat, feature_tag)", at, ch.Filter.Type)
			}
			if ch.Filter.Type == "feature_tag" && ch.Filter.Tag == "" && len(ch.Filter.Allowed) == 0 {
				v.err(kind, id, "%s: a feature_tag filter needs a tag or an allowed list", at)
			}
			if ch.Filter.Type == "skill" {
				for _, sid := range ch.Filter.Allowed {
					if !v.skills[sid] {
						v.err(kind, id, "%s: allowed list has unknown skill %q", at, sid)
					}
				}
			}
		}
	}
}

func (v *validator) feature(f Feature) {
	const k = "feature"
	if f.Name == "" {
		v.warn(k, f.ID, "has no name")
	}
	if f.Origin != "" && !origins[f.Origin] {
		v.err(k, f.ID, "unknown origin %q (class, race, background, feat, item, other)", f.Origin)
	}
	if f.Origin == OriginRace && touchesAbilityScores(f) {
		v.warn(k, f.ID, "ability score increases come from backgrounds, so a race shouldn't change ability scores")
	}
	for _, tag := range f.Tags {
		if tag == "background" && f.Origin != OriginBackground {
			v.warn(k, f.ID, "is tagged \"background\" but its origin is %q, not \"background\"", f.Origin)
		}
		if tag == "race" && f.Origin != OriginRace {
			v.warn(k, f.ID, "is tagged \"race\" but its origin is %q, not \"race\"", f.Origin)
		}
	}
	if strings.Contains(f.ID, "@") {
		v.err(k, f.ID, "feature ids can't contain \"@\" (it marks which choice a repeatable feature came from)")
	}
	if f.ScaleBy != "" && f.ScaleBy != "character" {
		v.err(k, f.ID, "scale_by %q must be empty or \"character\"", f.ScaleBy)
	}

	v.modifiers(k, f.ID, "feature", f.Modifiers)
	v.actions(k, f.ID, "feature", f.Actions)
	v.resource(k, f.ID, "feature", f.Resource)
	v.grants(k, f.ID, "feature", f.Grants)
	v.choices(k, f.ID, "feature", f.Choices)
	v.prerequisites(k, f.ID, "feature", f.Prerequisites)
	v.spellGrant(k, f.ID, "feature", f.Spells)
	for _, af := range f.ACFormulas {
		v.acFormula(k, f.ID, "feature", af)
	}

	seenTier := map[int]bool{}
	for i, t := range f.Tiers {
		where := fmt.Sprintf("tier %d (min level %d)", i+1, t.MinLevel)
		if t.MinLevel < 1 {
			v.err(k, f.ID, "%s: min_level must be at least 1", where)
		}
		if seenTier[t.MinLevel] {
			v.warn(k, f.ID, "%s: another tier has the same min_level", where)
		}
		seenTier[t.MinLevel] = true
		v.modifiers(k, f.ID, where, t.Modifiers)
		v.actions(k, f.ID, where, t.Actions)
		v.grants(k, f.ID, where, t.Grants)
		v.resource(k, f.ID, where, t.Resource)
		inherits := f.Spells != nil && f.Spells.Ability != ""
		v.checkSpellGrant(k, f.ID, where+" tier", t.Spells, inherits)
	}
}

func (v *validator) item(it Item) {
	const k = "item"
	if it.Name == "" {
		v.warn(k, it.ID, "has no name")
	}
	if !itemKinds[it.Kind] {
		v.err(k, it.ID, "unknown kind %q (armour, shield, weapon, other)", it.Kind)
	}
	if it.Weight < 0 {
		v.err(k, it.ID, "weight cannot be negative")
	}
	if it.Cost != nil {
		if it.Cost.Amount < 0 {
			v.err(k, it.ID, "cost cannot be negative")
		}
		if it.Cost.Currency == "" {
			v.warn(k, it.ID, "cost has no currency")
		}
	}
	if it.Container != nil && it.Container.Capacity < 0 {
		v.err(k, it.ID, "container capacity cannot be negative")
	}
	if it.Kind == ItemArmour && it.ACFormula == nil {
		v.warn(k, it.ID, "armour has no ac_formula, so it grants no AC")
	}
	if it.ACFormula != nil {
		if it.Kind != ItemArmour {
			v.warn(k, it.ID, "has an ac_formula but is not armour, so it will be ignored")
		}
		v.acFormula(k, it.ID, "item", *it.ACFormula)
	}
	if it.Kind == ItemWeapon && it.Weapon == nil {
		v.warn(k, it.ID, "weapon has no weapon stats, so it produces no attack")
	}
	if it.Weapon != nil {
		if it.Weapon.Category == "" {
			v.warn(k, it.ID, "weapon has no category, so proficiency can only match its id")
		}
		v.attack(k, it.ID, "weapon", it.Weapon.Attack)
		if it.Weapon.VersatileDice != "" && !diceRe.MatchString(it.Weapon.VersatileDice) {
			v.err(k, it.ID, "weapon: versatile_dice %q must look like \"1d10\"", it.Weapon.VersatileDice)
		}
	}
	v.modifiers(k, it.ID, "item", it.Modifiers)
	v.actions(k, it.ID, "item", it.Actions)
	v.spellGrant(k, it.ID, "item", it.Spells)
}

func (v *validator) class(c Class) {
	const k = "class"
	if c.Name == "" {
		v.warn(k, c.ID, "has no name")
	}
	if c.HitDie <= 0 {
		v.err(k, c.ID, "hit_die must be greater than 0")
	}
	v.grants(k, c.ID, "starting grants", c.StartingGrants)
	v.grants(k, c.ID, "multiclass grants", c.MulticlassGrants)
	v.choices(k, c.ID, "starting", c.StartingChoices)
	v.prerequisites(k, c.ID, "multiclass", c.MulticlassPrereq)

	for i, row := range c.Levels {
		for _, fid := range row {
			v.featureRef(k, c.ID, fmt.Sprintf("level %d", i+1), fid)
		}
	}

	if c.SubclassLevel > 0 && len(c.Subclasses) == 0 {
		v.warn(k, c.ID, "subclass_level is set but there are no subclasses")
	}
	subSeen := map[string]bool{}
	for _, sub := range c.Subclasses {
		if sub.ID == "" {
			v.err(k, c.ID, "a subclass has no id")
		} else if subSeen[sub.ID] {
			v.err(k, c.ID, "duplicate subclass id %q", sub.ID)
		}
		subSeen[sub.ID] = true
		for _, sid := range sub.ExpandedSpells {
			v.spellRef(k, c.ID, fmt.Sprintf("subclass %q expanded spells", sub.ID), sid)
		}
		for i, row := range sub.Levels {
			for _, fid := range row {
				v.featureRef(k, c.ID, fmt.Sprintf("subclass %q level %d", sub.ID, i+1), fid)
			}
		}
	}

	if sc := c.Spellcasting; sc != nil {
		if !isAbility(string(sc.Ability)) {
			v.err(k, c.ID, "spellcasting: ability %q is not an ability", sc.Ability)
		}
		if sc.Divisor < 0 {
			v.err(k, c.ID, "spellcasting: divisor cannot be negative")
		}
		if sc.Divisor == 0 && !sc.Pact {
			v.warn(k, c.ID, "spellcasting: divisor 0 and not pact casting, so this class gets no slots")
		}
		if !prepModes[sc.Prep] {
			v.err(k, c.ID, "spellcasting: prep %q must be known, prepared or book", sc.Prep)
		}
		if sc.SpellListID != "" {
			if _, ok := v.l.lists[sc.SpellListID]; !ok {
				v.err(k, c.ID, "spellcasting: unknown spell list %q", sc.SpellListID)
			}
		}
		if sc.PreparedFormula != nil {
			v.formula(k, c.ID, "spellcasting prepared formula", *sc.PreparedFormula)
		}
	}
}

func (v *validator) spell(s Spell) {
	const k = "spell"
	if s.Name == "" {
		v.warn(k, s.ID, "has no name")
	}
	if s.Level < 0 || s.Level > 9 {
		v.err(k, s.ID, "level %d must be between 0 (cantrip) and 9", s.Level)
	}
	if s.Save != "" && !isAbility(string(s.Save)) {
		v.err(k, s.ID, "save %q is not an ability", s.Save)
	}
	if s.ACFormula != nil {
		v.acFormula(k, s.ID, "spell", *s.ACFormula)
	}
	for i, e := range s.Effects {
		at := fmt.Sprintf("effect %d", i+1)
		if e.Kind != "damage" && e.Kind != "healing" {
			v.err(k, s.ID, "%s: kind %q must be damage or healing", at, e.Kind)
		}
		if !diceRe.MatchString(e.Dice) {
			v.err(k, s.ID, "%s: dice %q must look like \"8d6\" (put flat bonuses in the note)", at, e.Dice)
		}
		if e.UpcastDice != "" && !diceRe.MatchString(e.UpcastDice) {
			v.err(k, s.ID, "%s: upcast_dice %q must look like \"1d6\"", at, e.UpcastDice)
		}
		for _, t := range e.DiceTiers {
			if !diceRe.MatchString(t.Dice) {
				v.err(k, s.ID, "%s: tier dice %q must look like \"2d10\"", at, t.Dice)
			}
		}
	}
}

func (v *validator) spellList(sl SpellList) {
	for _, sid := range sl.SpellIDs {
		v.spellRef("spell_list", sl.ID, "list", sid)
	}
}

func modsTouchAbilities(mods []Modifier) bool {
	for _, m := range mods {
		if strings.HasPrefix(m.Target, "ability.") {
			return true
		}
	}
	return false
}

// touchesAbilityScores reports whether a feature, one of its tiers, or one of its choice options
// modifies an ability score.
func touchesAbilityScores(f Feature) bool {
	if modsTouchAbilities(f.Modifiers) {
		return true
	}
	for _, t := range f.Tiers {
		if modsTouchAbilities(t.Modifiers) {
			return true
		}
	}
	for _, ch := range f.Choices {
		for _, o := range ch.Options {
			if modsTouchAbilities(o.Modifiers) {
				return true
			}
		}
	}
	return false
}
