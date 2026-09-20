package promtui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

type PromptUI interface {
	AskForInput(title string, isRequired bool) (string, error)
	AskForPassword(title string, isRequired bool) (string, error)
	AskForSelect(title string, options []huh.Option[string], isRequired bool) (string, error)
	AskForYesNo(title string, isRequired bool) (bool, error)
}

type promptUI struct{}

func NewPromptUI() PromptUI {
	return &promptUI{}
}

// AskForInput reads text using the interactive prompt.
func (p *promptUI) AskForInput(title string, isRequired bool) (string, error) {
	return p.askForText(title, isRequired, huh.EchoModeNormal)
}

// AskForPassword reads sensitive input using a masked terminal field. The
// returned value is kept in memory only; callers must not log or persist it.
func (p *promptUI) AskForPassword(title string, isRequired bool) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", title)
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("prompt for %s: %w", title, err)
	}
	if isRequired && len(value) == 0 {
		return "", fmt.Errorf("sorry, %s is required", title)
	}
	return string(value), nil
}

func (p *promptUI) askForText(title string, isRequired bool, echoMode huh.EchoMode) (string, error) {
	var val string
	prompt := huh.NewInput().Title(title).Value(&val).EchoMode(echoMode)
	if isRequired {
		prompt.Validate(func(value string) error {
			if value == "" {
				return fmt.Errorf("sorry, %s is required", title)
			}
			return nil
		})
	}
	if err := prompt.Run(); err != nil {
		return val, fmt.Errorf("prompt for %s: %w", title, err)
	}
	return val, nil
}

func (p *promptUI) AskForSelect(title string, options []huh.Option[string], isRequired bool) (string, error) {
	var selected string

	prompt := huh.NewSelect[string]().Title(title).Options(options...).Value(&selected)

	if err := prompt.Run(); err != nil {
		return selected, fmt.Errorf("prompt for %s: %w", title, err)
	}

	return selected, nil
}

func (p *promptUI) AskForYesNo(title string, isRequired bool) (bool, error) {
	var val bool

	prompt := huh.NewConfirm().
		Title(title).
		Affirmative("Yes!").
		Negative("No.").
		Value(&val)

	if err := prompt.Run(); err != nil {
		return val, fmt.Errorf("prompt for %s: %w", title, err)
	}

	return val, nil
}
