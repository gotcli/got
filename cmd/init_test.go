package cmd

import "testing"

type inputPromptStub struct {
	value string
	calls int
}

func (s *inputPromptStub) AskForInput(_ string, _ bool) (string, error) {
	s.calls++
	return s.value, nil
}

func TestAskDatabaseHostPromptsWhenMissing(t *testing.T) {
	ui := &inputPromptStub{value: " db.internal "}
	host, err := askDatabaseHost(ui, "")
	if err != nil {
		t.Fatal(err)
	}
	if host != "db.internal" || ui.calls != 1 {
		t.Fatalf("host=%q calls=%d", host, ui.calls)
	}
}

func TestAskDatabaseHostKeepsFlagValue(t *testing.T) {
	ui := &inputPromptStub{value: "ignored"}
	host, err := askDatabaseHost(ui, "db.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if host != "db.example.com" || ui.calls != 0 {
		t.Fatalf("host=%q calls=%d", host, ui.calls)
	}
}
