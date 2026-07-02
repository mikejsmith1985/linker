package claude

import (
	"context"
	"errors"
	"testing"
)

func TestFakeRecordsCallsAndReturnsText(t *testing.T) {
	fake := &Fake{Text: "hello"}
	got, err := fake.Complete(context.Background(), "sys", "prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Errorf("Complete() = %q, want hello", got)
	}
	if len(fake.Calls) != 1 || fake.Calls[0].System != "sys" || fake.Calls[0].Prompt != "prompt" {
		t.Errorf("Calls = %+v, want one call with sys/prompt", fake.Calls)
	}
}

func TestFakeRespondFunc(t *testing.T) {
	fake := &Fake{Respond: func(_, prompt string) (string, error) {
		return "echo:" + prompt, nil
	}}
	got, _ := fake.Complete(context.Background(), "", "x")
	if got != "echo:x" {
		t.Errorf("Complete() = %q, want echo:x", got)
	}
}

func TestFakeReturnsError(t *testing.T) {
	sentinel := errors.New("boom")
	fake := &Fake{Err: sentinel}
	if _, err := fake.Complete(context.Background(), "", ""); !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want sentinel", err)
	}
}
