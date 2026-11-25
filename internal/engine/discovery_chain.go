package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type DiscoveryDecision uint8

const (
	DiscoveryHandled DiscoveryDecision = iota
	DiscoveryNext
	DiscoveryStop
)

// A handler may pass a failure to the next strategy or stop the request. A
// successful handler owns the returned download plan; it is never reselected.
type DiscoveryStepResult[T any] struct {
	Decision DiscoveryDecision
	Value    T
	Err      error
}

type DiscoveryHandler[T any] struct {
	Name   string
	Handle func(context.Context) DiscoveryStepResult[T]
}

type DiscoveryFailure struct {
	Handler string
	Err     error
}

type DiscoveryChainResult[T any] struct {
	DiscoveryStepResult[T]
	Handler  string
	Failures []DiscoveryFailure
}

var ErrInvalidDiscoveryChain = errors.New("다운로드 조회 연쇄 구성이 올바르지 않습니다")

// Only an exhausted chain exposes all failed strategies to errors.Is/As. A
// terminal failure keeps its own classification, rather than inheriting an
// earlier authentication or unsupported-URL error.
type DiscoveryChainError struct {
	Failures []DiscoveryFailure
}

func (e *DiscoveryChainError) Error() string {
	parts := make([]string, 0, len(e.Failures))
	for _, failure := range e.Failures {
		parts = append(parts, fmt.Sprintf("%s 조회 실패: %v", failure.Handler, failure.Err))
	}
	return strings.Join(parts, "; ")
}

func (e *DiscoveryChainError) Unwrap() []error {
	errs := make([]error, 0, len(e.Failures))
	for _, failure := range e.Failures {
		errs = append(errs, failure.Err)
	}
	return errs
}

func RunDiscoveryChain[T any](ctx context.Context, handlers ...DiscoveryHandler[T]) DiscoveryChainResult[T] {
	result := DiscoveryChainResult[T]{DiscoveryStepResult: DiscoveryStepResult[T]{Decision: DiscoveryStop}}
	if err := ctx.Err(); err != nil {
		result.Err = err
		return result
	}
	if len(handlers) == 0 {
		result.Err = ErrInvalidDiscoveryChain
		return result
	}
	for index, handler := range handlers {
		if err := ctx.Err(); err != nil {
			result.Decision, result.Err = DiscoveryStop, err
			return result
		}
		if handler.Name == "" || handler.Handle == nil {
			result.Decision, result.Err = DiscoveryStop, ErrInvalidDiscoveryChain
			return result
		}
		step := handler.Handle(ctx)
		if err := ctx.Err(); err != nil {
			step.Decision, step.Err = DiscoveryStop, err
		}
		if (step.Decision == DiscoveryHandled && step.Err != nil) ||
			(step.Decision != DiscoveryHandled && step.Decision != DiscoveryNext && step.Decision != DiscoveryStop) ||
			(step.Decision != DiscoveryHandled && step.Err == nil) {
			step.Decision, step.Err = DiscoveryStop, ErrInvalidDiscoveryChain
		}
		result.DiscoveryStepResult, result.Handler = step, handler.Name
		if step.Err != nil {
			result.Failures = append(result.Failures, DiscoveryFailure{Handler: handler.Name, Err: step.Err})
		}
		discoveryStepOutput(ctx, handler.Name, step.Decision, index+1 < len(handlers))
		switch step.Decision {
		case DiscoveryHandled, DiscoveryStop:
			return result
		case DiscoveryNext:
			continue
		}
	}
	result.Err = &DiscoveryChainError{Failures: append([]DiscoveryFailure(nil), result.Failures...)}
	return result
}

func discoveryStepOutput(ctx context.Context, name string, decision DiscoveryDecision, hasNext bool) {
	observer, _ := ctx.Value(outputKey{}).(func(Output))
	if observer == nil {
		return
	}
	state := "조회 성공"
	if decision == DiscoveryNext {
		state = "조회 실패 · 대체 방식 없음"
		if hasNext {
			state = "조회 실패 · 다음 방식으로 진행"
		}
	} else if decision == DiscoveryStop {
		state = "조회 중단"
	}
	// Log static strategy labels only; errors and results can contain signed
	// media URLs, credentials or private extractor metadata.
	observer(Output{Engine: "download", Stream: "system", Text: name + " · " + state})
}
