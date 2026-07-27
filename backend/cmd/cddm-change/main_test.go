package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestColorPolicy(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	var b bytes.Buffer
	always := newUI(ColorAlways)
	if got := always.style(&b, ansiGreen, "ok"); !strings.Contains(got, "\x1b[32m") {
		t.Fatalf("forced color missing ANSI: %q", got)
	}
	never := newUI(ColorNever)
	if got := never.style(&b, ansiGreen, "ok"); got != "ok" {
		t.Fatalf("color=never = %q", got)
	}
	t.Setenv("NO_COLOR", "1")
	auto := newUI(ColorAuto)
	if got := auto.style(os.Stdout, ansiGreen, "ok"); got != "ok" {
		t.Fatalf("NO_COLOR not respected in auto mode: %q", got)
	}
	if got := always.style(&b, ansiGreen, "ok"); got != "ok" {
		t.Fatalf("NO_COLOR must override forced color: %q", got)
	}
}

func TestRenderCommandEvents(t *testing.T) {
	started := parseRenderedEvent(`{"type":"item.started","item":{"type":"command_execution","command":"go test ./..."}}`)
	if started.Kind != "RUN" || started.Text != "go test ./..." {
		t.Fatalf("started = %#v", started)
	}
	done := parseRenderedEvent(`{"type":"item.completed","item":{"type":"command_execution","command":"go test ./...","exit_code":0}}`)
	if done.Kind != "DONE" || done.Success == nil || !*done.Success {
		t.Fatalf("done = %#v", done)
	}
	failed := parseRenderedEvent(`{"type":"item.completed","item":{"type":"command_execution","command":"go test ./...","exit_code":2}}`)
	if failed.Success == nil || *failed.Success {
		t.Fatalf("failed = %#v", failed)
	}
}

func TestRenderUnknownAndMalformedAreBounded(t *testing.T) {
	unknown := parseRenderedEvent(`{"type":"future.event","payload":{"secret":"must-not-dump"}}`)
	if unknown.Kind != "EVENT" || strings.Contains(unknown.Text, "must-not-dump") {
		t.Fatalf("unknown leaked payload: %#v", unknown)
	}
	malformed := parseRenderedEvent(`not-json`)
	if malformed.Kind != "EVENT" || !strings.Contains(malformed.Text, "malformed") {
		t.Fatalf("malformed = %#v", malformed)
	}
}

func TestRedaction(t *testing.T) {
	got := redact("token=supersecret Authorization: Bearer actual-token")
	if strings.Contains(got, "supersecret") || strings.Contains(got, "actual-token") || strings.Contains(strings.ToLower(got), "bearer") {
		t.Fatalf("redaction failed: %q", got)
	}
}

func TestRedactionTruncatesOnRuneBoundary(t *testing.T) {
	got := redact(strings.Repeat("я", maxEventText+20))
	if !utf8.ValidString(got) {
		t.Fatalf("truncation produced invalid UTF-8")
	}
	if utf8.RuneCountInString(got) != maxEventText {
		t.Fatalf("rune count = %d, want %d", utf8.RuneCountInString(got), maxEventText)
	}
}

func TestProxyRequiresIdentityBeforeCommandSeparator(t *testing.T) {
	_, _, _, _, _, err := parseProxyArgs([]string{"--", "codex", "exec"})
	if err == nil || !strings.Contains(err.Error(), "--repo and --issue") {
		t.Fatalf("err = %v", err)
	}
}

func TestHistoryFold(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/turns.jsonl"
	if err := appendHistory(path, map[string]any{"turn_key": "a", "phase": "start", "started_at": "2026-07-27T00:00:00Z", "mode": "resume", "model": "luna", "reasoning": "medium"}); err != nil {
		t.Fatal(err)
	}
	if err := appendHistory(path, map[string]any{"turn_key": "a", "phase": "finish", "ended_at": "2026-07-27T00:00:02Z", "rc": 0, "result_status": "CONTINUE", "usage": map[string]any{"input_tokens": 10, "cached_input_tokens": 4, "output_tokens": 2}}); err != nil {
		t.Fatal(err)
	}
	turns := readTurns(path)
	if len(turns) != 1 || turns[0].RC == nil || *turns[0].RC != 0 || turns[0].Usage.Input == nil || *turns[0].Usage.Input != 10 {
		t.Fatalf("turns = %#v", turns)
	}
	if turns[0].EndedAt.Sub(turns[0].StartedAt) != 2*time.Second {
		t.Fatalf("duration = %v", turns[0].EndedAt.Sub(turns[0].StartedAt))
	}
}

func TestUsageTakesMaximumObservedCounters(t *testing.T) {
	var usage Usage
	parseUsage(map[string]any{"usage": map[string]any{"input_tokens": float64(10), "cached_input_tokens": float64(3), "output_tokens": float64(2)}}, &usage)
	parseUsage(map[string]any{"usage": map[string]any{"input_tokens": float64(8), "cached_input_tokens": float64(5), "output_tokens": float64(4)}}, &usage)
	if *usage.Input != 10 || *usage.Cached != 5 || *usage.Output != 4 {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestPlainEventLineHasNoANSIWhenDisabled(t *testing.T) {
	ui := newUI(ColorNever)
	var b bytes.Buffer
	ok := true
	ui.printEvent(&b, 12*time.Second, renderedEvent{Kind: "DONE", Text: "go test · exit=0", Success: &ok})
	if strings.Contains(b.String(), "\x1b[") {
		t.Fatalf("plain output contains ANSI: %q", b.String())
	}
	if !strings.Contains(b.String(), "[00:12]") || !strings.Contains(b.String(), "DONE") {
		t.Fatalf("unexpected plain output: %q", b.String())
	}
}
