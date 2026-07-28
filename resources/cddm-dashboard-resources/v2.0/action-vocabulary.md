# CDDM Dashboard Typed Actions v2

Typed actions are closed routing requests. They never replace the target GitHub Issue contract and never carry arbitrary executable prompts.

## Common rules

- Every action has a unique `action_id` within one result.
- `repository` uses `owner/name`.
- Issue and PR numbers are positive integers.
- Candidate identities use full lowercase 40-character SHAs.
- Unknown fields, action types and role/action combinations are invalid.
- Dashboard revalidates current GitHub facts before materializing an action.
- M9 accepts and persists actions; M10 owns action materialization.

## Actions

### `dispatch`

Starts one bounded role command.

Required fields: `action_id`, `type`, `repository`, `issue`, `role`.

Allowed roles: `lead`, `implementor`, `qa`.

QA additionally requires `expected_head`.

### `correct`

Starts Implementor correction after bounded Lead correction authority exists in the Issue.

Required fields: `action_id`, `type`, `repository`, `issue`, `role=implementor`.

Optional Candidate correlation: `expected_previous_head`.

### `plan_next_wave`

Starts one Project Lead planning command through the Project Control Issue.

Required fields: `action_id`, `type`, `repository`, `issue`, `role=lead`.

The Issue must be the configured Control Issue.

### `merge_candidate`

Starts one Lead exact-Candidate merge command.

Required fields: `action_id`, `type`, `repository`, `issue`, `role=lead`, `pr`, `expected_head`.

### `hold`

Pauses one bounded scope.

Required fields: `action_id`, `type`, `repository`, `reason_code`.

Issue may be omitted only for a Project-level hold.

### `owner_required`

Requests one material Owner decision.

Required fields: `action_id`, `type`, `repository`, `reason_code`, `decision_category`.

Issue may be omitted only for a Project-level decision.

Allowed decision categories:

- `product_behavior`
- `scope`
- `architecture`
- `visual_acceptance`
- `security_privacy_legal`
- `release`
- `residual_risk`
- `product_envelope`

## Wave identity

An `actions_ready` result may include one Wave:

- `wave_id` is an opaque bounded identifier;
- `control_issue` is a positive integer;
- `issues` is a non-empty ordered list of unique positive Issue numbers;
- every Issue-targeted action in the result belongs to the same repository;
- each Wave member must have at least one `dispatch` action in the batch;
- actions for Issues outside the declared Wave are invalid when a Wave is present, except Project-level `hold`, `owner_required` and `plan_next_wave` actions.
