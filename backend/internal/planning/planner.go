package planning

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type PromptPlanner interface {
	Plan(context.Context, PlannerRequest) (PlannerResponse, error)
	Health(context.Context) error
	Metadata() PlannerMetadata
}

type PlannerMetadata struct {
	Enabled  bool
	Runtime  string
	Endpoint string
	Provider string
	Model    string
	Agent    string
}

type PlannerError struct {
	Category string
	Message  string
}

func (e *PlannerError) Error() string { return e.Message }

func errorCategory(err error) string {
	if err == nil {
		return ""
	}
	var plannerError *PlannerError
	if errors.As(err, &plannerError) {
		return plannerError.Category
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return "unavailable"
}

type OpenCodeConfig struct {
	Enabled         bool
	Endpoint        string
	Provider        string
	Model           string
	Agent           string
	Username        string
	Password        string
	Timeout         time.Duration
	MaxRequestBytes int64
}

type OpenCodePlanner struct {
	config OpenCodeConfig
	client *http.Client
}

func NewOpenCodePlanner(config OpenCodeConfig) (*OpenCodePlanner, error) {
	config.Endpoint = strings.TrimRight(strings.TrimSpace(config.Endpoint), "/")
	config.Provider = strings.TrimSpace(config.Provider)
	config.Model = strings.TrimSpace(config.Model)
	config.Agent = strings.TrimSpace(config.Agent)
	config.Username = strings.TrimSpace(config.Username)
	if config.Username == "" {
		config.Username = "opencode"
	}
	if config.Timeout <= 0 {
		config.Timeout = 45 * time.Second
	}
	if config.MaxRequestBytes <= 0 {
		config.MaxRequestBytes = 256 << 10
	}
	if config.Enabled {
		if config.Endpoint == "" || config.Provider == "" || config.Model == "" || config.Agent == "" {
			return nil, fmt.Errorf("enabled OpenCode planning requires endpoint, provider, model and agent")
		}
		parsed, err := url.Parse(config.Endpoint)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return nil, fmt.Errorf("OpenCode endpoint must be an absolute http(s) URL")
		}
		if parsed.User != nil {
			return nil, fmt.Errorf("OpenCode endpoint must not include credentials")
		}
	}
	return &OpenCodePlanner{
		config: config,
		client: &http.Client{
			Timeout:       config.Timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func (p *OpenCodePlanner) Metadata() PlannerMetadata {
	return PlannerMetadata{
		Enabled: p.config.Enabled, Runtime: "opencode", Endpoint: p.config.Endpoint,
		Provider: p.config.Provider, Model: p.config.Model, Agent: p.config.Agent,
	}
}

func (p *OpenCodePlanner) Health(ctx context.Context) error {
	if !p.config.Enabled {
		return &PlannerError{Category: "disabled", Message: "OpenCode planning is disabled"}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.config.Endpoint+"/global/health", nil)
	if err != nil {
		return &PlannerError{Category: "configuration", Message: "build OpenCode health request"}
	}
	p.authorize(request)
	response, err := p.client.Do(request)
	if err != nil {
		return classifyTransportError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return &PlannerError{Category: "unavailable", Message: fmt.Sprintf("OpenCode health returned HTTP %d", response.StatusCode)}
	}
	var health struct {
		Healthy bool `json:"healthy"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&health); err != nil {
		return &PlannerError{Category: "invalid_response", Message: "decode OpenCode health response"}
	}
	if !health.Healthy {
		return &PlannerError{Category: "unavailable", Message: "OpenCode reported unhealthy status"}
	}
	return nil
}

func (p *OpenCodePlanner) Plan(ctx context.Context, request PlannerRequest) (PlannerResponse, error) {
	if !p.config.Enabled {
		return PlannerResponse{}, &PlannerError{Category: "disabled", Message: "OpenCode planning is disabled"}
	}
	prompt := plannerInstruction(request)
	if int64(len(prompt)) > p.config.MaxRequestBytes {
		return PlannerResponse{}, &PlannerError{Category: "budget_rejected", Message: "OpenCode planning request exceeds the configured request budget"}
	}

	sessionID, err := p.createSession(ctx)
	if err != nil {
		return PlannerResponse{}, err
	}
	defer p.deleteSession(sessionID)

	body := map[string]any{
		"agent": p.config.Agent,
		"model": map[string]string{"providerID": p.config.Provider, "modelID": p.config.Model},
		"parts": []map[string]string{{"type": "text", "text": prompt}},
		"tools": map[string]bool{
			"bash": false, "edit": false, "write": false, "read": false,
			"glob": false, "grep": false, "list": false, "webfetch": false,
			"websearch": false, "task": false,
		},
	}
	var messageResponse struct {
		Info struct {
			ProviderID string  `json:"providerID"`
			ModelID    string  `json:"modelID"`
			Cost       float64 `json:"cost"`
			Tokens     struct {
				Input     int64 `json:"input"`
				Output    int64 `json:"output"`
				Reasoning int64 `json:"reasoning"`
				Cache     struct {
					Read  int64 `json:"read"`
					Write int64 `json:"write"`
				} `json:"cache"`
			} `json:"tokens"`
		} `json:"info"`
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	if err := p.doJSON(ctx, http.MethodPost, p.config.Endpoint+"/session/"+url.PathEscape(sessionID)+"/message", body, &messageResponse); err != nil {
		return PlannerResponse{}, err
	}
	var output strings.Builder
	for _, part := range messageResponse.Parts {
		if part.Type == "text" {
			output.WriteString(part.Text)
		}
	}
	if strings.TrimSpace(output.String()) == "" {
		return PlannerResponse{}, &PlannerError{Category: "invalid_response", Message: "OpenCode returned no text result"}
	}
	provider := messageResponse.Info.ProviderID
	if provider == "" {
		provider = p.config.Provider
	}
	model := messageResponse.Info.ModelID
	if model == "" {
		model = p.config.Model
	}
	return PlannerResponse{
		Output: output.String(), Provider: provider, Model: model,
		Usage: Usage{
			InputTokens: messageResponse.Info.Tokens.Input, OutputTokens: messageResponse.Info.Tokens.Output,
			ReasoningTokens: messageResponse.Info.Tokens.Reasoning,
			CacheReadTokens: messageResponse.Info.Tokens.Cache.Read, CacheWriteTokens: messageResponse.Info.Tokens.Cache.Write,
			CostMicros: int64(messageResponse.Info.Cost * 1_000_000),
		},
	}, nil
}

func (p *OpenCodePlanner) createSession(ctx context.Context) (string, error) {
	var response struct {
		ID string `json:"id"`
	}
	if err := p.doJSON(ctx, http.MethodPost, p.config.Endpoint+"/session", map[string]string{"title": "cddm-prompt-planner"}, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.ID) == "" {
		return "", &PlannerError{Category: "invalid_response", Message: "OpenCode session response is missing id"}
	}
	return response.ID, nil
}

func (p *OpenCodePlanner) deleteSession(sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, p.config.Endpoint+"/session/"+url.PathEscape(sessionID), nil)
	if err != nil {
		return
	}
	p.authorize(request)
	response, err := p.client.Do(request)
	if err == nil {
		response.Body.Close()
	}
}

func (p *OpenCodePlanner) doJSON(ctx context.Context, method, endpoint string, body any, destination any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return &PlannerError{Category: "configuration", Message: "encode OpenCode request"}
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return &PlannerError{Category: "configuration", Message: "build OpenCode request"}
	}
	request.Header.Set("Content-Type", "application/json")
	p.authorize(request)
	response, err := p.client.Do(request)
	if err != nil {
		return classifyTransportError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		category := "unavailable"
		if response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusGatewayTimeout {
			category = "timeout"
		}
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusPaymentRequired {
			category = "budget_rejected"
		}
		return &PlannerError{Category: category, Message: fmt.Sprintf("OpenCode returned HTTP %d", response.StatusCode)}
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2<<20))
	if err := decoder.Decode(destination); err != nil {
		return &PlannerError{Category: "invalid_response", Message: "decode OpenCode response"}
	}
	return nil
}

func (p *OpenCodePlanner) authorize(request *http.Request) {
	if p.config.Password != "" {
		request.SetBasicAuth(p.config.Username, p.config.Password)
	}
}

func classifyTransportError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &PlannerError{Category: "timeout", Message: "OpenCode request timed out"}
	}
	if errors.Is(err, context.Canceled) {
		return &PlannerError{Category: "cancelled", Message: "OpenCode request cancelled"}
	}
	return &PlannerError{Category: "unavailable", Message: "OpenCode server is unavailable"}
}

func plannerInstruction(request PlannerRequest) string {
	var builder strings.Builder
	builder.WriteString("You are the restricted CDDM prompt-planner. Return exactly one JSON object and no prose, Markdown fence, commentary or tool call. Stage 3 route fields are immutable. Compose a safe worker prompt from the supplied PromptContext only. Do not claim events or evidence that are absent. Unknown optional fields may be added only under extensions and must not contain secrets.\n\n")
	builder.WriteString("Required PromptPlan fields: v, action, target_role, lane_key, summary, reason, risk, requires_owner, expected_head, expected_event, guards, prohibited_actions, prompt, confidence, source. source must contain kind=opencode, runtime=opencode, mode=opencode, and the supplied context_hash.\n")
	builder.WriteString("The prompt must contain named sections for current objective, authoritative state, required next action, scope and constraints, prohibited actions, required evidence, stop conditions, Initiative Clause, and terminal worker_result. A QA route also requires a QA Verdict Contract. Owner-attention and no-op routes must not invent a worker-chat prompt.\n")
	if request.Attempt > 0 {
		encoded, _ := json.Marshal(request.Violations)
		builder.WriteString("This is the only repair attempt. Correct exactly these machine-readable violations: ")
		builder.Write(encoded)
		builder.WriteString("\n")
	}
	builder.WriteString("PromptContext JSON:\n")
	builder.Write(request.ContextJSON)
	return builder.String()
}
