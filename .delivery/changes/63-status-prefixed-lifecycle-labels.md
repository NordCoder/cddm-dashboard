# Change — Parse Status-Prefixed Lifecycle Labels

Milestone: M8 — Worker Session Provisioning
Issue: #63
Risk: Low
Authorized Base: `637a2a2fe25b755e6c5b244f4b535fe309201b6c`

## Outcome

Allow repository lifecycle labels written as `status:<status>` to derive the same Dashboard lifecycle and route as their existing bare aliases.

## Requirements

- Recognize the exact `status:` namespace case-insensitively after trimming surrounding whitespace.
- Pass the namespace value through the existing canonical alias mapping.
- Preserve bare, `status_`, `lifecycle_` and `stage_` label compatibility.
- Deduplicate semantically equivalent labels.
- Preserve existing ambiguity warnings for distinct canonical lifecycle values.
- Preserve the missing lifecycle warning for unknown `status:*` values.
- Do not mutate GitHub labels.

## Verification

- focused normalization tests for canonical lifecycle aliases;
- duplicate, conflict and unknown-value tests;
- Work Unit routing fixture using `status:ready`;
- full backend test suite and race detector;
- exact-Head CI.
