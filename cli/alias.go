package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"miren.dev/mflags"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/pkg/ui"
)

// expandAlias checks if the given args match a configured alias and expands it.
// It tries progressively longer prefixes (longest match wins).
// Returns an error if an alias name conflicts with a built-in command.
func expandAlias(d *mflags.Dispatcher, args []string) ([]string, error) {
	if len(args) == 0 {
		return args, nil
	}

	ac, err := appconfig.LoadAppConfig()
	if err != nil {
		// errors.AsType, not a type assertion: a wrapped rich error would
		// otherwise silently degrade to the plain path below.
		if se, ok := errors.AsType[ui.SeverityTerminalError](err); ok {
			se.WriteWithSeverity(os.Stderr, ui.SeverityWarning)
		} else if te, ok := errors.AsType[ui.TerminalError](err); ok {
			fmt.Fprint(os.Stderr, "warning: ")
			te.WriteForTerminal(os.Stderr)
		} else {
			fmt.Fprintf(os.Stderr, "warning: could not load %s: %v\n", appconfig.AppConfigPath, err)
		}
		return args, nil
	}
	if ac == nil || len(ac.Aliases) == 0 {
		return args, nil
	}

	// Try progressively longer prefixes, longest match wins
	var bestMatch string
	var bestLen int

	for name := range ac.Aliases {
		words := strings.Fields(name)
		if len(words) > len(args) || len(words) <= bestLen {
			continue
		}

		match := true
		for i, w := range words {
			if args[i] != w {
				match = false
				break
			}
		}

		if match {
			bestMatch = name
			bestLen = len(words)
		}
	}

	if bestMatch == "" {
		return args, nil
	}

	// Check that the alias doesn't shadow a built-in command
	if d.HasCommand(bestMatch) {
		return nil, fmt.Errorf("alias %q shadows built-in command %q", bestMatch, bestMatch)
	}

	target := ac.Aliases[bestMatch]
	expanded, err := tokenizeCommand(target)
	if err != nil {
		return nil, fmt.Errorf("invalid alias %q: %w", bestMatch, err)
	}

	// Append any additional arguments the user provided after the alias
	expanded = append(expanded, args[bestLen:]...)
	return expanded, nil
}

// tokenizeCommand splits a command string into tokens, handling quoted strings.
func tokenizeCommand(s string) ([]string, error) {
	var tokens []string
	var current []byte
	inDouble := false
	inSingle := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]

		if escaped {
			current = append(current, c)
			escaped = false
			continue
		}

		if c == '\\' && inDouble {
			escaped = true
			continue
		}

		if c == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}

		if c == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}

		if c == ' ' && !inDouble && !inSingle {
			if len(current) > 0 {
				tokens = append(tokens, string(current))
				current = current[:0]
			}
			continue
		}

		current = append(current, c)
	}

	if inDouble || inSingle {
		return nil, fmt.Errorf("unterminated quote in command string")
	}

	if len(current) > 0 {
		tokens = append(tokens, string(current))
	}

	return tokens, nil
}
