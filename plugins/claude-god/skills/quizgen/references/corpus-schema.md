# Corpus schema

The benchmark loads `.benchmark/corpus/<name>.yaml`. Read an existing corpus before writing; match its shape.

```yaml
# Top level is ONLY `probes:`. The loader STRICT-decodes — any unknown field errors at load.
probes:
  # rule_based (the default — omit `kind:`)
  - id: monetary_amounts_string          # probe id, unique
    prompt: "What type must all money use?"
    rules:                               # >=1 rule required for rule_based
      - id: monetary_amounts_string      # rule id
        severity: critical               # SEVERITY LIVES ON THE RULE, not the probe
        checks:                          # each check = exactly ONE kind
          - text_matches: '(?i)\bstring\b'     # one RE2 regex (a string, not a list)
          - text_matches: 'NUMERIC\(38,19\)'   # multiple checks in a rule = AND

  # judge_rubric rule (prose graded by the Judge — needs --judge at run time)
      - id: migration_order
        severity: high
        checks:
          - judge_rubric:
              facts: ["run the migration script before deploy"]  # >=1 fact
              pass_score: 60             # 1..100

  # open_ended — prompt only, NO rules
  - id: event_bus_shape
    kind: open_ended
    prompt: "Explain the event bus end to end — producers, topics, consumers, ordering."

  # plan — prompt only, NO rules
  - id: wire_new_service
    kind: plan
    prompt: "Give me a step-by-step plan to wire a new microservice into this system."
```

## Rules the loader enforces

- `kind` ∈ `rule_based | open_ended | plan`; omitted → `rule_based`.
- `rule_based` needs ≥1 rule; `open_ended`/`plan` must have **no** rules.
- every probe and every rule needs an `id`.
- a check has **exactly one** kind (`text_matches` *or* `judge_rubric`).
- `judge_rubric` needs ≥1 fact and `pass_score` in 1..100.
- `text_matches` is a **single regex string**; the empty pattern is rejected.

## Two traps

1. **Go regexp is RE2 — no lookahead/backreference.** You cannot write "answer contains A *and* B" in one regex. Encode AND as **multiple `text_matches` checks in one rule**.
2. **Severity is rule-level only.** `open_ended`/`plan` have no rules, so they carry no severity — they are preference-graded, report-only.
