package main

func effectiveExecutionOptions(opts globalOptions, profile profileConfig) executionOptions {
	return executionOptions{
		ProfileModel:     profile.Model,
		ProfileReasoning: profile.Reasoning,
		Model:            opts.Model,
		Reasoning:        opts.Reasoning,
	}
}

func (e *engine) resumeOrRotateWithOptions(mode, instruction string, legacy []string, opts executionOptions) int {
	model := opts.ProfileModel
	reasoning := opts.ProfileReasoning
	if len(legacy) >= 1 {
		model = legacy[0]
	}
	if len(legacy) >= 2 {
		reasoning = legacy[1]
	}
	if opts.Model != "" {
		model = opts.Model
	}
	if opts.Reasoning != "" {
		reasoning = opts.Reasoning
	}

	// The legacy resumeOrRotate API uses positional overrides where reasoning
	// cannot be supplied without model. Preserve state-aware fallback for a
	// reasoning-only profile/flag by supplying the persisted model explicitly.
	if reasoning != "" && model == "" {
		state, err := loadState(e.statePath)
		if err != nil {
			e.ui.errorf("no persistent session state for Issue #%d; use start", e.issue)
			return 1
		}
		model = state.Model
	}

	var overrides []string
	if model != "" {
		overrides = append(overrides, model)
	}
	if reasoning != "" {
		overrides = append(overrides, reasoning)
	}
	return e.resumeOrRotate(mode, instruction, overrides)
}
