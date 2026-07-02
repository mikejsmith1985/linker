package claude

import "context"

// Fake is an in-memory LLM for unit tests. It records every call and returns a
// canned response, so LLM-dependent code runs without a network or API key.
type Fake struct {
	// Respond, when set, computes the reply from the prompts. When nil, Text and
	// Err are returned verbatim.
	Respond func(system, prompt string) (string, error)
	Text    string
	Err     error

	// Calls records every Complete invocation, in order.
	Calls []FakeCall
}

// FakeCall captures the arguments of one Complete call.
type FakeCall struct {
	System string
	Prompt string
}

// Complete satisfies LLM. It never touches the network.
func (f *Fake) Complete(_ context.Context, system, prompt string) (string, error) {
	f.Calls = append(f.Calls, FakeCall{System: system, Prompt: prompt})
	if f.Respond != nil {
		return f.Respond(system, prompt)
	}
	return f.Text, f.Err
}
