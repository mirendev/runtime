// Package logfilter provides a simple query syntax for filtering logs.
// The syntax supports:
//   - Word matching: `error` matches logs containing "error" (case-insensitive)
//   - Quoted phrases: `"error message"` or `'error message'` matches exact phrase (case-insensitive)
//   - Regex matching: `/pattern/` matches logs matching the regex pattern
//   - Negation: `-word` excludes logs containing "word"
//   - Multiple terms: `error timeout` requires both terms to match (AND)
//
// Examples:
//   - `error` - logs containing "error"
//   - `"connection failed"` - logs containing exact phrase "connection failed"
//   - `error timeout` - logs containing both "error" AND "timeout"
//   - `-debug` - logs NOT containing "debug"
//   - `error -debug` - logs containing "error" but NOT "debug"
//   - `/err(or)?/` - logs matching the regex
//   - `error /warn(ing)?/` - logs containing "error" AND matching the regex
package logfilter

import (
	"regexp"
	"strings"
)

// Filter represents a compiled log filter that can match log lines
// and be converted to LogsQL for server-side filtering.
type Filter struct {
	terms []term
}

type termType int

const (
	termWord   termType = iota // Simple word/substring match
	termPhrase                 // Quoted phrase (exact match with spaces)
	termRegex                  // Regex match
)

type term struct {
	typ     termType
	value   string         // Original value (for word) or pattern (for regex)
	regex   *regexp.Regexp // Compiled regex (for regex terms, or word converted to case-insensitive)
	negated bool           // If true, this term must NOT match
}

// Parse parses a filter string into a Filter.
// Returns nil if the input is empty.
func Parse(input string) (*Filter, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}

	// Convert to runes for proper UTF-8 handling
	runes := []rune(input)
	var terms []term

	i := 0
	for i < len(runes) {
		// Skip whitespace
		for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
			i++
		}
		if i >= len(runes) {
			break
		}

		// Check for negation prefix
		negated := false
		if runes[i] == '-' {
			negated = true
			i++
			if i >= len(runes) {
				break
			}
		}

		// Check for quoted string (starts with " or ')
		if runes[i] == '"' || runes[i] == '\'' {
			quote := runes[i]
			i++ // Skip opening quote
			var phrase strings.Builder
			for i < len(runes) && runes[i] != quote {
				if runes[i] == '\\' && i+1 < len(runes) {
					i++ // Skip backslash
					phrase.WriteRune(runes[i])
				} else {
					phrase.WriteRune(runes[i])
				}
				i++
			}
			if i < len(runes) {
				i++ // Skip closing quote
			}
			phraseStr := phrase.String()
			if phraseStr != "" {
				re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(phraseStr))
				if err != nil {
					return nil, err
				}
				terms = append(terms, term{
					typ:     termPhrase,
					value:   phraseStr,
					regex:   re,
					negated: negated,
				})
			}
		} else if runes[i] == '/' {
			// Regex (starts with /)
			start := i
			i++ // Skip opening /
			var pattern strings.Builder
			for i < len(runes) && runes[i] != '/' {
				if runes[i] == '\\' && i+1 < len(runes) {
					pattern.WriteRune(runes[i])
					i++
					pattern.WriteRune(runes[i])
				} else {
					pattern.WriteRune(runes[i])
				}
				i++
			}
			if i >= len(runes) {
				// No closing /, treat as literal word
				literal := string(runes[start:])
				re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(literal))
				if err != nil {
					return nil, err
				}
				terms = append(terms, term{
					typ:     termWord,
					value:   literal,
					regex:   re,
					negated: negated,
				})
				break
			}
			i++ // Skip closing /

			patternStr := pattern.String()
			re, err := regexp.Compile(patternStr)
			if err != nil {
				return nil, err
			}
			terms = append(terms, term{
				typ:     termRegex,
				value:   patternStr,
				regex:   re,
				negated: negated,
			})
		} else {
			// Regular word - read until whitespace
			var word strings.Builder
			for i < len(runes) && runes[i] != ' ' && runes[i] != '\t' {
				word.WriteRune(runes[i])
				i++
			}
			wordStr := word.String()
			if wordStr != "" {
				re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(wordStr))
				if err != nil {
					return nil, err
				}
				terms = append(terms, term{
					typ:     termWord,
					value:   wordStr,
					regex:   re,
					negated: negated,
				})
			}
		}
	}

	if len(terms) == 0 {
		return nil, nil
	}

	return &Filter{terms: terms}, nil
}

// Match returns true if the line matches the filter.
// All terms must match (AND logic), with negated terms inverted.
func (f *Filter) Match(line string) bool {
	if f == nil {
		return true
	}

	for _, t := range f.terms {
		matches := t.regex.MatchString(line)
		if t.negated {
			matches = !matches
		}
		if !matches {
			return false
		}
	}
	return true
}

// ToLogsQL converts the filter to a LogsQL filter expression.
// This can be appended to a LogsQL query for server-side filtering.
func (f *Filter) ToLogsQL() string {
	if f == nil || len(f.terms) == 0 {
		return ""
	}

	var parts []string
	for _, t := range f.terms {
		var part string
		switch t.typ {
		case termWord:
			// LogsQL word filter - case insensitive by default
			if t.negated {
				part = "-" + t.value
			} else {
				part = t.value
			}
		case termPhrase:
			// LogsQL phrase filter - quoted for exact match
			if t.negated {
				part = "-\"" + escapeLogsQLString(t.value) + "\""
			} else {
				part = "\"" + escapeLogsQLString(t.value) + "\""
			}
		case termRegex:
			// LogsQL regex filter
			if t.negated {
				part = "-~\"" + escapeLogsQLString(t.value) + "\""
			} else {
				part = "~\"" + escapeLogsQLString(t.value) + "\""
			}
		}
		parts = append(parts, part)
	}

	return strings.Join(parts, " ")
}

// String returns the original filter string representation.
func (f *Filter) String() string {
	if f == nil || len(f.terms) == 0 {
		return ""
	}

	var parts []string
	for _, t := range f.terms {
		var part string
		if t.negated {
			part = "-"
		}
		switch t.typ {
		case termWord:
			part += t.value
		case termPhrase:
			part += "\"" + strings.ReplaceAll(t.value, "\"", "\\\"") + "\""
		case termRegex:
			part += "/" + t.value + "/"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " ")
}

func escapeLogsQLString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
