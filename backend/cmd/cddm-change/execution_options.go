package main

func resolveExecutionOptions(opts globalOptions, workspace workspaceConfig) executionOptions {
	return executionOptions{
		ProfileModel:     workspace.Model,
		ProfileReasoning: workspace.Reasoning,
		Model:            opts.Model,
		Reasoning:        opts.Reasoning,
		CodexProfile:     opts.CodexProfile,
	}
}

func (e *engine) resumeOrRotateWithOptions(mode, instruction string, legacy []string, opts executionOptions) int {
	state, err := loadState(e.statePath)
	if err != nil {
		e.ui.errorf("no persistent session state for Issue #%d; use start", e.issue)
		return 1
	}

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
	if reasoning != "" && model == "" {
		model = state.Model
	}

	profile := e.loadCodexProfile()
	if opts.CodexProfile != "" {
		profile = opts.CodexProfile
	}
	if profile != "" {
		if err := e.activateCodexProfile(profile); err != nil {
			e.ui.errorf("Codex profile: %v", err)
			return 1
		}
	}
	if err := e.persistCodexProfile(profile); err != nil {
		e.ui.errorf("persist Codex profile: %v", err)
		return 1
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
