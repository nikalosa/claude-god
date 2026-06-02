# Steering config

The checked-in artifact that drives the **Generator** (ADR-0004), committed to the
**Before** branch beside the frozen **Corpus** at `.validator/steering.yaml`. It makes
generation reproducible and turns regeneration into a reviewed, additive diff.

## Schema

```yaml
# .validator/steering.yaml
before: validator/before          # the ref to generate from and freeze onto

sources:                          # globs of hand-selected source text, resolved on `before`.
  - CLAUDE.md                     #   The ONLY doc grounding for rule-based probes — the
  - .claude/rules/*.md            #   Closed-book check strips exactly these files. Curate
  - docs/conventions/*.md         #   them; the Generator never scrapes the Environment blind.

pasted: |                         # optional: a Rule not in any file (CONTEXT.md sanctions
  Amounts cross the gRPC          #   pasted text). Treated as a source doc by the check.
  boundary as minor-unit strings.

emphasis:                         # free-text steer: what to probe hard, what to ignore.
  - Money is always string-typed — probe it hard.
  - Skip CHANGELOG and LICENSE.

severities:                       # priors applied while drafting. The Generator proposes a
  monetary_as_string: critical    #   Severity for anything unlisted, and EVERY severity is
  migration_script: high          #   dev-confirmed before freeze (critical sets the gate bit).
```

## Notes

- `sources` is the curated input — hand-selected, never the whole Environment. The
  Closed-book check removes exactly these files to prove an answer is doc-borne.
- `severities` keys are topics/rule-ids used as priors, not authority. The dev confirms
  each proposed **Severity** at review (CONTEXT.md: proposed → dev-confirmed).
- **memory** is excluded from Generator input. If a Target ever encodes a Rule in project
  memory, lift it into `pasted` or a doc first — don't read memory.
