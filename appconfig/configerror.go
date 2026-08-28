package appconfig

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	toml "github.com/pelletier/go-toml/v2"
	tomlast "github.com/pelletier/go-toml/v2/unstable"

	"miren.dev/runtime/pkg/theme"
)

// ConfigError is returned from config loading when the TOML file has problems.
// It renders compiler-style diagnostics with file:line references.
type ConfigError struct {
	FilePath    string
	Diagnostics []Diagnostic
}

// Diagnostic represents a single error within a config file.
type Diagnostic struct {
	Line    int    // 1-indexed, 0 if unknown
	Column  int    // 1-indexed, 0 if unknown
	Message string // the core error message
	Context string // visual context from go-toml (if available)
	Hint    string // e.g. "did you mean \"command\"?"
}

func (e *ConfigError) Error() string {
	var b strings.Builder
	for i, d := range e.Diagnostics {
		if i > 0 {
			b.WriteByte('\n')
		}
		// file:line: message
		if d.Line > 0 {
			fmt.Fprintf(&b, "%s:%d: %s", e.FilePath, d.Line, d.Message)
		} else {
			fmt.Fprintf(&b, "%s: %s", e.FilePath, d.Message)
		}
		// Visual context from go-toml (already includes line numbers and tildes)
		if d.Context != "" {
			b.WriteByte('\n')
			b.WriteString(d.Context)
		}
		if d.Hint != "" {
			b.WriteByte('\n')
			fmt.Fprintf(&b, "  hint: %s", d.Hint)
		}
	}
	return b.String()
}

// WriteForTerminal renders a colorized, multi-line diagnostic to w.
// Implements ui.TerminalError.
//
// Styling goes through pkg/theme rather than raw ANSI attributes so the output
// stays readable on light backgrounds. The source context in particular used to
// use the terminal's dim attribute, which disappears entirely on some light
// themes — exactly the failure theme's Muted role exists to avoid.
func (e *ConfigError) WriteForTerminal(w io.Writer) {
	var (
		header  = lipgloss.NewStyle().Foreground(theme.Error).Bold(true)
		hint    = lipgloss.NewStyle().Foreground(theme.Warning)
		context = lipgloss.NewStyle().Foreground(theme.Muted)
	)

	for i, d := range e.Diagnostics {
		if i > 0 {
			fmt.Fprintln(w)
		}
		// file:line: message
		if d.Line > 0 {
			fmt.Fprint(w, header.Render(fmt.Sprintf("%s:%d: ", e.FilePath, d.Line)))
		} else {
			fmt.Fprint(w, header.Render(fmt.Sprintf("%s: ", e.FilePath)))
		}
		fmt.Fprintln(w, d.Message)

		// Visual context from go-toml (line numbers + tildes)
		if d.Context != "" {
			fmt.Fprintln(w, context.Render(d.Context))
		}

		if d.Hint != "" {
			fmt.Fprintln(w, hint.Render(fmt.Sprintf("  hint: %s", d.Hint)))
		}
	}
}

// ValidationError is a structured error from Validate() that carries
// the TOML key path for AST-based line number resolution.
type ValidationError struct {
	KeyPath string // e.g. "services.web.concurrency.mode"
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// enrichDecodeError wraps go-toml decode errors with file path, source context,
// and "did you mean?" suggestions for unknown fields.
func enrichDecodeError(filePath string, _ []byte, err error) error {
	switch e := err.(type) {
	case *toml.StrictMissingError:
		return enrichStrictMissingError(filePath, e)
	case *toml.DecodeError:
		return enrichSingleDecodeError(filePath, e)
	default:
		return fmt.Errorf("%s: %w", filePath, err)
	}
}

func enrichStrictMissingError(filePath string, sme *toml.StrictMissingError) *ConfigError {
	ce := &ConfigError{FilePath: filePath}
	for _, de := range sme.Errors {
		row, col := de.Position()
		key := de.Key()
		unknown := ""
		var parentPath string
		if len(key) > 0 {
			unknown = key[len(key)-1]
			parentPath = keyParentPath(key)
		}

		d := Diagnostic{
			Line:    row,
			Column:  col,
			Message: fmt.Sprintf("unknown field %q", unknown),
			Context: indentContext(de.String()),
		}

		if unknown != "" {
			if candidates, ok := validFieldsForPath(parentPath); ok {
				if suggestion := suggestField(unknown, candidates); suggestion != "" {
					d.Hint = fmt.Sprintf("did you mean %q?", suggestion)
				}
			}
		}

		ce.Diagnostics = append(ce.Diagnostics, d)
	}
	return ce
}

func enrichSingleDecodeError(filePath string, de *toml.DecodeError) *ConfigError {
	row, _ := de.Position()
	return &ConfigError{
		FilePath: filePath,
		Diagnostics: []Diagnostic{{
			Line:    row,
			Message: de.Error(),
			Context: indentContext(de.String()),
		}},
	}
}

// enrichValidationError wraps a ValidationError with file path and
// AST-resolved line numbers.
func enrichValidationError(filePath string, raw []byte, err error) error {
	ve, ok := err.(*ValidationError)
	if !ok {
		return fmt.Errorf("%s: %w", filePath, err)
	}

	d := Diagnostic{
		Message: ve.Message,
	}

	if ve.KeyPath != "" && len(raw) > 0 {
		if line := resolveKeyLine(raw, ve.KeyPath); line > 0 {
			d.Line = line
			d.Context = extractSourceLine(raw, line)
		}
	}

	return &ConfigError{
		FilePath:    filePath,
		Diagnostics: []Diagnostic{d},
	}
}

// resolveKeyLine uses the go-toml/v2 unstable AST parser to find the line
// number of a dotted key path (e.g. "services.web.concurrency.mode").
func resolveKeyLine(data []byte, keyPath string) int {
	parts := strings.Split(keyPath, ".")

	var p tomlast.Parser
	p.Reset(data)

	return walkAST(&p, parts, 0)
}

// walkAST recursively searches the TOML AST for a key path and returns the
// line number where the final key is defined.
func walkAST(p *tomlast.Parser, parts []string, depth int) int {
	if depth >= len(parts) {
		return 0
	}

	target := parts[depth]
	isLast := depth == len(parts)-1
	// For wildcard segments (service names, addon names), match any key
	isWild := target == "*"

	for p.NextExpression() {
		node := p.Expression()

		switch node.Kind {
		case tomlast.Table, tomlast.ArrayTable:
			keyIter := node.Key()
			// Table headers contain the full key path (e.g. [services.web.concurrency]).
			// When we're at depth > 0, the first `depth` components were already
			// matched by a parent table — skip them and only compare the remainder.
			keyIdx := 0
			matchDepth := depth
			matched := true
			var lastKeyNode *tomlast.Node

			for keyIter.Next() {
				n := keyIter.Node()
				keyName := string(n.Data)

				if keyIdx < depth {
					// Verify skipped prefix components still match the
					// already-resolved path — prevents sibling tables like
					// [services.api.concurrency] from matching when we're
					// looking for services.web.concurrency.
					wantPrefix := parts[keyIdx]
					if wantPrefix != "*" && keyName != wantPrefix {
						matched = false
						break
					}
					keyIdx++
					continue
				}

				if matchDepth >= len(parts) {
					matched = false
					break
				}
				lastKeyNode = n
				wantKey := parts[matchDepth]
				if wantKey != "*" && keyName != wantKey {
					matched = false
					break
				}
				matchDepth++
				keyIdx++
			}

			if !matched || keyIdx < depth {
				// keyIdx < depth means the table header had fewer components
				// than our current depth — it's an unrelated table.
				continue
			}

			if matchDepth >= len(parts) && lastKeyNode != nil {
				// The table header itself matches the full key path
				shape := p.Shape(lastKeyNode.Raw)
				return shape.Start.Line
			}

			// Continue scanning expressions under this table for remaining parts
			return walkAST(p, parts, matchDepth)

		case tomlast.KeyValue:
			keyIter := node.Key()
			if !keyIter.Next() {
				continue
			}
			keyNode := keyIter.Node()
			keyName := string(keyNode.Data)

			if (isWild || keyName == target) && isLast {
				shape := p.Shape(keyNode.Raw)
				return shape.Start.Line
			}

		case tomlast.Invalid, tomlast.Comment, tomlast.Key, tomlast.Array,
			tomlast.InlineTable, tomlast.String, tomlast.Bool, tomlast.Float,
			tomlast.Integer, tomlast.LocalDate, tomlast.LocalTime,
			tomlast.LocalDateTime, tomlast.DateTime:
			// Not top-level expressions; keep scanning.
		}
	}

	return 0
}

// extractSourceLine returns a formatted context line for display.
func extractSourceLine(data []byte, line int) string {
	lines := strings.Split(string(data), "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	return fmt.Sprintf("  %d | %s", line, lines[line-1])
}

// indentContext adds two spaces of indentation to each line of a go-toml
// context string for nested display.
func indentContext(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		lines[i] = "  " + line
	}
	return strings.Join(lines, "\n")
}

// keyParentPath returns the section path for looking up valid fields.
// For example, Key{"services", "web", "comand"} → "services.*"
// Key{"services", "database", "disks", "size"} → "services.*.disks"
func keyParentPath(key toml.Key) string {
	if len(key) <= 1 {
		return ""
	}
	// Build the parent path (everything except the unknown field itself),
	// replacing dynamic map/array names with "*".
	var parts []string
	for i, k := range key[:len(key)-1] {
		if i > 0 && isMapSection(parts) {
			// This segment is a dynamic key (e.g., service name, addon name)
			parts = append(parts, "*")
		} else {
			parts = append(parts, k)
		}
	}
	return strings.Join(parts, ".")
}

// isMapSection returns true if the last element of the path represents a
// section where the next key is a dynamic name.
func isMapSection(path []string) bool {
	if len(path) == 0 {
		return false
	}
	last := path[len(path)-1]
	return last == "services" || last == "addons" || last == "tasks"
}

// validFieldsForPath returns the valid field names for a given section path.
func validFieldsForPath(path string) ([]string, bool) {
	fields, ok := validFields[path]
	return fields, ok
}

// validFields maps section paths to their valid field names.
var validFields = map[string][]string{
	"":                       {"name", "env", "concurrency", "services", "tasks", "web", "build", "include", "addons", "aliases", "workload_role"},
	"services.*":             {"command", "port", "port_name", "port_type", "ports", "image", "env", "concurrency", "disks", "port_timeout"},
	"services.*.concurrency": {"mode", "requests_per_instance", "scale_down_delay", "num_instances", "shutdown_timeout"},
	"services.*.disks":       {"name", "provider", "mount_path", "read_only", "size_gb", "filesystem", "lease_timeout"},
	"services.*.ports":       {"port", "name", "type", "node_port"},
	// Tasks deliberately have no ports, concurrency, image, or disks: a task is
	// a command the platform runs, not a process it keeps up. Suggesting them
	// here would advertise fields the schema doesn't accept.
	"tasks.*":        {"command", "trigger", "every", "schedule", "timeout", "retries", "max_concurrent", "env"},
	"tasks.*.env":    {"key", "value", "required", "sensitive", "description"},
	"build":          {"dockerfile", "onbuild", "version", "alpine_image"},
	"addons.*":       {"variant", "version"},
	"env":            {"key", "value", "required", "sensitive", "description"},
	"services.*.env": {"key", "value", "required", "sensitive", "description"},
}

// suggestField returns the best matching field name if similar enough,
// or empty string if no good match exists. Prioritizes substring matches
// over pure edit distance.
func suggestField(unknown string, candidates []string) string {
	// First check substring containment — these are high-confidence matches
	for _, c := range candidates {
		if strings.Contains(c, unknown) || strings.Contains(unknown, c) {
			return c
		}
	}

	// Fall back to Levenshtein distance (threshold: 3)
	bestDist := 4
	bestMatch := ""
	for _, c := range candidates {
		d := levenshteinDistance(unknown, c)
		if d < bestDist {
			bestDist = d
			bestMatch = c
		}
	}
	return bestMatch
}

func levenshteinDistance(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}

	return prev[lb]
}
