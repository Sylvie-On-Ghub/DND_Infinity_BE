# Adding a race (instructions for an agent)

You are adding a playable **race** to a D&D-style character backend written in Go. Races are **data, not code**:
a race is a JSON file in a content pack, and the rules engine reads it. In almost every case you should
**write JSON only and change no Go code**. A worked example, **Emberkin**, is in `content/races-example/`; read it
before you start, and copy its shape.

## What you will deliver

1. A **content pack folder** containing your race(s) (and any spells they need).
2. The output of the **content checker** showing `0 error(s)`.
3. A **Go test** for each race (copy `emberkin_test.go`), if you can run Go.
4. A short **report**: files created, how to load them, the checker output, and anything in the source rules
   that the engine **cannot** express (see "Limits").

Do not edit `content/example/`. Do not edit existing Go files unless a rule you need genuinely cannot be
expressed; in that case stop, describe the gap in your report, and write the closest description-only trait.

## How the project works (what you need to know)

- A **content pack** is a folder. Packs load in order, and a later pack can add to (or, with a repeated id,
  replace) an earlier one. Your pack must load **after** `content/example`, which defines the skills.
- Pack layout (only the folders you use are needed):

  ```
  content/<your-pack>/
    pack.json              {"id": "...", "name": "...", "version": "0.1.0"}   (optional but recommended)
    features/              races, racial traits, feats, backgrounds ...   (any depth, .json files)
    spells/                spells your race grants
    spell_lists/ items/ classes/   (not usually needed for a race)
  ```

  A `.json` file may hold **one object** or an **array** of objects. A file in any other folder is ignored
  with a warning. JSON is parsed **strictly**: an unknown key is an error, so a misspelt field fails loudly.
- **A race is a feature** with `"origin": "race"` and the tag `"race"`. Each racial trait is **another feature**
  that the race grants. The tag is what makes it appear in `GET /api/content/races` and count as a valid
  `race_id`; without it the character gets a `wrong_kind` warning.
- A character chooses a race by setting `race_id` to the race feature's id. Anything the race needs the player to
  decide (a lineage, a skill) becomes a **pending choice** on the character's sheet
  until they pick.

## The rule about ability scores

**Races do not change ability scores. Backgrounds do.** If the source material gives a race ability increases
(for example "+2 Constitution"), leave them out of the race and mention in your report that they belong on a
background. The existing backgrounds (`content/example/features/backgrounds.json`) show how a background grants
its increase: an `allocate` choice of 3 points across its three abilities. The loader prints a warning if a race, one
of its tiers, or one of its choice options modifies an `ability.*` target.

## Step by step

### 1. Decide what the race gives, and map each rule to a mechanism

Use the "Rule to JSON" section below. For each rule in the source, pick one of: a **modifier**, a **grant**, a
**choice**, an **action**, a **resource**, an **innate spell**, or a **description-only note**. Write down any
rule that fits none of them; those go in your report under "Limits".

### 2. Create the pack

Create `content/<pack-name>/pack.json` and `content/<pack-name>/features/<race>.json`. Use unique,
lowercase `snake_case` ids. Prefix every trait id with the race id (`emberkin_fire_resistance`) so ids never
collide with existing ones. Ids must not contain `@`. If an id repeats an existing one, your entry silently
**replaces** it (reported as a warning), so only do that on purpose.

### 3. Write the race feature

```json
{
  "id": "my_race",
  "name": "My Race",
  "origin": "race",
  "tags": ["race"],
  "description": "One or two sentences, in your own words.",
  "modifiers": [
    {"target": "speed.walk",  "kind": "add", "value": 30},
    {"target": "sense.darkvision", "kind": "add", "value": 60}
  ],
  "grants": {
    "languages": ["common"],
    "feature_ids": ["my_race_trait_one", "my_race_trait_two"]
  },
  "choices": []
}
```

Put the always-on numbers (base speed, senses) on the race feature itself. **Do not put ability score increases on a race** (see the next rule). Put each distinct
named trait in its own feature and list its id in `grants.feature_ids`.

### 4. Write each trait as its own feature

```json
{
  "id": "my_race_trait_one",
  "name": "Trait Name",
  "origin": "race",
  "source_id": "my_race",
  "description": "What it does, in your own words."
}
```

Add `modifiers`, `grants`, `actions`, `resource`, `spells`, `choices`, `tiers` or `ac_formulas` as the trait
needs (see the reference below). Do not put `tags` on traits.

### 5. Add any spells

Innate spells must exist. Put them in `content/<pack>/spells/<race>_spells.json` (see Emberkin's `kindle_flame`).
A spell needs `id`, `name`, `level` (0 = cantrip), and text fields; add `attack: true` or `save: "<ability>"` and
`effects` if it deals damage or heals. Spell dice must be plain `NdM` (for example `1d8`); put flat bonuses in
the effect's `note`.

### 6. Validate

From the project root (the folder containing `go.mod`):

```
go run ./cmd/contentcheck content/example content/<pack-name>
```

Load **`content/example` first**, then your pack. Fix every `error:` line. Read every `warning:`; the expected
ones are none, or an override you intended. The last line must say `0 error(s)`.

### 7. Test it

Copy `emberkin_test.go` to `<race>_test.go`, rename the helpers and ids, and assert what your race should put on
the sheet. Run `go test ./...`. The helpers you can use are already defined in the package's test files:
`loadExample`, `mustLoad`, `memPack`, `mustCompute`, `findPending`, `findSource`, `findSpell`, `realPending`,
`noContentWarnings`, `hasWarning`, `hasWarningValue`, `dumpIssues`, and the engine's own `ChoiceOptions`.

### 8. Try it through the API (optional)

```
go run ./cmd/dndserver -content content/example,content/<pack-name>
curl localhost:8080/api/content/races
curl -X POST localhost:8080/api/characters -d '{"id":"t1","name":"Test","race_id":"my_race"}'
curl localhost:8080/api/characters/t1/sheet
```

Look at `pending_choices` for anything the player must decide, and at `senses`, `speeds`, `resistances`,
`languages`, `spellcasting`, `resources`, `actions` and `abilities` for what the race granted.

## Rule to JSON

All of these go on the race feature or on a trait feature.

| The rule says | Write this |
|---|---|
| +N to an ability score | **Not on a race.** Ability increases come from the background (see "The rule about ability scores") |
| Walking, flying, swimming, climbing or burrowing speed | `{"target":"speed.walk","kind":"add","value":30}` (also `fly`, `swim`, `climb`, `burrow`) |
| Darkvision, blindsight and so on | `{"target":"sense.darkvision","kind":"add","value":60}` (the name after `sense.` is free text: `sense.tremorsense`) |
| Damage resistance / immunity / vulnerability | `{"target":"resist.fire","kind":"add","value":1}` (`immune.<type>`, `vuln.<type>`; damage types are free text, lowercase) |
| Bonus to a save, check, skill, AC, initiative or HP maximum | `{"target":"save.wis","kind":"add","value":1}`; other targets: `save.all`, `check.<ability>`, `check.all`, `skill.<id>`, `passive.<skill id>`, `ac`, `initiative`, `hp.max` |
| Advantage or disadvantage | `{"target":"save.wis","kind":"advantage"}`. Add `"condition":"against being charmed"` if it is situational; a conditional modifier appears as a note and changes no number |
| Attack or damage bonuses | targets `attack.all`, `attack.melee`, `attack.ranged`, `damage.all`, `damage.melee`, `damage.ranged`, `damage.spell`; an extra-dice bonus uses `"dice":"1d6"` with kind `add` |
| A reminder with no maths | `{"target":"check.all","kind":"note","note":"Text shown on the sheet"}` |
| Carrying capacity doubled | grant the existing feature `powerful_build`, or `{"target":"carry_capacity","kind":"multiply","value":2}` |
| Fixed skill proficiency | `"grants":{"skills":{"perception":"trained"}}` (levels: `half`, `trained`, `expert`) |
| Choose a skill (or several) | a choice: `{"id":"skill","prompt":"Choose one skill","kind":"pick","count":1,"filter":{"type":"skill","allowed":["arcana","history"]}}`. Omit `allowed` to allow any skill. |
| Tool, weapon or armour proficiency | `"grants":{"tools":{"smiths_tools":"trained"},"weapon_profs":["longsword","martial"],"armour_profs":["light"]}` (weapons match a category such as `simple` / `martial`, or a specific item id) |
| Fixed languages | `"grants":{"languages":["common","elvish"]}` |
| Sub-race or lineage | a choice on the race feature with `options`, each granting a trait: `{"id":"lineage","kind":"pick","count":1,"options":[{"id":"cinder","name":"Cinder","grants":{"feature_ids":["my_race_cinder"]}}]}` |
| Natural armour | on a trait: `"ac_formulas":[{"id":"my_race_hide","name":"Natural Armour","no_armour":true,"formula":{"base":13,"terms":[{"source":"mod.dex"}]}}]`. The best eligible AC formula is used. |
| Natural weapon (claws, bite) | an action with attack details: `"actions":[{"name":"Claws","type":"action","is_attack":true,"attack":{"damage_dice":"1d6","damage_type":"slashing","range_normal":5,"properties":["finesse"]}}]` |
| Innate spell, at will | `"spells":{"spell_ids":["some_spell"],"ability":"cha"}`. `ability` is the casting ability for its save DC and attack bonus. |
| Innate spell, limited uses | the same, plus `"resource":{"name":"Once per day","max":1,"recharge":"long_rest"}` |
| A limited-use ability (breath weapon) | `"resource":{"name":"Breath","max":1,"recharge":"short_rest"}` plus an `actions` entry describing it. Recharge is `short_rest`, `long_rest`, `dawn` or `none`. |
| A trait that gets stronger with level | `"tiers":[{"min_level":1,"modifiers":[...]},{"min_level":6,"modifiers":[...]}]`. A tier can carry `modifiers`, `actions`, `grants`, a `resource` and `spells`. For a race the level used is the character's total level (for a class feature, the level in that class). Only the highest tier at or below the level applies; tiers replace each other. |
| Spells that unlock at a level | put a `spells` block inside a tier: `"tiers":[{"min_level":3,"spells":{"spell_ids":["spell_a"],"ability":"con"}},{"min_level":5,"spells":{"spell_ids":["spell_a","spell_b"],"ability":"con"}}]`. A tier's spells are added to the feature's own `spells` block, as one spell source. **A higher tier replaces the lower one, so list every tier spell that should still apply at that level.** A tier can leave out `ability` if the feature's own `spells` block names one. Add `"resource"` for limited uses; the uses are shared by every spell in that source. |
| Anything else (size, creature type, flavour) | put it in the feature's `description` |

Valid values at a glance:

- **Modifier kinds:** `add`, `set`, `min`, `multiply`, `advantage`, `disadvantage`, `note`. How a single target is worked out: `set` first, then all `add`s, then `multiply`, then `min` as a floor.
- **Action types:** `action`, `bonus_action`, `reaction`, `free`, `other`.
- **Skill ids** (from `content/example/skills.json`): `acrobatics animal_handling arcana athletics deception history insight intimidation investigation medicine nature perception performance persuasion religion sleight_of_hand stealth survival`.
- **Choice kinds:** `pick` (needs `count`) and `allocate` (needs `points`; options only, no filter). A choice has **either** `options` **or** `filter`, never both.

## Limits

State these plainly in your report instead of working around them with invented fields:

- **Modifiers are fixed numbers.** "Add your Constitution modifier to X" cannot be calculated. Use a `note` modifier plus the description.
- **No per-spell bonuses.** A modifier cannot target one named spell, only `damage.spell` (all spell damage).
- **Size and creature type are not modelled.** Put them in the description.
- **No choosing of tools or free languages by the player**, except by a `filter` of type `tool`/`language` with an `allowed` list.
- **No daily choices.** Anything that changes between rests (which resistance you picked today) is not modelled.
- Do not add JSON keys that do not exist. The loader rejects them.

## Common mistakes

| Symptom | Cause |
|---|---|
| Race missing from `/api/content/races` | forgot `"tags": ["race"]` |
| `wrong_kind` warning on a character | `race_id` names something without the `race` tag |
| `error: ... unknown field "x"` | a misspelt or invented key |
| `unknown skill "x"` / `unknown feature "x"` | typo, or the pack was not loaded after `content/example`, or a feature id is not defined anywhere |
| `overrides the feature from pack ...` | your id duplicates an existing one |
| `unknown target "x"` | a modifier target that is not in the lists above |
| warning: `ability score increases come from backgrounds` | the race (or one of its tiers or options) has an `ability.*` modifier; move that to a background |
| `dice "..." must look like "1d6"` | dice with a `+N` or wrong shape; use `NdM` only |
| Choice never appears as pending | `count` or `points` missing, or the trait that owns it is never granted (check `grants.feature_ids`) |
| Spell source missing from the sheet | the spell id is not defined, or the `spells` block has no `ability` |

## Writing rules

- **Write descriptions in your own words.** Do not paste rulebook text. Numbers and mechanics are fine; prose is not.
- Keep ids lowercase `snake_case`, prefixed with the race id.
- One race per file under `features/`, named after the race.
- Keep each description short. The engine does not read it; people do.

## Worked example: Emberkin

`content/races-example/` contains a complete race. In outline:

- `features/emberkin.json` holds the race feature (tagged `race`) and seven traits.
- The **race feature** carries the always-on numbers: 30 ft walking speed and darkvision 60. It changes no ability scores.
  It grants two languages and five traits, and has a `lineage` choice (Cinder or Ash), each option granting one more trait.
- **Fire Resistance** is a single `resist.fire` modifier.
- **Ember Breath** is a `resource` (1 use, short rest) plus an action describing the cone.
- **Kindle** is an innate cantrip: a `spells` block naming `kindle_flame` and Constitution as the casting ability.
- **Ember Magic** shows spells unlocking by level: a feature with no spells of its own whose tiers add `scorching_burst` at level 3 and both `scorching_burst` and `smoldering_veil` at level 5, sharing one use per long rest.
- **Forge Lore** is a skill choice limited to three skills.
- **Cinder Lineage** adds 2 to initiative. **Ash Lineage** adds 30 ft of darkvision (it stacks with the race's 60 because they come from different features).
- `spells/emberkin_spells.json` defines `kindle_flame`, with damage dice that scale at levels 5, 11 and 17.
- `emberkin_test.go` shows the matching test: that ability scores are untouched, speed, senses, resistance, languages, the breath weapon and its resource, the spell source and its DC, the two pending choices, and both lineages.

Load it with:

```
go run ./cmd/contentcheck content/example content/races-example
go run ./cmd/dndserver -content content/example,content/races-example
```

## Definition of done

- [ ] New pack folder under `content/`, nothing in `content/example/` changed
- [ ] Race feature has `"origin": "race"` and `"tags": ["race"]`
- [ ] No `ability.*` modifier on the race, its traits, tiers or choice options
- [ ] Every trait is its own feature, granted from the race, with a prefixed unique id
- [ ] Every spell, skill and feature id the race refers to exists
- [ ] `contentcheck` run with `content/example` first: `0 error(s)`, and no unintended warnings
- [ ] A test (or, if you cannot run Go, a written list of what you would assert) for the race
- [ ] Report lists files created, load command, checker output, and every rule the engine cannot express
- [ ] If you could not run any commands, say so explicitly rather than implying the pack was validated