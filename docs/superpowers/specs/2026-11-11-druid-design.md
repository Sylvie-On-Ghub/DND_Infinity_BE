# Druid and Circle of the Moon design

Adds the Druid, the last core class, with Circle of the Moon as its only subclass. Wild Shape is a
form (see `2026-11-11-forms-design.md`) that also substitutes a Beast's stat block into the sheet.
Everything is driven from the API. This builds on the forms work (branch `temp-forms`).

## Goals

- A Druid levels 1-20 with correct slots, cantrips, prepared spell counts and Wild Shape uses.
- Starting Wild Shape spends a use, grants temp HP, and turns `GET /sheet` into the shaped character
  according to "Rules While Shape-Shifted". Clients read the same sheet fields they always do.
- Known Forms are tracked as a choice, limited by count, max CR and fly speed by level.
- Circle of the Moon's riders (AC 13 + Wis, 3 × level temp HP, CR level ÷ 3, Increased Toughness) are
  content on its features, not Go code.
- The Druid spell list is complete.

## Non-goals

- Other Druid circles.
- Discord commands.
- Enforcing Wild Shape's duration, ending it on Incapacitated, or refusing spell casts while shaped. The
  sheet reports `can_cast: false`; nothing rejects a cast.
- Enforcing "replace one known form per Long Rest".
- Resource conversions as API actions. Wild Resurgence, Wild Companion and Archdruid's Nature Magician
  are described actions, as Font of Magic is; the player spends through the existing slot and resource
  endpoints.

## Data model

### Creatures

`Creature` gains:

```go
CR string `json:"cr,omitempty"` // "0", "1/8", "1/4", "1/2", "1".."30"
```

The loader rejects any other value. Existing familiars and companions may leave it empty. A creature
with the tag `wild_shape` must have a CR (load error otherwise). CRs compare numerically (`"1/2"` < `"1"`).

### Modifiers and AC formulas

`Modifier` gains:

```go
// ValueFormula computes the value from a Formula ("three times your Druid level"). Overrides Value.
ValueFormula *Formula `json:"value_formula,omitempty"`
```

It is evaluated with the character's formula context (`level`, `class_level.<id>`, `mod.<ability>`,
`pb`, with `div`/`mul`). The loader rejects unknown term sources, as it does for other formulas.

`ACFormula` gains:

```go
Form string `json:"form,omitempty"` // only eligible while this form is active
```

New modifier targets:

- `shape.max_cr` (kind `min`): raises the shape's max CR to a whole-number value.
- `shape.temp_hp` (kind `set`): replaces the shape's temp HP formula result.
- `shape.cast_spells` (kind `set`, value 1): the character can cast spells while shaped.

These are read only while a shape form is active or being started; outside that they are ignored (and
with `form: "wild_shape"` they show as the usual "while Wild Shape is active" notes when off).

### Forms

`FormDef` gains an optional shape:

```go
Shape *ShapeDef `json:"shape,omitempty"` // this form substitutes a creature's stat block

type ShapeDef struct {
    CreatureTag string       `json:"creature_tag"` // "wild_shape": creatures this form can become
    ClassID     string       `json:"class_id"`     // "druid": whose level the limits read
    Limits      []ShapeLimit `json:"limits"`       // highest MinLevel at or below the class level applies
    TempHP      Formula      `json:"temp_hp"`      // granted on start; shape.temp_hp set replaces it
}

type ShapeLimit struct {
    MinLevel int    `json:"min_level"`
    Known    int    `json:"known"`  // Known Forms count
    MaxCR    string `json:"max_cr"` // a CR string
    Fly      bool   `json:"fly"`    // forms with a fly speed allowed
}
```

Loader: a shape needs at least one limit, a known `class_id`, valid `max_cr` strings, and at least one
creature with its tag. A shape form needs a resource unless `no_cost`, as other forms do.

The effective limit for a character is the applicable `ShapeLimit`, with `MaxCR` raised to the highest
active `shape.max_cr` modifier value if that is higher.

### Known Forms

A shape synthesises one choice on its declaring feature:

- id `known_forms`, kind `pick`, count = the applicable limit's `Known`,
- pool: creatures carrying `CreatureTag`.

It is stored as an ordinary `Selection` (`feature_id` = the Wild Shape feature instance, `choice_id` =
`known_forms`) and appears in `pending_choices` while fewer than `Known` are picked. The existing choice
validation gives `not_in_pool` and `over_count`. A picked creature that is over the max CR or flies when
`Fly` is false is not a choice warning; it shows as ineligible in `FormView.KnownForms`.

`PoolFilter.Type` gains `creature` (pool = library creatures with `Tag`), used by this synthesised choice.

### Stored play state

```go
type ActiveForm struct {
    ID         string              `json:"id"`
    Picks      map[string][]string `json:"picks,omitempty"`
    CreatureID string              `json:"creature_id,omitempty"` // shape forms only
}
```

### Sheet

```go
Shape *ShapeView `json:"shape,omitempty"` // set while a shape form is active

type ShapeView struct {
    FormID     string  `json:"form_id"`
    CreatureID string  `json:"creature_id"`
    Name       string  `json:"name"`
    Size       string  `json:"size"`
    CR         string  `json:"cr"`
    Traits     []Trait `json:"traits"`
    CanCast    bool    `json:"can_cast"` // true only with an active shape.cast_spells
}
```

`FormView` gains, for shape forms only:

```go
MaxCR      string          `json:"max_cr,omitempty"`
KnownForms []ShapeOption   `json:"known_forms,omitempty"`

type ShapeOption struct {
    ID       string `json:"id"`
    Name     string `json:"name"`
    CR       string `json:"cr"`
    Eligible bool   `json:"eligible"`
    Reason   string `json:"reason,omitempty"` // "CR 2 is above your max of 1", "has a fly speed before level 8"
}
```

## Compute

The pipeline is unchanged up to the finished sheet: Wild Shape's `form` modifiers are active while it is
on. Then, if an active form has a `shape`, `applyShape` rewrites the sheet. The rule throughout:
**the beast replaces the base numbers; the character's modifiers still apply on top.**

| Part | While shaped |
|---|---|
| Str, Dex, Con | The beast's scores and modifiers. Int, Wis, Cha stay. |
| Saves | Per ability: own = new ability mod + the character's PB if the character is proficient; block = beast mod + beast PB if the beast lists it in `saves`. Take the higher, then add the character's save modifiers. |
| Skills | Same: own (new ability mod + the character's proficiency at its level) vs the stat block's bonus; higher wins, then modifiers. A skill only the beast has is added. |
| Initiative | Recomputed from the beast's Dex, plus initiative modifiers. |
| AC | The beast's AC is an option ("<Beast> natural armour"). While shaped, only it and AC formulas with `form` equal to the active form are eligible; the best wins. Armour and shield items don't count. Flat `ac` modifiers still apply. |
| HP | Max HP unchanged. |
| Speeds | The beast's speeds replace the character's, then `speed.*` modifiers apply. |
| Senses | Union of the beast's and the character's, the longer range of each. |
| Resistances, immunities | Union of the character's and the beast's. |
| Attacks | Weapon and unarmed attacks are replaced by the beast's attacks, evaluated with the beast's modifiers and PB as pets are. Attack and damage modifiers and dice riders still apply. `strength_attack` matches a beast attack whose to-hit formula uses `mod.str`. |
| Actions | The beast's actions are added after the character's. |
| Features, languages, resources, slots, spellcasting | Unchanged. |
| Creature type | The character's (not shown in `ShapeView`). |

`Sheet.Shape` is filled in, and the shape form's `FormView` gets `MaxCR` and `KnownForms`.

Warnings (permissive):

- `shape_unknown_creature`: the active form's creature isn't in the library. No overlay; the form's
  modifiers still apply.
- `shape_not_known`: the active creature isn't one of the Known Forms. Overlay still applies.
- `shape_ineligible`: the active creature is over the max CR or flies when not allowed. Overlay still applies.

Pets keep reading the owner's finished sheet, which is the shaped sheet while shaped. The owner numbers
companions read (spell attack, spell save DC, level) don't change.

## API

`POST /api/characters/{id}/forms`:

```go
type FormStart struct {
    ID         string              `json:"id"`
    Picks      map[string][]string `json:"picks,omitempty"`
    Free       bool                `json:"free,omitempty"`
    CreatureID string              `json:"creature_id,omitempty"`
}
```

- Shape form without `creature_id`, or `creature_id` on a non-shape form: 400.
- `creature_id` not among the Known Forms, or not eligible now: 400.
- Starting a shape form that is already active changes the beast ("until you use Wild Shape again") instead
  of 400. It spends a use unless `free`, and re-applies temp HP. Non-shape forms keep the 400.
- Cost: as other forms; 409 `no_uses`.
- Temp HP on start: `temp_hp = max(temp_hp, shape temp HP)`, where shape temp HP is the highest active
  `shape.temp_hp` value, else `ShapeDef.TempHP`. `shape.temp_hp` modifiers are read as active for this
  calculation, since the form is starting.
- End: as today. Temp HP is kept.
- Known Forms are set through the existing selections path.
- Long rest clears active forms, as today. A short rest regains one Wild Shape use
  (`short_rest_regain: 1`) and leaves the form active.

`apidocs.go` and `openapi.json` document `creature_id`, `Sheet.Shape`, `FormView.max_cr`,
`FormView.known_forms`, `Creature.cr`, `value_formula`, `ACFormula.form` and the new warnings.

## Content

### Druid class (`content/classes/classes/druid.json`)

- Hit die 8. Saves Int, Wis. Simple weapons; light armour and shields; Herbalism Kit.
- Starting choice: two skills from Arcana, Animal Handling, Insight, Medicine, Nature, Perception,
  Religion, Survival.
- Multiclass prereq Wisdom 13; multiclass grants the hit die and light armour and shields.
- Starting equipment as a feature, following the other classes: (A) Leather Armor, Shield, Sickle,
  Quarterstaff (Druidic Focus), Explorer's Pack, Herbalism Kit, 9 GP; or (B) 50 GP.
- Subclass level 3.

### Druid features (`content/classes/features/druid.json`)

- **Spellcasting (1):** Wisdom, prepared, full caster, Druid list. Cantrips 2 (1), 3 (4), 4 (10).
  Prepared: 4, 5, 6, 7, 9, 10, 11, 12, 14, 15, 16, 16, 17, 17, 18, 18, 19, 20, 21, 22.
- **Druidic (1):** grants the Druidic language; Speak with Animals always prepared.
- **Primal Order (1):** choice. Magician: one extra Druid cantrip; `skill.arcana` and `skill.nature` add
  Wis mod, min 1. Warden: martial weapon proficiency and medium armour training.
- **Wild Shape (2):** `form: {id: "wild_shape", name: "Wild Shape", shape: ...}`. Resource uses 2 (2),
  3 (6), 4 (17), `recharge: long_rest`, `short_rest_regain: 1`. Limits: level 2 known 4 max "1/4" no fly;
  level 4 known 6 "1/2" no fly; level 8 known 8 "1" fly. Temp HP `class_level.druid`.
- **Wild Companion (2):** Magic action described; pet source for Find Familiar forms (Fey, ends on a Long Rest).
- **ASI (4, 8, 12, 16), Epic Boon (19):** as other classes.
- **Wild Resurgence (5):** described free actions.
- **Elemental Fury (7) / Improved Elemental Fury (15):** choice. Potent Spellcasting: notes (+Wis to
  Druid cantrip damage; at 15, +300 ft range). Primal Strike: damage dice rider 1d8 (2d8 at 15),
  condition "once per turn; Cold, Fire, Lightning or Thunder".
- **Beast Spells (18):** `shape.cast_spells` set 1, `form: "wild_shape"`, with a note on the
  costly-component exception.
- **Archdruid (20):** Evergreen Wild Shape and Nature Magician as described actions; Longevity note.

### Circle of the Moon

- **Circle Forms (3):** `shape.max_cr` min `class_level.druid div 3`; `shape.temp_hp` set
  `class_level.druid mul 3`; AC formula "Circle Forms" 13 + `mod.wis` with `form: "wild_shape"`.
- **Circle of the Moon Spells (3):** `granted_spells` Cure Wounds, Moonbeam, Starry Wisp (3); Conjure
  Animals (5); Fount of Moonlight (7); Mass Cure Wounds (9). Note with `form: "wild_shape"`: these can be
  cast in Wild Shape.
- **Improved Circle Forms (6):** Lunar Radiance note; `save.con` add Wis mod, both `form: "wild_shape"`.
- **Moonlight Step (10):** Bonus Action; resource max 1 with `max_ability: wis`, long rest.
- **Lunar Form (14):** Improved Lunar Radiance damage dice rider 2d10 radiant, condition "once per turn",
  `form: "wild_shape"`; Shared Moonlight note.

### Spells

`spell_lists/druid.json` becomes the full 2024 Druid list. Spells missing from the library are added in
`content/classes/spells/druid_spells.json`. Cure Wounds (today only in the `example` pack) gets an entry
in the classes pack. The plan counts the missing spells exactly.

### Beasts

About 30 Wild Shape beasts in `content/classes/creatures/beasts.json`, each with a CR and the
`wild_shape` tag, covering CR 0-6 with walkers, swimmers and fliers: Rat, Riding Horse, Spider, Wolf,
Giant Badger, Panther, Brown Bear, Dire Wolf, Giant Spider, Giant Eagle, Ape, Crocodile, Polar Bear,
Saber-Toothed Tiger, Giant Constrictor Snake, Killer Whale, Giant Scorpion, Elephant, Giant Crocodile,
Giant Shark, Mammoth and similar. Existing familiar creatures that are Beasts get the `wild_shape` tag
and a CR rather than being duplicated.

## Testing

- `shape_test.go`: each row of the compute table against an in-memory library; the three warnings;
  Circle Forms AC beating a low beast AC but not a high one.
- `druid_test.go`: slots, cantrips, prepared counts and Wild Shape uses at levels 1, 2, 5, 8, 17, 20;
  both Primal Order picks; the Known Forms pending choice and `known_forms` eligibility by CR and fly.
- `druid_test.go` (Moon): temp HP 3 × level, max CR at levels 3 and 9, Increased Toughness, granted spells.
- `actions_test.go`: start with and without `creature_id`, unknown or ineligible beast, swapping beasts,
  temp HP not stacking, short rest regaining one use.
- `api_test.go`: the route with `creature_id`, 400 on an ineligible beast, `Sheet.Shape` in the response.
- `loader_test.go`: bad `cr`, a `wild_shape` creature without a CR, a shape with no limits,
  `value_formula` with an unknown term.
