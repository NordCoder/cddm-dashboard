Use `$cddm-fix-ci` for Issue #{{ISSUE}}.

Use only the exact Issue/PR/Candidate/CI evidence supplied below plus local repository evidence. Classify the failure before editing and inspect the bounded failing diagnostics first.

Reproduce locally when practical without network access. Change code only for an evidenced implementation defect and run focused local checks after correction. Do not stage, commit, push, use GitHub, or write delivery state; host-side orchestration rechecks the original Candidate, runs authoritative full V2, and publishes a corrected Candidate only for `STATUS: FIXED`.

Final response MUST be exactly the `$cddm-fix-ci` result schema with no prose or code fence.