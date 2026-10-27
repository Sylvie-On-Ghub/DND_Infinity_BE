// Package character is a planning draft, not final code.
// Three layers:
//  1. Library  - rules content as data (features, items, races, classes...), referenced by ID
//  2. Character - stored state: choices + things that change in play
//  3. Sheet    - computed view, produced by Compute(); never stored
package character

import "time"

// ---------------------------------------------------------------------------
// Enums (typed strings serialise cleanly to JSON and are readable in the DB)
// ---------------------------------------------------------------------------

type Ability string

const (
	Str Ability = "str"
	Dex Ability = "dex"
	Con Ability = "con"
	Int Ability = "int"
	Wis Ability = "wis"
	Cha Ability = "cha"
)

type ProficiencyLevel string

const (
	ProfNone    ProficiencyLevel = "none"
	ProfHalf    ProficiencyLevel = "half" // kept for homebrew; Jack of All Trades could instead be a modifier
	ProfTrained ProficiencyLevel = "trained"
	ProfExpert  ProficiencyLevel = "expert"
)

type ActionType string

const (
	ActionAction      ActionType = "action"
	ActionBonusAction ActionType = "bonus_action"
	ActionReaction    ActionType = "reaction"
	ActionFree        ActionType = "free"
	ActionOther       ActionType = "other" // "special", longer casting times, etc.
)

type Origin string

const (
	OriginClass      Origin = "class"
	OriginRace       Origin = "race"
	OriginBackground Origin = "background"
	OriginFeat       Origin = "feat"
	OriginItem       Origin = "item"
	OriginOther      Origin = "other"
)

type ModKind string

const (
	ModAdd          ModKind = "add"          // +Value to the target
	ModSet          ModKind = "set"          // target becomes Value (e.g. an ability score of 19)
	ModMin          ModKind = "min"          // target is at least Value
	ModAdvantage    ModKind = "advantage"    // flag; Value unused
	ModDisadvantage ModKind = "disadvantage" // flag; Value unused
	ModNote         ModKind = "note"         // reminder text only; shown on sheet, no maths
	ModMultiply     ModKind = "multiply"     // multiplies the target after adds and before min (Powerful Build doubles carry capacity)
)

type Condition string // blinded, charmed, deafened, frightened, grappled, ... (exhaustion is a level, see Character)

type DamageType string // fire, cold, slashing, ...

type SpeedType string

const (
	SpeedWalk   SpeedType = "walk"
	SpeedFly    SpeedType = "fly"
	SpeedSwim   SpeedType = "swim"
	SpeedClimb  SpeedType = "climb"
	SpeedBurrow SpeedType = "burrow"
)

type RechargeType string

const (
	RechargeShortRest RechargeType = "short_rest"
	RechargeLongRest  RechargeType = "long_rest"
	RechargeDawn      RechargeType = "dawn"
	RechargeNone      RechargeType = "none"
)

// ---------------------------------------------------------------------------
// Modifier targets
// Fixed targets are constants; parameterised ones are built with helpers so a
// typo can't silently target nothing. Validate Target against this set on write.
// ---------------------------------------------------------------------------

const (
	TargetAC               = "ac"
	TargetInitiative       = "initiative"
	TargetMaxHP            = "hp.max"
	TargetProficiencyBonus = "proficiency_bonus"
	TargetSpellSaveDC      = "spell.save_dc"
	TargetSpellAttack      = "spell.attack"
	TargetAttackAll        = "attack.all" // attack rolls (weapon + spell)
	TargetAttackMelee      = "attack.melee"
	TargetAttackRanged     = "attack.ranged"
	TargetCheckAll         = "check.all" // every ability/skill check
	TargetSaveAll          = "save.all"  // every saving throw
	TargetDeathSave        = "save.death"
	TargetCarryCapacity    = "carry_capacity"
	TargetDamageAll        = "damage.all" // flat and dice bonuses to weapon damage
	TargetDamageMelee      = "damage.melee"
	TargetDamageRanged     = "damage.ranged"
	TargetDamageSpell      = "damage.spell"
)

func AbilityTarget(a Ability) string      { return "ability." + string(a) } // the score itself
func CheckTarget(a Ability) string        { return "check." + string(a) }   // raw ability checks
func SaveTarget(a Ability) string         { return "save." + string(a) }
func SkillTarget(skillID string) string   { return "skill." + skillID }
func PassiveTarget(skillID string) string { return "passive." + skillID }
func SpeedTarget(s SpeedType) string      { return "speed." + string(s) }
func SenseTarget(name string) string      { return "sense." + name }       // adds to range
func ResistTarget(d DamageType) string    { return "resist." + string(d) } // ModSet/flag style
func ImmuneTarget(d DamageType) string    { return "immune." + string(d) }
func VulnTarget(d DamageType) string      { return "vuln." + string(d) }

// Modifier is the single general-purpose modifier. Everything that tweaks a
// derived number or reminds the player of a situational rule is one of these.
type Modifier struct {
	Target string  `json:"target"`
	Kind   ModKind `json:"kind"`
	Value  int     `json:"value,omitempty"`
	// Dice makes this a "dice rider" (Sneak Attack 3d6, Hunter's Mark 1d6, Bless 1d4). Only meaningful with
	// Kind ModAdd. Riders are never summed into flat numbers; on attack/damage targets they are listed on the
	// matching attack entries, elsewhere they surface in Sheet.Notes.
	Dice      string `json:"dice,omitempty"`
	Condition string `json:"condition,omitempty"` // "vs. poison", "while raging"; non-empty => shown as situational
	Note      string `json:"note,omitempty"`
	SourceID  string `json:"source_id"` // LIBRARY id of the feature/item/spell/condition (e.g. "bless", "ring_of_protection"), not an instance id

	// Stacking: modifiers with the same (Target, Kind, SourceID) do not stack.
	// Two Rings of Protection or two casts of Bless count once; the best Value wins
	// (highest for add/min, highest for set). Empty SourceID (ad hoc modifiers) never dedupes.
	// AllowStack opts a specific modifier out, for homebrew or the rare rules exception.
	AllowStack bool `json:"allow_stack,omitempty"`
}

// ---------------------------------------------------------------------------
// Library layer (data, not code) - homebrew means adding rows here
// ---------------------------------------------------------------------------

type AttackDetails struct {
	Ability     Ability    `json:"ability"`     // used for to-hit/damage
	DamageDice  string     `json:"damage_dice"` // "1d8"
	DamageType  DamageType `json:"damage_type"`
	RangeNormal int        `json:"range_normal"`
	RangeLong   int        `json:"range_long,omitempty"`
	Properties  []string   `json:"properties,omitempty"`  // finesse, versatile, ...
	Ranged      bool       `json:"ranged,omitempty"`      // uses ranged targets and Dex by default
	FlatDamage  int        `json:"flat_damage,omitempty"` // fixed damage with no dice (unarmed strike deals 1)
}

type Action struct {
	Name        string         `json:"name"`
	Type        ActionType     `json:"type"`
	IsAttack    bool           `json:"is_attack"` // true => attacks section, false => grouped by origin
	Attack      *AttackDetails `json:"attack,omitempty"`
	Description string         `json:"description,omitempty"`
	Duration    string         `json:"duration,omitempty"` // instant / 1 minute / concentration - separate from action type
	// Filled in on the sheet, never in content: the (instance) id of the feature that granted the action,
	// and the id of that feature's limited-use resource in Sheet.Resources, if it has one.
	FeatureID  string `json:"feature_id,omitempty"`
	ResourceID string `json:"resource_id,omitempty"`
}

type Resource struct {
	Name string `json:"name"`
	Max  int    `json:"max"` // with MaxAbility set, the minimum
	// MaxAbility scales uses with an ability modifier ("uses equal to your Charisma modifier").
	MaxAbility Ability      `json:"max_ability,omitempty"`
	Recharge   RechargeType `json:"recharge"`
}

// Feature covers class features, racial traits, feats and background features.
type Feature struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Origin      Origin     `json:"origin"`
	SourceID    string     `json:"source_id,omitempty"` // which class/race/background it belongs to
	LevelGained int        `json:"level_gained,omitempty"`
	Description string     `json:"description"`
	Actions     []Action   `json:"actions,omitempty"`
	Modifiers   []Modifier `json:"modifiers,omitempty"`
	Resource    *Resource  `json:"resource,omitempty"`

	Grants        Grants         `json:"grants,omitempty"` // unconditional proficiencies etc.
	Choices       []Choice       `json:"choices,omitempty"`
	Prerequisites []Prerequisite `json:"prerequisites,omitempty"` // checked permissively: failures are warnings
	ACFormulas    []ACFormula    `json:"ac_formulas,omitempty"`   // extra ways to calculate AC (Unarmored Defense, natural armour...)
	Tiers         []Tier         `json:"tiers,omitempty"`         // scaling: the highest tier at or below the level replaces lower ones
	ScaleBy       string         `json:"scale_by,omitempty"`      // "" = class level when granted by a class, else character level; "character" forces character level
	Tags          []string       `json:"tags,omitempty"`          // labels a feature so choices can pool by tag (e.g. every eldritch invocation)
	Repeatable    bool           `json:"repeatable,omitempty"`    // can be taken more than once (Ability Score Improvement as a feat); each pick is its own instance with its own choices
	Spells        *SpellGrant    `json:"spells,omitempty"`        // spells this feature lets the character cast (innate casting)
}

// Grants are non-numeric things a feature (or a picked option) gives the character.
// Numeric/situational effects stay as Modifiers; senses and resistances are Modifiers too.
type Grants struct {
	Skills      map[string]ProficiencyLevel `json:"skills,omitempty"`
	Tools       map[string]ProficiencyLevel `json:"tools,omitempty"`
	Languages   []string                    `json:"languages,omitempty"`
	ArmourProfs []string                    `json:"armour_profs,omitempty"`
	WeaponProfs []string                    `json:"weapon_profs,omitempty"`
	Saves       []Ability                   `json:"saves,omitempty"`       // saving throw proficiencies
	FeatureIDs  []string                    `json:"feature_ids,omitempty"` // nested features; this is how a picked feat's own choices resolve
}

type Prerequisite struct {
	Description string `json:"description"`      // always shown
	Target      string `json:"target,omitempty"` // sheet value to check, e.g. "ability.str"; empty => can't be auto-checked
	Min         int    `json:"min,omitempty"`
}

type ChoiceKind string

const (
	ChoicePick     ChoiceKind = "pick"     // choose Count options
	ChoiceAllocate ChoiceKind = "allocate" // spread Points across options (ASI-style)
)

// Option is one explicit pick, with whatever it grants.
type Option struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Grants    Grants     `json:"grants,omitempty"`
	Modifiers []Modifier `json:"modifiers,omitempty"`
}

// PoolFilter describes a pool by rule instead of listing every option, e.g.
// {Type:"skill", Allowed:["athletics","stealth"]} or {Type:"feat", Tag:"fighting_style"}.
type PoolFilter struct {
	Type    string   `json:"type"` // skill | tool | language | feat | feature_tag
	Tag     string   `json:"tag,omitempty"`
	Allowed []string `json:"allowed,omitempty"` // restrict to this subset (e.g. a class skill list)
}

type Choice struct {
	ID     string     `json:"id"`
	Prompt string     `json:"prompt"`
	Group  string     `json:"group,omitempty"` // choices in one group are alternatives (ASI or feat): any one filled satisfies the rest
	Kind   ChoiceKind `json:"kind"`
	Count  int        `json:"count,omitempty"` // pick
	// Pool: explicit Options OR a Filter.
	Options []Option    `json:"options,omitempty"`
	Filter  *PoolFilter `json:"filter,omitempty"`
	// For filter pools, what each pick grants (e.g. ProfTrained for skills, ProfExpert for Expertise).
	GrantLevel ProficiencyLevel `json:"grant_level,omitempty"`
	// Allocate only
	Points       int `json:"points,omitempty"`
	MaxPerOption int `json:"max_per_option,omitempty"`
}

// Selection is what the character actually picked. Stored; flat; keyed by feature + choice.
type Selection struct {
	FeatureID string         `json:"feature_id"`
	ChoiceID  string         `json:"choice_id"`
	Picked    []string       `json:"picked,omitempty"`    // pick
	Allocated map[string]int `json:"allocated,omitempty"` // allocate
}

// ---- Validation is permissive: problems come back as Warnings, never as rejections. ----

type WarningCode string

const (
	WarnNotInPool          WarningCode = "not_in_pool"
	WarnOverCount          WarningCode = "over_count"
	WarnPrereqUnmet        WarningCode = "prereq_unmet"
	WarnPrereqUnverifiable WarningCode = "prereq_unverifiable"
	WarnUnknownID          WarningCode = "unknown_id"     // feature/option no longer in the library
	WarnUnknownTarget      WarningCode = "unknown_target" // modifier target not in the valid set
	WarnOverCap            WarningCode = "over_cap"       // e.g. inspiration above Ruleset.MaxInspiration
)

type Warning struct {
	Key       string      `json:"key"` // deterministic; see WarningKey
	Code      WarningCode `json:"code"`
	Message   string      `json:"message"`
	FeatureID string      `json:"feature_id,omitempty"`
	ChoiceID  string      `json:"choice_id,omitempty"`
	Target    string      `json:"target,omitempty"`
	Value     string      `json:"value,omitempty"` // the offending pick/value; part of the key

	// Filled in by Compute from Character.Acknowledgements. Warnings are never hidden by
	// the API; frontends decide whether to dim or collapse acknowledged ones.
	Acknowledged bool   `json:"acknowledged"`
	AckNote      string `json:"ack_note,omitempty"`
}

// WarningKey includes the offending value on purpose: change the pick and the key changes,
// so an old acknowledgement lapses instead of silently covering a new problem.
// e.g. "not_in_pool:champion:fighting_style:great_weapon_master"
func WarningKey(code WarningCode, featureID, choiceID, value string) string {
	return string(code) + ":" + featureID + ":" + choiceID + ":" + value
}

// Acknowledgement is a per-character "yes, I know, it's allowed" for one specific warning.
// Use for genuine one-offs; table-wide house rules should be fixed in the Ruleset or library instead.
type Acknowledgement struct {
	Key  string    `json:"key"`
	Note string    `json:"note,omitempty"` // "DM approved", "homebrew exception"
	At   time.Time `json:"at"`
}

// PendingChoice: a choice the character is entitled to but hasn't made.
// Drives level-up flows, and surfaces new choices added by editing homebrew.
type PendingChoice struct {
	FeatureID string `json:"feature_id"`
	ChoiceID  string `json:"choice_id"`
	Prompt    string `json:"prompt"`
	Remaining int    `json:"remaining"`
}

// EligibleOption is what an "options" endpoint returns. The API reports eligibility;
// the frontend decides whether to hide ineligible options (strict UI) or show them (permissive UI).
type EligibleOption struct {
	OptionID string `json:"option_id"`
	Name     string `json:"name"`
	Eligible bool   `json:"eligible"`
	Reason   string `json:"reason,omitempty"` // why not, when Eligible is false
}

type SkillDef struct {
	ID             string  `json:"id"` // "athletics"
	Name           string  `json:"name"`
	DefaultAbility Ability `json:"default_ability"`
}

type Sense struct {
	Name  string `json:"name"` // darkvision, blindsight, ...
	Range int    `json:"range"`
}

// Ruleset holds table-level homebrew switches so they aren't hardcoded.
type Ruleset struct {
	MaxInspiration   int           `json:"max_inspiration"`              // 3 for your table
	BaseAC           *ACFormula    `json:"base_ac,omitempty"`            // nil => unarmoured 10 + Dex
	Unarmed          *WeaponStats  `json:"unarmed,omitempty"`            // nil => 1 bludgeoning, 5 ft, Str
	SlotTable        [][]int       `json:"slot_table,omitempty"`         // row = caster level, entries = slots for spell levels 1..9; nil => standard table
	PactTable        []PactRow     `json:"pact_table,omitempty"`         // row = warlock level; nil => standard table
	Currencies       []CurrencyDef `json:"currencies,omitempty"`         // nil => cp, sp, ep, gp, pp
	CoinsPerWeight   int           `json:"coins_per_weight,omitempty"`   // 0 => 50 per weight unit; negative => coins weigh nothing
	Encumbrance      string        `json:"encumbrance,omitempty"`        // "" or "capacity" (default), "variant", "none"
	CarryPerStrength int           `json:"carry_per_strength,omitempty"` // 0 => 15 per point of Strength
	MaxAttuned       int           `json:"max_attuned,omitempty"`        // 0 => 3; negative => unlimited
	WeightUnit       string        `json:"weight_unit,omitempty"`        // label only; "" => "lb"
	// JackAppliesToInitiative bool, etc. as they come up
}

// ---------------------------------------------------------------------------
// Character layer (stored state only)
// ---------------------------------------------------------------------------

type ClassLevel struct {
	ClassID    string `json:"class_id"`
	SubclassID string `json:"subclass_id,omitempty"`
	Level      int    `json:"level"`
	RolledHP   []int  `json:"rolled_hp"` // one entry per level taken in this class
}

type AbilityScore struct {
	Score          int  `json:"score"` // base score; mod is derived
	SaveProficient bool `json:"save_proficient"`
}

type InventoryItem struct {
	ItemID          string  `json:"item_id"`
	Quantity        int     `json:"quantity"`
	Equipped        bool    `json:"equipped"`
	Attuned         bool    `json:"attuned"`
	AbilityOverride Ability `json:"ability_override,omitempty"` // weapon attacks: force an ability (Monk, Pact of the Blade...)
	Proficient      bool    `json:"proficient,omitempty"`       // weapon attacks: proficient with this weapon whatever the character's weapon_profs (Pact of the Blade)
	ID              string  `json:"id,omitempty"`               // instance id; needed if something is stored inside this entry
	Container       string  `json:"container,omitempty"`        // id of the entry this one is stored in
	Stashed         bool    `json:"stashed,omitempty"`          // left behind (home, a mount): not counted in carried weight
	Custom          *Item   `json:"custom,omitempty"`           // a one-off item defined inline; used instead of item_id when set
	Notes           string  `json:"notes,omitempty"`
}

// Details are the descriptive parts of a character: free text, never read by the rules engine.
// Size is here as a description too; nothing computes with it yet.
type Details struct {
	Alignment string `json:"alignment,omitempty"`
	Gender    string `json:"gender,omitempty"`
	Age       string `json:"age,omitempty"`
	Size      string `json:"size,omitempty"`
	Height    string `json:"height,omitempty"`
	Weight    string `json:"weight,omitempty"`
	Eyes      string `json:"eyes,omitempty"`
	Hair      string `json:"hair,omitempty"`
	Skin      string `json:"skin,omitempty"`
	Faith     string `json:"faith,omitempty"`

	PersonalityTraits string `json:"personality_traits,omitempty"`
	Ideals            string `json:"ideals,omitempty"`
	Bonds             string `json:"bonds,omitempty"`
	Flaws             string `json:"flaws,omitempty"`
	Appearance        string `json:"appearance,omitempty"`
}

type Character struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	RaceID       string  `json:"race_id"`
	BackgroundID string  `json:"background_id"`
	PortraitURL  string  `json:"portrait_url,omitempty"` // an http(s) image link; the image itself isn't stored
	Details      Details `json:"details"`

	Classes []ClassLevel `json:"classes"` // character level = sum of Level

	Abilities map[Ability]AbilityScore `json:"abilities"`

	// Play state
	CurrentHP   int         `json:"current_hp"`
	TempHP      int         `json:"temp_hp"` // top level, as planned
	HitDiceUsed int         `json:"hit_dice_used"`
	DeathSaves  [2]int      `json:"death_saves"` // successes, failures
	Inspiration int         `json:"inspiration"` // validated against Ruleset.MaxInspiration
	Conditions  []Condition `json:"conditions"`
	Exhaustion  int         `json:"exhaustion"`

	// Proficiencies (stored as levels; bonuses are computed)
	Skills       map[string]ProficiencyLevel `json:"skills"`                  // keyed by SkillDef.ID
	SkillAbility map[string]Ability          `json:"skill_ability,omitempty"` // optional per-character override
	Tools        map[string]ProficiencyLevel `json:"tools"`
	ArmourProfs  []string                    `json:"armour_profs"`
	WeaponProfs  []string                    `json:"weapon_profs"`
	Languages    []string                    `json:"languages"`

	FeatureIDs []string    `json:"feature_ids"` // references into the library
	Selections []Selection `json:"selections"`  // choices made on those features

	Acknowledgements []Acknowledgement `json:"acknowledgements,omitempty"` // suppression is per-character only

	ACFormulaOverride string `json:"ac_formula_override,omitempty"` // force a specific AC formula; unmet requirements warn, never reject

	ResourceUsed map[string]int `json:"resource_used,omitempty"` // feature id -> uses spent; maximums come from the feature
	Coins        map[string]int `json:"coins,omitempty"`         // currency id -> amount; negative means owed

	Spellbooks     map[string]SpellState `json:"spellbooks,omitempty"`       // keyed by source id: "class:<id>"
	SpellSlotsUsed map[int]int           `json:"spell_slots_used,omitempty"` // spell level -> slots spent (shared pool)
	PactSlotsUsed  int                   `json:"pact_slots_used,omitempty"`
	Concentrating  string                `json:"concentrating,omitempty"` // spell id
	Modifiers      []Modifier            `json:"modifiers"`               // ad hoc, character-specific
	Inventory      []InventoryItem       `json:"inventory"`

	// Linked entities, each with their own record later
	CompanionIDs    []string `json:"companion_ids,omitempty"`
	OrganisationIDs []string `json:"organisation_ids,omitempty"`
}

// ---------------------------------------------------------------------------
// Sheet layer (computed, returned by the API, never persisted)
// ---------------------------------------------------------------------------

type AbilityView struct {
	Score     int  `json:"score"`
	Mod       int  `json:"mod"`
	SaveBonus int  `json:"save_bonus"`
	SaveProf  bool `json:"save_proficient"`
}

type SkillView struct {
	Ability      Ability          `json:"ability"`
	Level        ProficiencyLevel `json:"level"`
	Bonus        int              `json:"bonus"`
	Passive      int              `json:"passive"`
	Advantage    bool             `json:"advantage"`
	Disadvantage bool             `json:"disadvantage"`
}

type ToolView struct {
	Level     ProficiencyLevel `json:"level"`
	ProfBonus int              `json:"prof_bonus"` // the proficiency part only; frontend formats "Mod+<ProfBonus>"
}

type Sheet struct {
	Level            int                     `json:"level"`
	ProficiencyBonus int                     `json:"proficiency_bonus"`
	MaxHP            int                     `json:"max_hp"` // sum(RolledHP) + level*conMod + modifiers
	AC               int                     `json:"ac"`
	ACDetail         ACBreakdown             `json:"ac_detail"`
	Initiative       int                     `json:"initiative"`
	Abilities        map[Ability]AbilityView `json:"abilities"`
	Skills           map[string]SkillView    `json:"skills"`
	Tools            map[string]ToolView     `json:"tools"` // trained/expert only
	Speeds           map[SpeedType]int       `json:"speeds"`
	Senses           []Sense                 `json:"senses"`
	Resistances      []DamageType            `json:"resistances"`
	Immunities       []DamageType            `json:"immunities"`
	Vulnerabilities  []DamageType            `json:"vulnerabilities"`
	Features         []FeatureView           `json:"features"`
	Actions          []Action                `json:"actions"`
	Attacks          []AttackView            `json:"attacks"`
	Resources        []ResourceView          `json:"resources"`
	CasterLevel      int                     `json:"caster_level"` // combined, for multiclass slot tables (pact casters excluded)
	Spellcasting     []SpellSourceView       `json:"spellcasting"`
	SpellSlots       []SlotView              `json:"spell_slots"`
	PactSlots        *SlotView               `json:"pact_slots,omitempty"`
	Inventory        []InventoryView         `json:"inventory"`
	Wallet           WalletView              `json:"wallet"`
	Carry            CarryView               `json:"carry"`
	Notes            []Modifier              `json:"notes"` // situational reminders: advantage, "vs. poison", etc.
	PendingChoices   []PendingChoice         `json:"pending_choices"`
	Warnings         []Warning               `json:"warnings"`  // permissive validation output
	Languages        []string                `json:"languages"` // merged from grants
	ArmourProfs      []string                `json:"armour_profs"`
	WeaponProfs      []string                `json:"weapon_profs"`
}

// ---------------------------------------------------------------------------
// Formulas, AC and items
// ---------------------------------------------------------------------------

// Term is one addend in a Formula. Source is "mod.<ability>", "pb", "level" or "class_level.<classId>".
type Term struct {
	Source string `json:"source"`
	Max    *int   `json:"max,omitempty"` // cap on this term, e.g. medium armour caps mod.dex at 2
	Div    int    `json:"div,omitempty"` // divide this term (rounding down) before capping, e.g. half class level
}

// Formula is Base plus its Terms. Reusable beyond AC later (resource maximums, spell save DCs...).
type Formula struct {
	Base  int    `json:"base"`
	Terms []Term `json:"terms,omitempty"`
}

// ACFormula is one way of calculating AC. Unarmoured Defense, Mage Armor, natural armour and
// armour items are all just these. The best eligible one is used, unless the character sets
// ACFormulaOverride.
type ACFormula struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Formula  Formula `json:"formula"`
	NoArmour bool    `json:"no_armour,omitempty"` // only eligible while no armour is worn
	NoShield bool    `json:"no_shield,omitempty"` // only eligible while no shield is used (Monk)
}

type ItemKind string

const (
	ItemArmour ItemKind = "armour"
	ItemShield ItemKind = "shield"
	ItemWeapon ItemKind = "weapon"
	ItemOther  ItemKind = "other"
)

// Item is library data. A shield is an ItemShield with Modifiers {ac add 2}; magic armour adds its
// bonus as a modifier on the armour item; heavy armour has an ACFormula with no terms.
type Item struct {
	ID                 string        `json:"id"`
	Name               string        `json:"name"`
	Kind               ItemKind      `json:"kind"`
	Description        string        `json:"description,omitempty"`
	ACFormula          *ACFormula    `json:"ac_formula,omitempty"` // armour only
	Weapon             *WeaponStats  `json:"weapon,omitempty"`
	Spells             *SpellGrant   `json:"spells,omitempty"`    // spells cast from the item (needs attunement if the item does)
	Modifiers          []Modifier    `json:"modifiers,omitempty"` // apply while equipped (and attuned, if required)
	Actions            []Action      `json:"actions,omitempty"`   // e.g. a weapon's attack
	RequiresAttunement bool          `json:"requires_attunement,omitempty"`
	Cost               *Price        `json:"cost,omitempty"`
	Weight             float64       `json:"weight,omitempty"` // in the ruleset's weight unit (pounds by default)
	Rarity             string        `json:"rarity,omitempty"`
	Tags               []string      `json:"tags,omitempty"`
	Container          *ContainerDef `json:"container,omitempty"`
}

// ACOption is one candidate AC calculation, shown on the sheet so a UI can display its working.
type ACOption struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Value     int    `json:"value"`
	Eligible  bool   `json:"eligible"`
	Reason    string `json:"reason,omitempty"`     // why not eligible
	FromSpell bool   `json:"from_spell,omitempty"` // only used when chosen with ac_formula_override
}

type ACBreakdown struct {
	Total        int        `json:"total"`
	FormulaID    string     `json:"formula_id"`
	Formula      string     `json:"formula"` // name of the formula used
	FormulaValue int        `json:"formula_value"`
	Bonus        int        `json:"bonus"`      // Total - FormulaValue: shields, magic bonuses, etc.
	Overridden   bool       `json:"overridden"` // true if the character's override picked the formula
	Options      []ACOption `json:"options"`    // eligible first, best first
}

// ---------------------------------------------------------------------------
// Weapons and attacks
// ---------------------------------------------------------------------------

// WeaponStats hangs off Item.Weapon. The inherent magic bonus lives here (not in general modifiers),
// so +1 weapons are self-contained. Melee vs ranged comes from Attack.Ranged.
type WeaponStats struct {
	Category      string        `json:"category"`                 // "simple" | "martial" | "other"
	Attack        AttackDetails `json:"attack"`                   // dice, damage type, range, properties
	VersatileDice string        `json:"versatile_dice,omitempty"` // e.g. "1d10" when held two-handed
	MagicBonus    int           `json:"magic_bonus,omitempty"`    // applies to hit and damage; needs attunement if the item does
}

// Rider is a dice bonus attached to an attack ("+1d6 Hunter's Mark", "+1d4 Bless").
type Rider struct {
	Dice      string `json:"dice"`
	Condition string `json:"condition,omitempty"`
	Note      string `json:"note,omitempty"`
	Source    string `json:"source,omitempty"` // library id of what grants it
}

// AttackView is one computed attack: equipped weapons, feature-granted attacks, and unarmed strike.
type AttackView struct {
	Name          string     `json:"name"`
	ItemID        string     `json:"item_id,omitempty"`
	Ability       Ability    `json:"ability"`
	Proficient    bool       `json:"proficient"`
	ToHit         int        `json:"to_hit"`
	Advantage     bool       `json:"advantage"`
	Disadvantage  bool       `json:"disadvantage"`
	DamageDice    string     `json:"damage_dice,omitempty"`
	DamageBonus   int        `json:"damage_bonus"`
	DamageType    DamageType `json:"damage_type"`
	VersatileDice string     `json:"versatile_dice,omitempty"`
	RangeNormal   int        `json:"range_normal,omitempty"`
	RangeLong     int        `json:"range_long,omitempty"`
	Properties    []string   `json:"properties,omitempty"`
	AttackRiders  []Rider    `json:"attack_riders,omitempty"` // dice added to the attack roll
	DamageRiders  []Rider    `json:"damage_riders,omitempty"` // dice added to damage
}

// ---------------------------------------------------------------------------
// Classes, progression and scaling
// ---------------------------------------------------------------------------

// Tier is one rung of a scaling feature (Sneak Attack 1d6 / 2d6 / 3d6, Rage uses and damage).
// Only the highest tier whose MinLevel is at or below the relevant level applies; it is applied in
// addition to the feature's own base Modifiers/Actions/Grants. Tiers replace each other, they never stack.
type Tier struct {
	MinLevel  int         `json:"min_level"`
	Modifiers []Modifier  `json:"modifiers,omitempty"`
	Actions   []Action    `json:"actions,omitempty"`
	Grants    Grants      `json:"grants,omitempty"`
	Resource  *Resource   `json:"resource,omitempty"` // replaces the feature's base resource at this tier
	Spells    *SpellGrant `json:"spells,omitempty"`   // spells unlocked at this tier; added to the feature's own spells (a tier replaces lower tiers, so list every spell that should still apply)
}

// FeatureView is one feature the character has, resolved through classes, race, background, feats and picks.
type FeatureView struct {
	FeatureID   string `json:"feature_id"` // instance id, as used by resources, selections and actions
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Source      Origin `json:"source"`
	SourceID    string `json:"source_id,omitempty"`   // the class, race or background it belongs to; a feat's own id
	GrantedBy   string `json:"granted_by,omitempty"`  // the feature whose grant or choice added it; empty for top-level ones
	Level       int    `json:"level,omitempty"`       // class level it was gained at; 0 outside a class
	ResourceID  string `json:"resource_id,omitempty"` // its limited-use resource in Sheet.Resources, if any
}

// ResourceView is a feature's limited-use resource with the character's spend applied.
type ResourceView struct {
	FeatureID string       `json:"feature_id"`
	Name      string       `json:"name"`
	Max       int          `json:"max"`
	Used      int          `json:"used"`
	Remaining int          `json:"remaining"`
	Recharge  RechargeType `json:"recharge"`
}

// SpellcastingDef is only a definition for now; slots, spells and DCs come with the spells work.
type SpellcastingDef struct {
	Ability         Ability  `json:"ability"`
	Divisor         int      `json:"divisor"`            // 1 full caster, 2 half, 3 third; 0 none
	RoundUp         bool     `json:"round_up,omitempty"` // Artificer rounds up; Paladin and Ranger round down
	Pact            bool     `json:"pact,omitempty"`     // Warlock-style own slot pool; not added to caster level
	SpellListID     string   `json:"spell_list_id,omitempty"`
	Ritual          bool     `json:"ritual,omitempty"`
	StartLevel      int      `json:"start_level,omitempty"`      // class level when casting begins; 0 => 1 (Paladin/Ranger 2, third-casters 3)
	Prep            string   `json:"prep,omitempty"`             // PrepKnown (default), PrepPrepared (from the class list), PrepBook (spellbook)
	CantripsKnown   []int    `json:"cantrips_known,omitempty"`   // by class level; empty = not enforced
	SpellsKnown     []int    `json:"spells_known,omitempty"`     // by class level, for PrepKnown casters; empty = not enforced
	PreparedFormula *Formula `json:"prepared_formula,omitempty"` // PrepPrepared/PrepBook, e.g. mod.wis + class_level.cleric
}

type Subclass struct {
	ID     string     `json:"id"`
	Name   string     `json:"name"`
	Levels [][]string `json:"levels"` // Levels[i] = feature ids gained at class level i+1
	// ExpandedSpells are added to the class's spell list for characters with this subclass
	// (a warlock patron's expanded list, a cleric domain's spells). They become legal picks
	// for the class's spell source; they are not granted for free.
	ExpandedSpells []string `json:"expanded_spells,omitempty"`
}

// Class is a table of features by level plus what the class grants at the start.
// Starting choices (e.g. pick 2 skills) live on a synthetic feature with id "class:<id>:start",
// so selections for them use that feature id.
type Class struct {
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	HitDie           int              `json:"hit_die"`
	MaxLevel         int              `json:"max_level,omitempty"` // 0 => 20
	StartingGrants   Grants           `json:"starting_grants"`     // first class only; Saves here are the class's save proficiencies
	StartingChoices  []Choice         `json:"starting_choices,omitempty"`
	MulticlassGrants Grants           `json:"multiclass_grants"`           // when taken as a later class
	MulticlassPrereq []Prerequisite   `json:"multiclass_prereq,omitempty"` // failures warn
	Levels           [][]string       `json:"levels"`                      // Levels[i] = feature ids gained at class level i+1
	SubclassLevel    int              `json:"subclass_level,omitempty"`
	Subclasses       []Subclass       `json:"subclasses,omitempty"`
	Spellcasting     *SpellcastingDef `json:"spellcasting,omitempty"`
}

// ---------------------------------------------------------------------------
// Spells
// ---------------------------------------------------------------------------

const (
	PrepKnown    = "known"    // spells known (Bard, Sorcerer, Warlock, Ranger)
	PrepPrepared = "prepared" // prepared from the whole class list (Cleric, Druid, Paladin)
	PrepBook     = "book"     // spellbook, with a prepared subset (Wizard)
)

type SpellSourceKind string

const (
	SourceClass   SpellSourceKind = "class"
	SourceFeature SpellSourceKind = "feature"
	SourceItem    SpellSourceKind = "item"
)

// DiceTier replaces an effect's dice from MinLevel (character level) up: cantrip scaling at 5/11/17.
type DiceTier struct {
	MinLevel int    `json:"min_level"`
	Dice     string `json:"dice"`
}

// SpellEffect is optional structure on a spell. Spells without it are just text.
type SpellEffect struct {
	Kind          string     `json:"kind"` // "damage" | "healing"
	Dice          string     `json:"dice"` // "8d6"
	DamageType    DamageType `json:"damage_type,omitempty"`
	AddAbilityMod bool       `json:"add_ability_mod,omitempty"` // healing spells usually add the casting modifier
	DiceTiers     []DiceTier `json:"dice_tiers,omitempty"`      // by character level
	UpcastDice    string     `json:"upcast_dice,omitempty"`     // added per slot level above the spell's level
	Note          string     `json:"note,omitempty"`            // e.g. "per ray", "half on a successful save"
}

type Spell struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Level         int           `json:"level"` // 0 = cantrip
	School        string        `json:"school,omitempty"`
	CastingTime   string        `json:"casting_time,omitempty"`
	Range         string        `json:"range,omitempty"`
	Components    string        `json:"components,omitempty"`
	Duration      string        `json:"duration,omitempty"`
	Ritual        bool          `json:"ritual,omitempty"`
	Concentration bool          `json:"concentration,omitempty"`
	Description   string        `json:"description,omitempty"`
	HigherLevels  string        `json:"higher_levels,omitempty"`
	Attack        bool          `json:"attack,omitempty"`      // needs a spell attack roll
	Save          Ability       `json:"save,omitempty"`        // target saves with this ability against the source's DC
	SaveEffect    string        `json:"save_effect,omitempty"` // "half damage", "negates", ...
	Effects       []SpellEffect `json:"effects,omitempty"`
	ACFormula     *ACFormula    `json:"ac_formula,omitempty"` // Mage Armor: selectable via ac_formula_override while castable
}

type SpellList struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	SpellIDs []string `json:"spell_ids"`
}

// SpellGrant lets a feature or item cast spells outside a class (innate casting, Magic Initiate, staffs).
type SpellGrant struct {
	SpellIDs []string  `json:"spell_ids"`
	Ability  Ability   `json:"ability"`            // casting ability for these spells
	Resource *Resource `json:"resource,omitempty"` // limited uses or charges; spend is tracked in ResourceUsed
}

// SpellState is what the character has chosen for one source.
type SpellState struct {
	Cantrips []string `json:"cantrips,omitempty"`
	Known    []string `json:"known,omitempty"`    // PrepKnown casters
	Book     []string `json:"book,omitempty"`     // PrepBook casters: spells in the spellbook
	Prepared []string `json:"prepared,omitempty"` // PrepPrepared and PrepBook casters
}

type PactRow struct {
	Slots int `json:"slots"`
	Level int `json:"level"` // slot level
}

type SlotView struct {
	Level     int `json:"level"`
	Max       int `json:"max"`
	Used      int `json:"used"`
	Remaining int `json:"remaining"`
}

type UpcastEntry struct {
	SlotLevel int    `json:"slot_level"`
	Dice      string `json:"dice"`
}

type SpellEffectView struct {
	Kind       string        `json:"kind"`
	Dice       string        `json:"dice"` // resolved for the character's level
	DamageType DamageType    `json:"damage_type,omitempty"`
	Bonus      int           `json:"bonus"` // flat: casting modifier (if it applies) plus damage.spell modifiers
	Riders     []Rider       `json:"riders,omitempty"`
	Upcast     []UpcastEntry `json:"upcast,omitempty"`
	Note       string        `json:"note,omitempty"`
}

type SpellView struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Level         int               `json:"level"`
	School        string            `json:"school,omitempty"`
	CastingTime   string            `json:"casting_time,omitempty"`
	Range         string            `json:"range,omitempty"`
	Components    string            `json:"components,omitempty"`
	Duration      string            `json:"duration,omitempty"`
	Ritual        bool              `json:"ritual,omitempty"`
	Concentration bool              `json:"concentration,omitempty"`
	Prepared      bool              `json:"prepared"`
	Description   string            `json:"description,omitempty"`
	HigherLevels  string            `json:"higher_levels,omitempty"`
	Attack        bool              `json:"attack,omitempty"`
	AttackBonus   int               `json:"attack_bonus,omitempty"`
	SaveAbility   Ability           `json:"save_ability,omitempty"`
	SaveDC        int               `json:"save_dc,omitempty"`
	SaveEffect    string            `json:"save_effect,omitempty"`
	Effects       []SpellEffectView `json:"effects,omitempty"`
}

type SpellSourceView struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Kind            SpellSourceKind `json:"kind"`
	Ability         Ability         `json:"ability,omitempty"`
	SaveDC          int             `json:"save_dc"`
	AttackBonus     int             `json:"attack_bonus"`
	Prep            string          `json:"prep"`             // known | prepared | book | innate
	CantripsAllowed int             `json:"cantrips_allowed"` // -1 = not specified
	SpellsAllowed   int             `json:"spells_allowed"`   // known or prepared count, whichever applies; -1 = not specified
	Cantrips        []SpellView     `json:"cantrips"`
	Spells          []SpellView     `json:"spells"` // known spells, or the prepared ones for prepared/book casters
}

// ---------------------------------------------------------------------------
// Inventory, currency and carrying
// ---------------------------------------------------------------------------

const (
	EncumbranceCapacity = "capacity" // show weight against capacity; no penalties (default)
	EncumbranceVariant  = "variant"  // also apply the encumbered / heavily encumbered penalties
	EncumbranceNone     = "none"     // weight only
)

// CurrencyDef is one denomination. Value is its worth in base units (the smallest coin is 1).
type CurrencyDef struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Abbrev string `json:"abbrev"`
	Value  int    `json:"value"`
}

type Price struct {
	Amount   int    `json:"amount"`
	Currency string `json:"currency"`
}

// ContainerDef marks an item as holding other inventory entries.
type ContainerDef struct {
	Capacity              float64 `json:"capacity,omitempty"`                // weight it can hold; 0 = no limit
	IgnoresContentsWeight bool    `json:"ignores_contents_weight,omitempty"` // Bag of Holding: contents add no carried weight
}

// InventoryView is one resolved inventory entry as the sheet shows it.
type InventoryView struct {
	EntryID            string        `json:"entry_id,omitempty"`
	ItemID             string        `json:"item_id,omitempty"`
	Name               string        `json:"name"`
	Kind               ItemKind      `json:"kind,omitempty"`
	Quantity           int           `json:"quantity"`
	Equipped           bool          `json:"equipped"`
	Attuned            bool          `json:"attuned"`
	Stashed            bool          `json:"stashed"`
	Container          string        `json:"container,omitempty"`
	Weight             float64       `json:"weight"`                   // total for this stack
	Counted            bool          `json:"counted"`                  // whether it counts toward carried weight
	Contents           float64       `json:"contents,omitempty"`       // containers: weight of everything inside, nested too; absent = 0
	ContainerInfo      *ContainerDef `json:"container_info,omitempty"` // set when the entry is a container (even an empty one)
	Cost               *Price        `json:"cost,omitempty"`           // per unit
	Rarity             string        `json:"rarity,omitempty"`
	Description        string        `json:"description,omitempty"`
	Notes              string        `json:"notes,omitempty"`
	RequiresAttunement bool          `json:"requires_attunement,omitempty"`
	Custom             bool          `json:"custom,omitempty"`
	Unknown            bool          `json:"unknown,omitempty"` // item id not found in the library
}

type CoinView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Abbrev string `json:"abbrev"`
	Amount int    `json:"amount"`
	Value  int    `json:"value"` // worth of one coin in base units
}

type WalletView struct {
	Coins []CoinView `json:"coins"`
	Total int        `json:"total"` // all coins in base units
}

const (
	CarryOK                = "ok"
	CarryEncumbered        = "encumbered"
	CarryHeavilyEncumbered = "heavily_encumbered"
	CarryOverCapacity      = "over_capacity"
)

type CarryView struct {
	Weight       float64 `json:"weight"`   // carried weight, coins included
	Capacity     int     `json:"capacity"` // 0 when the encumbrance mode is "none"
	PushDragLift int     `json:"push_drag_lift"`
	Unit         string  `json:"unit"`
	Mode         string  `json:"mode"`
	State        string  `json:"state"` // ok | encumbered | heavily_encumbered | over_capacity
}

// Library is whatever lookup you build (DB, JSON files, in-memory map).
type Library interface {
	Feature(id string) (Feature, error)
	Skills() []SkillDef // the full skill list with default abilities
	Item(id string) (Item, error)
	Class(id string) (Class, error)
	Spell(id string) (Spell, error)
	SpellList(id string) (SpellList, error)
	// Item, Race, Class ... added as needed
}

// Compute lives in compute.go.
