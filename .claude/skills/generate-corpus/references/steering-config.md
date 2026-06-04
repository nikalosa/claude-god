# Steering config

The checked-in artifact that drives corpus generation, committed to the **Before** branch
beside the frozen corpus at `.validator/steering.yaml`. It makes generation reproducible and
turns regeneration into a reviewed, additive diff.

## Schema

```yaml
# .validator/steering.yaml
before: validator/before          # the ref to generate from and freeze onto

sources:                          # globs of hand-selected source text, resolved on `before`.
  - CLAUDE.md                     #   The ONLY grounding for rule-based probes — each doc is
  - .claude/rules/*.md            #   fed to its own generation subagent (docs only, no code).
  - docs/conventions/*.md         #   Curate them; the Generator never scrapes blind.

pasted: |                         # optional: a rule not stated in any file. Treated as a
  Amounts cross the gRPC          #   source doc by the rule-based stream.
  boundary as minor-unit strings.

emphasis:                         # free-text steer: what to probe hard, what to ignore.
  - Money is always string-typed — probe it hard.
  - Skip CHANGELOG and LICENSE.

severities:                       # priors applied while drafting. The Generator proposes a
  monetary_as_string: critical    #   Severity for anything unlisted, and EVERY severity is
  migration_script: high          #   dev-confirmed before freeze (reading priority, not a gate).
```

## Notes

- `sources` is the curated input — hand-selected, never the whole Environment. Each matched
  doc is fed to its own rule-based generation subagent (docs only, no code in context).
- `severities` keys are topics/rule-ids used as priors, not authority. The dev confirms
  each proposed **Severity** at review. Severity is reading priority — the validator never
  gates.
- **memory** is excluded from Generator input. If a Target ever encodes a Rule in project
  memory, lift it into `pasted` or a doc first — don't read memory.
