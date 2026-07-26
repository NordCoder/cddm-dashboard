Use `$cddm-review` for PR #{{PR}}.

Establish the exact Base and current Candidate Head, then read the Active Milestone, canonical Change Contract, exact diff, relevant code/tests, and Candidate-bound CI evidence.

Review independently from the Implementor. Do not modify files, commits, branches, or PR state. Focus on requirement coverage, semantic correctness, regressions, failure paths, security, compatibility, persistence/concurrency, test quality, and scope leakage.

Any Head change invalidates the verdict. Persist exactly one compact REVIEW result to the PR for the exact reviewed Head.
