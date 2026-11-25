package engine_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

func chainHandler(name string, decision engine.DiscoveryDecision, value string, err error) engine.DiscoveryHandler[string] {
	return engine.DiscoveryHandler[string]{Name: name, Handle: func(context.Context) engine.DiscoveryStepResult[string] {
		return engine.DiscoveryStepResult[string]{Decision: decision, Value: value, Err: err}
	}}
}

func TestDiscoveryChainOrderAndPlanOwnership(t *testing.T) {
	var calls []string
	ctx := t.Context()
	step := func(name string, decision engine.DiscoveryDecision, value string, err error) engine.DiscoveryHandler[string] {
		h := chainHandler(name, decision, value, err)
		original := h.Handle
		h.Handle = func(received context.Context) engine.DiscoveryStepResult[string] {
			if received != ctx {
				t.Error("request context changed")
			}
			calls = append(calls, name)
			return original(received)
		}
		return h
	}
	result := engine.RunDiscoveryChain(ctx,
		step("embed", engine.DiscoveryNext, "partial-plan", engine.ErrInstagramPublicExtraction),
		step("public", engine.DiscoveryHandled, "direct-transfer-plan", nil),
		step("cli", engine.DiscoveryHandled, "cli-transfer-plan", nil),
	)
	if result.Err != nil || result.Decision != engine.DiscoveryHandled || result.Handler != "public" || result.Value != "direct-transfer-plan" ||
		!reflect.DeepEqual(calls, []string{"embed", "public"}) || len(result.Failures) != 1 || result.Failures[0].Handler != "embed" {
		t.Fatalf("wrong strategy or lost failure history: %+v, calls=%v", result, calls)
	}
}

func TestDiscoveryChainExhaustionAndTerminalClassification(t *testing.T) {
	first := chainHandler("embed", engine.DiscoveryNext, "", engine.ErrInstagramPublicExtraction)
	last := chainHandler("cli", engine.DiscoveryNext, "", engine.ErrAuthenticationRequired)
	result := engine.RunDiscoveryChain(t.Context(), first, last)
	var exhausted *engine.DiscoveryChainError
	if result.Decision != engine.DiscoveryNext || !errors.As(result.Err, &exhausted) || len(exhausted.Failures) != 2 ||
		!errors.Is(result.Err, engine.ErrInstagramPublicExtraction) || !errors.Is(result.Err, engine.ErrAuthenticationRequired) ||
		!strings.Contains(result.Err.Error(), "embed 조회 실패:") || !strings.Contains(result.Err.Error(), "cli 조회 실패:") {
		t.Fatalf("exhausted chain lost failures: %+v", result)
	}
	for _, terminal := range []error{engine.ErrRateLimited, engine.ErrOutputLimit, engine.ErrStopUnconfirmed, context.Canceled, context.DeadlineExceeded} {
		t.Run(terminal.Error(), func(t *testing.T) {
			called := false
			after := engine.DiscoveryHandler[string]{Name: "must-not-run", Handle: func(context.Context) engine.DiscoveryStepResult[string] {
				called = true
				return engine.DiscoveryStepResult[string]{}
			}}
			result := engine.RunDiscoveryChain(t.Context(), first, last, chainHandler("terminal", engine.DiscoveryStop, "", terminal), after)
			if called || result.Err != terminal || result.Decision != engine.DiscoveryStop || len(result.Failures) != 3 ||
				errors.Is(result.Err, engine.ErrAuthenticationRequired) || errors.Is(result.Err, engine.ErrInstagramPublicExtraction) {
				t.Fatalf("terminal failure inherited earlier classification or continued: %+v", result)
			}
		})
	}
}

func TestDiscoveryChainCancellation(t *testing.T) {
	for _, before := range []bool{true, false} {
		for _, decision := range []engine.DiscoveryDecision{engine.DiscoveryHandled, engine.DiscoveryNext} {
			ctx, cancel := context.WithCancel(t.Context())
			calls := 0
			if before {
				cancel()
			}
			handler := engine.DiscoveryHandler[string]{Name: "cancel", Handle: func(context.Context) engine.DiscoveryStepResult[string] {
				calls++
				cancel()
				return engine.DiscoveryStepResult[string]{Decision: decision, Value: "late-result", Err: nil}
			}}
			result := engine.RunDiscoveryChain(ctx, handler, handler)
			cancel()
			wantCalls := 1
			if before {
				wantCalls = 0
			}
			if result.Err != context.Canceled || result.Decision != engine.DiscoveryStop || calls != wantCalls {
				t.Fatalf("cancelled chain continued: before=%v, calls=%d, result=%+v", before, calls, result)
			}
		}
	}
	// A strategy's own timeout can fall back while the request is still alive.
	result := engine.RunDiscoveryChain(t.Context(), chainHandler("timed-out", engine.DiscoveryNext, "", context.DeadlineExceeded), chainHandler("fallback", engine.DiscoveryHandled, "plan", nil))
	if result.Err != nil || result.Value != "plan" {
		t.Fatalf("strategy timeout cancelled the request: %+v", result)
	}
}

func TestDiscoveryChainInvalidContract(t *testing.T) {
	for _, handlers := range [][]engine.DiscoveryHandler[string]{
		nil,
		{{Name: "missing-callback"}},
		{chainHandler("", engine.DiscoveryHandled, "", nil)},
		{chainHandler("success-with-error", engine.DiscoveryHandled, "", errors.New("failure"))},
		{chainHandler("next-without-error", engine.DiscoveryNext, "", nil)},
		{chainHandler("stop-without-error", engine.DiscoveryStop, "", nil)},
		{chainHandler("unknown", engine.DiscoveryDecision(255), "", errors.New("failure"))},
	} {
		result := engine.RunDiscoveryChain(t.Context(), handlers...)
		if result.Err != engine.ErrInvalidDiscoveryChain || result.Decision != engine.DiscoveryStop {
			t.Fatalf("invalid contract allowed: %+v", result)
		}
	}
}

func TestDiscoveryChainOutputOmitsPrivateDetails(t *testing.T) {
	var outputs []engine.Output
	ctx := engine.WithOutputObserver(t.Context(), func(output engine.Output) { outputs = append(outputs, output) })
	result := engine.RunDiscoveryChain(ctx,
		chainHandler("embed", engine.DiscoveryNext, "private-result", errors.New("https://example.invalid/?session=private-value")),
		chainHandler("public", engine.DiscoveryHandled, "private-result", nil),
	)
	if result.Err != nil || len(outputs) != 2 || !strings.Contains(outputs[0].Text, "embed · 조회 실패") || !strings.Contains(outputs[1].Text, "public · 조회 성공") {
		t.Fatalf("missing strategy trace: %+v", outputs)
	}
	for _, output := range outputs {
		if output.Engine != "download" || output.Stream != "system" || strings.Contains(output.Text, "private-") {
			t.Fatalf("private discovery data in log: %+v", output)
		}
	}
	outputs = nil
	engine.RunDiscoveryChain(ctx, chainHandler("last", engine.DiscoveryNext, "", engine.ErrUnsupportedURL))
	if len(outputs) != 1 || !strings.Contains(outputs[0].Text, "대체 방식 없음") {
		t.Fatalf("exhausted chain promised another strategy: %+v", outputs)
	}
}
