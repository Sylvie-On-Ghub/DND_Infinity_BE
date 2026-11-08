# Pets: design

Date: 2026-11-08
Status: approved in conversation, awaiting spec review

## Goal

One shared model for creatures that belong to a character, so players can track them at the table:

- **Find Familiar** familiars (any caster with the spell).
- **Pact of the Chain** familiars, including its special forms and Investment of the Chain Master.
- **Beast Master** Primal Companions. Ranger content is out of scope here; the model must support it
  without further engine changes.

Success: a player can add a pet, see its computed stat block on their sheet, and track its HP,
conditions and status during play. Stats that scale with the owner (Primal Companion AC, HP, attack)
update automatically when the owner changes.

## Decisions

- **Catalog plus owned list.** Creature stat blocks are library content (`creatures/` in a pack). Each
  character stores a list of the pets it owns, each referencing a creature by id. Pets live inside the
  character's JSON, not in their own table.
- **Full play state** per pet: name, form, status (active / dismissed / dead), current and temp HP,
  conditions, ad hoc modifiers, notes.
- **Formulas with owner terms.** Every scaling number on a stat block is a `Formula`, which may read
  the owner's finished sheet through `owner.*` terms. Owner features affect pets through ordinary
  `Modifier`s with `pet.<tag>.*` targets. No per-subclass Go code.
- **Permissive.** Rules problems are warnings on the sheet, never rejections.

Not doing (YAGNI): inline one-off pets (`Custom *Creature`), pet inventory or equipment, pet Hit Dice
and short-rest healing, pets shared between characters.

## 1. Data model

### Library: `Creature`

Loaded from `creatures/` in a content pack, any depth, one object or an array per file, like the
other content folders. A later pack replaces an earlier creature with the same id.

```go
type Creature struct {
	ID          string                      `json:"id"`
	Name        string                      `json:"name"`
	Size        string                      `json:"size"`           // "Tiny", "Medium"...
	Type        string                      `json:"type"`           // "beast", "celestial", "fey"...
	Tags        []string                    `json:"tags,omitempty"` // "familiar", "chain_familiar", "primal_companion"
	Abilities   map[Ability]int             `json:"abilities"`
	AC          Formula                     `json:"ac"`
	HP          Formula                     `json:"hp"`
	PB          *Formula                    `json:"pb,omitempty"` // nil => 2
	Speeds      map[SpeedType]int           `json:"speeds"`
	Senses      []Sense                     `json:"senses,omitempty"`
	Saves       []Ability                   `json:"saves,omitempty"` // proficient saving throws
	Skills      map[string]ProficiencyLevel `json:"skills,omitempty"`
	Resistances []DamageType                `json:"resistances,omitempty"`
	Immunities  []DamageType                `json:"immunities,omitempty"`
	Languages   []string                    `json:"languages,omitempty"`
	Traits      []Trait                     `json:"traits,omitempty"`
	Attacks     []CreatureAttack            `json:"attacks,omitempty"`
	Actions     []Action                    `json:"actions,omitempty"` // non-attack actions
	Modifiers   []Modifier                  `json:"modifiers,omitempty"`
}

type Trait struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type CreatureAttack struct {
	Name        string     `json:"name"`
	Type        ActionType `json:"type"`
	Ranged      bool       `json:"ranged,omitempty"`
	Reach       int        `json:"reach,omitempty"`        // melee
	RangeNormal int        `json:"range_normal,omitempty"` // ranged
	RangeLong   int        `json:"range_long,omitempty"`
	ToHit       Formula    `json:"to_hit"`
	DamageDice  string     `json:"damage_dice,omitempty"`
	Damage      Formula    `json:"damage"` // flat part added to the dice
	DamageType  DamageType `json:"damage_type"`
	Description string     `json:"description,omitempty"` // riders, save DCs, etc.
}
```

`CreatureAttack` is separate from `AttackDetails` on purpose. Stat blocks give to-hit directly, and a
Primal Companion's is "your spell attack modifier", which a `Formula` expresses and `AttackDetails`
does not. Character attack code is untouched.

Example: an Owl's Talons is `to_hit {terms: [mod.dex, pb]}`, `damage_dice ""`, `damage {base: 1}`.
A Primal Beast of the Land's Beast's Strike is `to_hit {terms: [owner.spell_attack]}`,
`damage_dice "1d8"`, `damage {base: 2, terms: [owner.mod.wis]}`.

### Granting pets: `PetGrant`

`Feature` and `Spell` each gain an optional field:

```go
type PetGrant struct {
	Tags        []string `json:"tags,omitempty"`         // creatures with any of these tags are allowed
	CreatureIDs []string `json:"creature_ids,omitempty"` // plus these specific creatures
}

// on Feature and Spell:
Pets *PetGrant `json:"pets,omitempty"`
```

### Character: `Pet`

```go
type PetStatus string

const (
	PetActive    PetStatus = "active"
	PetDismissed PetStatus = "dismissed" // e.g. a familiar in its pocket dimension
	PetDead      PetStatus = "dead"      // dropped to 0 HP; a Primal Companion awaiting revival
)

type Pet struct {
	ID         string      `json:"id"`          // instance id, stable across form changes
	CreatureID string      `json:"creature_id"` // which stat block
	SourceID   string      `json:"source_id"`   // library id of the granting feature or spell
	Name       string      `json:"name,omitempty"`
	Status     PetStatus   `json:"status"`
	CurrentHP  int         `json:"current_hp"`
	TempHP     int         `json:"temp_hp"`
	Conditions []Condition `json:"conditions,omitempty"`
	Modifiers  []Modifier  `json:"modifiers,omitempty"` // ad hoc, like Character.Modifiers
	Notes      string      `json:"notes,omitempty"`
}

// on Character:
Pets []Pet `json:"pets,omitempty"`
```

`SourceID` decides which spellcasting `owner.spell_attack` and `owner.spell_save_dc` read, and which
`PetGrant` the creature is checked against. For a Chain warlock's familiar it is `pact_of_the_chain`,
not `find_familiar`, because the pact allows more forms.

`Character.CompanionIDs` is removed: nothing reads it and `Pets` replaces it. Old characters that
stored it still load because decoding is lenient.

## 2. Computing a pet

### Formula context

`evalFormula(f, modOf, pb, level, classLevels)` becomes `evalFormula(f, ctx formulaCtx)`, where the
context holds the existing values plus an optional owner context. Existing callers build a context
with no owner and behave exactly as before.

| Term | Meaning |
|---|---|
| `mod.<ability>`, `pb`, `level`, `class_level.<id>` | the subject's own values (the pet's, for a creature; a pet has no level or class levels, so those are 0) |
| `owner.mod.<ability>` | owner's ability modifier |
| `owner.pb` | owner's proficiency bonus |
| `owner.level` | owner's character level |
| `owner.class_level.<id>` | owner's level in that class |
| `owner.spell_attack` | owner's spell attack bonus for the spellcasting source of the pet's `SourceID` |
| `owner.spell_save_dc` | owner's spell save DC for that source |

The spellcasting source is the `SpellSourceView` that the pet's `SourceID` feature belongs to (for
a class feature, `class:<source_id>`; for a spell, the source that has the spell). If none is found,
the term is 0 and the pet gets a warning.

An `owner.*` term evaluated with no owner (in a character formula) counts as 0 and adds a sheet warning.

### Owner modifiers on pets

New target helper, in the style of the existing ones:

```go
func PetTarget(tag, target string) string { return "pet." + tag + "." + target } // pet.familiar.speed.fly
```

While computing the owner's sheet, modifiers whose target starts with `pet.` are collected separately
and not applied to the character. Each is applied to every pet whose creature has that tag, with the
`pet.<tag>.` prefix stripped. Supported inner targets: `ac`, `hp.max`, `speed.*`, `save.*`,
`skill.*`, `attack.all`, `damage.all`, `resist.*`, `immune.*`, and `ModNote` on anything.

A note's text can include the owner's spell save DC: notes from owner features get
`{owner.spell_save_dc}` and `{owner.spell_attack}` substituted with the resolved number.

Investment of the Chain Master in content:
- Notes: uses your save DC (`{owner.spell_save_dc}`), necrotic or radiant damage swap.
- A `Choice` "Aerial or Aquatic" with two options, each carrying a `pet.familiar.speed.fly` or
  `pet.familiar.speed.swim` `ModMin 40` modifier.
- The existing Command Familiar Attack and Familiar Resistance actions stay on the owner.

### Compute order

1. Compute the owner's sheet exactly as today (pet-targeted modifiers set aside).
2. For each pet: look up the creature, build its modifier list (creature `Modifiers`, then matching
   owner `pet.*` modifiers, then the pet's ad hoc `Modifiers`), deduplicate with the existing stacking
   rules, evaluate formulas with the owner context, and produce a `PetView`.

### Sheet output

```go
// on Sheet:
Pets []PetView `json:"pets"`

type PetView struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	CreatureID  string                  `json:"creature_id"`
	CreatureName string                 `json:"creature_name"`
	SourceID    string                  `json:"source_id"`
	Status      PetStatus               `json:"status"`
	Size        string                  `json:"size"`
	Type        string                  `json:"type"`
	Abilities   map[Ability]AbilityView `json:"abilities"`
	AC          int                     `json:"ac"`
	MaxHP       int                     `json:"max_hp"`
	CurrentHP   int                     `json:"current_hp"`
	TempHP      int                     `json:"temp_hp"`
	PB          int                     `json:"proficiency_bonus"`
	Speeds      map[SpeedType]int       `json:"speeds"`
	Senses      []Sense                 `json:"senses"`
	Skills      map[string]SkillView    `json:"skills"`
	Resistances []DamageType            `json:"resistances"`
	Immunities  []DamageType            `json:"immunities"`
	Languages   []string                `json:"languages"`
	Traits      []Trait                 `json:"traits"`
	Attacks     []PetAttackView         `json:"attacks"`
	Actions     []Action                `json:"actions"`
	Conditions  []Condition             `json:"conditions"`
	Notes       []Modifier              `json:"notes"`
	Warnings    []Warning               `json:"warnings"`
}

type PetAttackView struct {
	Name        string     `json:"name"`
	Type        ActionType `json:"type"`
	Range       string     `json:"range"`  // "reach 5 ft." or "80/320 ft."
	ToHit       int        `json:"to_hit"`
	Damage      string     `json:"damage"` // "1d8+5 piercing"
	Description string     `json:"description,omitempty"`
}
```

A pet whose creature id is unknown still gets a `PetView` with its stored play state and a warning, so
it never disappears from the sheet.

### Warnings

Pet warnings go on the `PetView`. None of them reject a save.

- Unknown `creature_id`.
- The owner has no feature or spell with the pet's `SourceID`.
- The source has no `PetGrant`, or the creature isn't allowed by it.
- More than one `active` pet with the same `SourceID` (one familiar at a time).
- `CurrentHP` above max.
- `owner.spell_attack` / `owner.spell_save_dc` used but no spellcasting source found.

## 3. API, content and testing

### API

Follows the inventory sub-route pattern: the per-character lock, version checks, strict request
bodies, every route returns `{"sheet": ...}` plus any extra fields.

| Route | Body | Does |
|---|---|---|
| `POST /api/characters/{id}/pets` | `{creature_id, source_id, name?}` | Adds a pet, status `active`, HP at max. Returns `pet_id`. |
| `PATCH /api/characters/{id}/pets/{petId}` | any of `name, notes, status, creature_id, modifiers` | Edits it. Changing `creature_id` (recasting Find Familiar) resets HP to the new form's max, clears temp HP and conditions. |
| `DELETE /api/characters/{id}/pets/{petId}` | none | Removes it. |
| `POST /api/characters/{id}/pets/{petId}/hp` | `HPAction` | Damage, heal, temp HP, using the character's HP rules. Reaching 0 sets status `dead`. Healing a `dead` pet does not revive it; set status with PATCH. |
| `POST /api/characters/{id}/pets/{petId}/conditions` | same as character conditions | Adds or removes conditions. |

An unknown `petId` is a 404. A `creature_id` that isn't in the library is a 400 on add and patch,
since there would be no max HP to start from; a stored pet whose creature later vanishes from content
only warns.

**Rest:** a long rest restores every `active` and `dismissed` pet to max HP and clears temp HP. A short
rest does nothing to pets.

**Content routes:** `GET /api/content/creatures` and `GET /api/content/creatures/{id}` in
`content_query.go`.

**Docs:** `openapi.json` and `apidocs.go` gain the routes and schemas; `companion_ids` is removed
from `openapi.json`.

### Content (local packs, gitignored)

- `content/classes/creatures/familiars.json`: the 2024 Find Familiar forms (Bat, Cat, Frog, Hawk,
  Lizard, Octopus, Owl, Rat, Raven, Spider, Weasel), tagged `familiar` and `standard_familiar`.
- `content/classes/creatures/chain_familiars.json`: Imp, Pseudodragon, Quasit, Skeleton, Slaad
  Tadpole, Sphinx of Wonder, Sprite, Venomous Snake, tagged `familiar` and `chain_familiar`.
- `find_familiar` spell: `pets: {tags: ["standard_familiar"]}`.
- `pact_of_the_chain`: `pets: {tags: ["standard_familiar", "chain_familiar"]}`.
- `investment_of_the_chain_master`: modifiers and choice as in Section 2, targeting tag `familiar`.
- Primal Beast of the Land, Sea and Sky are added with the ranger work, tagged `primal_companion`.

Tag summary: `familiar` means "is a familiar", which owner modifiers target; `standard_familiar` and
`chain_familiar` say which source can summon the form. A homebrew familiar form just carries the
right tags.

### Loader validation

Strict, like existing content:
- Unknown JSON fields are errors.
- `owner.*` terms anywhere other than a creature's formulas are errors.
- `PetGrant.CreatureIDs` that aren't loaded creatures are errors.
- A `PetTarget` tag that no loaded creature has is a warning.
- A creature with no `abilities` or a malformed formula term is an error.

### Testing

TDD. New `pets_test.go`, in the style of the class test files, plus additions to `api_test.go`,
`loader_test.go` and `store_test.go`.

- **Formulas:** `owner.mod.wis`, `owner.class_level.ranger`, `owner.pb` and `owner.spell_attack`
  resolve; a Primal-style test creature's AC, HP and attack change as the owner levels; an `owner.`
  term in a character formula warns and counts as 0; existing formulas are unchanged.
- **Owner modifiers:** Investment of the Chain Master gives a Chain familiar its chosen fly or swim
  speed and a note with the warlock's actual DC; a pet without the `familiar` tag is unaffected;
  `pet.*` modifiers never change the owner's own sheet.
- **Warnings:** each case listed in Section 2.
- **API:** add, patch, form swap resets HP, damage to 0 sets `dead`, healing doesn't revive,
  conditions, delete, unknown pet 404, unknown creature 400, version conflict, long rest heals pets.
- **Loader:** creatures load from any depth, a later pack overrides, unknown fields and bad
  `owner.*` placement are rejected.
- **Store:** a character with no `pets` loads; one with stored `companion_ids` loads and drops it.
