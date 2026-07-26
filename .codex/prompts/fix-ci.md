Use `$cddm-fix-ci` for Issue #{{ISSUE}}.

Locate the current Change PR, exact Candidate Head, and failing/inconclusive required check with `gh`. Classify the failure before editing and inspect only the relevant logs first.

Reproduce locally when practical. Change code only for an evidenced implementation defect. Run required V1/V2 after correction, commit and push the new Candidate, and preserve exact-Head semantics.

Do not merge or broaden Scope. Persist exactly one compact FIX_CI result to the PR.
