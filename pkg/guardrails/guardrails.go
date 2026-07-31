// Package guardrails provides a security layer for agent input/output.
package guardrails

import "context"

// Guardrail validates user input and LLM output.
// Implementations: RateLimiter, ContentFilter, or custom.
type Guardrail interface {
	ValidateInput(ctx context.Context, input string) error
	ValidateOutput(ctx context.Context, output string) error
}

// Chain combines several guardrails; stops at the first error.
type Chain []Guardrail

func (c Chain) ValidateInput(ctx context.Context, input string) error {
	for _, g := range c {
		if err := g.ValidateInput(ctx, input); err != nil {
			return err
		}
	}
	return nil
}

func (c Chain) ValidateOutput(ctx context.Context, output string) error {
	for _, g := range c {
		if err := g.ValidateOutput(ctx, output); err != nil {
			return err
		}
	}
	return nil
}
