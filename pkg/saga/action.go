package saga

import (
	"context"
	"encoding/json"
	"fmt"
)

// Action represents a single step in a saga. Actions are stateless and
// created by factories that are registered at application startup.
// All runtime data flows through ActionInputs.
type Action interface {
	// Execute performs the action and returns an output that can be used
	// by subsequent actions. The output must be JSON-serializable.
	// On error, output is retained only for diagnostics; it supplies no inputs
	// and Undo is not called. Failed actions must clean up their own work.
	// Typed actions omit zero-valued error outputs, including nil pointers.
	Execute(ctx context.Context, inputs ActionInputs) (output any, err error)

	// Undo reverses the action. It receives the same inputs and the output
	// that was produced by Execute. Undo should be idempotent.
	Undo(ctx context.Context, inputs ActionInputs, output any) error
}

// ActionInputs provides access to initial saga inputs and outputs from
// prior actions. All outputs live in a flat namespace.
type ActionInputs interface {
	// Get retrieves an input by key, deserializing it into target.
	// Returns an error if the key doesn't exist or deserialization fails.
	Get(key string, target any) error

	// Has checks if an input exists (for optional inputs).
	Has(key string) bool

	// Keys returns all available input keys.
	Keys() []string
}

// inputs implements ActionInputs by combining initial inputs with action outputs.
type inputs struct {
	initial map[string]any
	outputs map[string]json.RawMessage
}

// newInputs creates an ActionInputs from initial inputs and prior action outputs.
func newInputs(initial map[string]any, outputs map[string]json.RawMessage) ActionInputs {
	return &inputs{
		initial: initial,
		outputs: outputs,
	}
}

// Get retrieves a value by key, checking outputs first then initial inputs.
func (i *inputs) Get(key string, target any) error {
	// Check outputs first (from prior actions)
	if raw, ok := i.outputs[key]; ok {
		if err := json.Unmarshal(raw, target); err != nil {
			return fmt.Errorf("deserializing output %q: %w", key, err)
		}
		return nil
	}

	// Check initial inputs
	if val, ok := i.initial[key]; ok {
		// Initial inputs may already be the correct type or need JSON round-trip
		switch v := val.(type) {
		case json.RawMessage:
			if err := json.Unmarshal(v, target); err != nil {
				return fmt.Errorf("deserializing input %q: %w", key, err)
			}
			return nil
		default:
			// For values that came from Go code (not persistence), do JSON round-trip
			data, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("serializing input %q: %w", key, err)
			}
			if err := json.Unmarshal(data, target); err != nil {
				return fmt.Errorf("deserializing input %q: %w", key, err)
			}
			return nil
		}
	}

	return fmt.Errorf("input %q not found", key)
}

// Has returns true if the key exists in outputs or initial inputs.
func (i *inputs) Has(key string) bool {
	if _, ok := i.outputs[key]; ok {
		return true
	}
	_, ok := i.initial[key]
	return ok
}

// Keys returns all available input keys.
func (i *inputs) Keys() []string {
	seen := make(map[string]bool)
	var keys []string

	for k := range i.outputs {
		if !seen[k] {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	for k := range i.initial {
		if !seen[k] {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	return keys
}
