package cmd

import (
	"fmt"
	"os"
	"strings"
)

// databasePasswordEnv supplies the inspection password when no terminal is
// available, for example in CI. It is read once and never written anywhere.
const databasePasswordEnv = "GOT_DB_PASSWORD"

type requiredFlag struct {
	name    string
	missing bool
}

// requireFlags fails fast in non-interactive mode, naming every missing flag at
// once instead of failing on the first prompt that cannot open a terminal.
func requireFlags(cfg *config, flags ...requiredFlag) error {
	if !cfg.nonInteractive {
		return nil
	}
	var missing []string
	for _, flag := range flags {
		if flag.missing {
			missing = append(missing, "--"+flag.name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("missing required flags: %s (interactive prompts are unavailable without a terminal or with --no-input)", strings.Join(missing, ", "))
}

func readDatabasePassword(cfg *config, prompt passwordPrompter) (string, error) {
	if value, ok := os.LookupEnv(databasePasswordEnv); ok && value != "" {
		return value, nil
	}
	if cfg.nonInteractive {
		return "", fmt.Errorf("database password is required: set %s (interactive prompts are unavailable without a terminal or with --no-input)", databasePasswordEnv)
	}
	return prompt.AskForPassword("Database password", true)
}
