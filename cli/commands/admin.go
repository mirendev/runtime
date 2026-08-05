package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"miren.dev/runtime/api/admin/admin_v1alpha"
	"miren.dev/runtime/pkg/theme"
	"miren.dev/runtime/pkg/ui"
)

func Admin(ctx *Context, opts struct {
	AppCentric
	List       bool     `long:"list" short:"l" description:"List available admin methods"`
	FuncHelp   bool     `long:"func-help" description:"Show help for a specific admin method"`
	JSON       bool     `long:"json" short:"j" description:"Output as highlighted JSON (default for non-TTY)"`
	Pretty     bool     `long:"pretty" short:"p" description:"Render output in a human-friendly format (default for TTY)"`
	NoValidate bool     `long:"no-validate" description:"Skip method/parameter validation"`
	ParamsFile string   `long:"params-file" short:"f" description:"Read params as JSON from file (use - for stdin)"`
	Method     string   `position:"0" description:"Admin method to call"`
	Params     []string `rest:"true" description:"Method parameters as key=value pairs"`
	Unknown    []string `unknown:"true"`
}) error {
	// Get RPC client
	cl, err := ctx.RPCClient("dev.miren.runtime/admin")
	if err != nil {
		return err
	}

	adminClient := admin_v1alpha.NewAdminClient(cl)

	// Handle --list flag
	if opts.List {
		return adminListMethods(ctx, adminClient, opts.App)
	}

	// Method is required if not listing or getting help
	if opts.Method == "" {
		return fmt.Errorf("method name is required (use --list to see available methods)")
	}

	// Handle --func-help flag, or natural --help/-h on a method (treated the same)
	if opts.FuncHelp || hasHelpFlag(opts.Unknown) {
		return adminMethodHelp(ctx, adminClient, opts.App, opts.Method)
	}

	// Fetch method introspection for type-aware parsing and validation
	var methodInfo *admin_v1alpha.AdminMethod
	var paramTypes map[string]string
	if !opts.NoValidate {
		methodInfo, err = fetchMethodInfo(ctx, adminClient, opts.App, opts.Method)
		if err != nil {
			return err
		}
		if methodInfo != nil && methodInfo.HasParams() {
			paramTypes = make(map[string]string)
			for _, p := range methodInfo.Params() {
				if p.HasParamType() && p.ParamType() != "" {
					paramTypes[p.Name()] = p.ParamType()
				}
			}
		}
	}

	// Build params object from flags, key=value pairs, and/or file.
	// Track where each param came from to detect duplicates.
	params := make(map[string]any)
	paramSources := make(map[string]string) // key -> "file", "flag", or "argument"

	if opts.ParamsFile != "" {
		// Read params from file (or stdin if "-")
		var reader io.Reader
		if opts.ParamsFile == "-" {
			reader = os.Stdin
		} else {
			f, err := os.Open(opts.ParamsFile)
			if err != nil {
				return fmt.Errorf("failed to open params file: %w", err)
			}
			defer f.Close()
			reader = f
		}

		data, err := io.ReadAll(reader)
		if err != nil {
			return fmt.Errorf("failed to read params file: %w", err)
		}

		if err := json.Unmarshal(data, &params); err != nil {
			return fmt.Errorf("failed to parse params JSON: %w", err)
		}

		for key := range params {
			paramSources[key] = "file"
		}
	}

	// Parse unknown flags into params (e.g. --name=value, --name value, --flag)
	if err := parseUnknownFlags(opts.Unknown, params, paramSources, paramTypes); err != nil {
		return err
	}

	// Parse key=value pairs
	for _, p := range opts.Params {
		parts := strings.SplitN(p, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid parameter format: %s (expected key=value)", p)
		}

		key := parts[0]
		value := parts[1]

		if source, ok := paramSources[key]; ok {
			return fmt.Errorf("parameter %q specified multiple times (as %s and as argument)", key, source)
		}

		parsed, err := parseParamValue(key, value, paramTypes)
		if err != nil {
			return err
		}
		paramSources[key] = "argument"
		params[key] = parsed
	}

	// If the method declares params but none were supplied, error out with the
	// method help block instead of letting an empty call fall through to a raw
	// JSON-RPC error. Returning an error keeps the exit code non-zero so shell
	// chains (e.g. `&&`) don't treat a help-rendered call as success — matching
	// the missing-required-params branch in validateAdminCall.
	// Skipped under --no-validate so power users can still issue empty calls.
	if !opts.NoValidate && len(params) == 0 && methodInfo != nil &&
		methodInfo.HasParams() && len(methodInfo.Params()) > 0 {
		return fmt.Errorf("no parameters supplied\n\n%s",
			renderMethodHelpString(opts.App, methodInfo))
	}

	// Validate method and parameters against introspection (unless --no-validate)
	if !opts.NoValidate {
		if err := validateAdminCall(ctx, adminClient, opts.App, opts.Method, params); err != nil {
			return err
		}
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("failed to encode params: %w", err)
	}

	// Make the call
	result, err := adminClient.Invoke(ctx, opts.App, opts.Method, string(paramsJSON))
	if err != nil {
		return fmt.Errorf("RPC error: %w", err)
	}

	// Check for errors
	if result.Result().HasError() && result.Result().Error() != "" {
		errorMsg := result.Result().Error()
		if result.Result().HasErrorCode() && result.Result().ErrorCode() != 0 {
			return fmt.Errorf("admin call failed (code %d): %s", result.Result().ErrorCode(), errorMsg)
		}
		return fmt.Errorf("admin call failed: %s", errorMsg)
	}

	// Determine output format:
	// - If --json is passed, always use JSON
	// - If --pretty is passed, always use pretty
	// - Otherwise, use pretty for TTY, JSON for non-TTY
	isTTY := term.IsTerminal(int(os.Stdout.Fd()))
	usePretty := opts.Pretty || (isTTY && !opts.JSON)

	// Print result
	if result.Result().HasResult() && result.Result().Result() != "" {
		var prettyResult interface{}
		if err := json.Unmarshal([]byte(result.Result().Result()), &prettyResult); err == nil {
			if usePretty {
				ctx.Printf("%s", renderPretty(prettyResult))
			} else {
				ctx.Printf("%s\n", highlightJSON(prettyResult, 0))
			}
		} else {
			ctx.Printf("%s\n", result.Result().Result())
		}
	} else {
		ctx.Printf("OK\n")
	}

	return nil
}

// fetchMethodInfo fetches introspection for a specific method via DescribeMethods.
// Returns nil (no error) if introspection is unavailable or the method isn't found.
func fetchMethodInfo(ctx *Context, client *admin_v1alpha.AdminClient, app, method string) (*admin_v1alpha.AdminMethod, error) {
	result, err := client.DescribeMethods(ctx, app, []string{method})
	if err != nil {
		return nil, nil
	}

	if result.HasError() && result.Error() != "" {
		return nil, nil
	}

	if !result.HasMethods() || len(result.Methods()) == 0 {
		return nil, nil
	}

	return result.Methods()[0], nil
}

// parseParamValue parses a CLI parameter value string using type information from $methods.
// If paramTypes is nil or has no entry for the key, it falls back to trying JSON then string.
func parseParamValue(key, value string, paramTypes map[string]string) (any, error) {
	typ, ok := paramTypes[key]
	if !ok || typ == "" {
		// No type info — try JSON, fall back to string
		var jsonVal any
		if err := json.Unmarshal([]byte(value), &jsonVal); err == nil {
			return jsonVal, nil
		}
		return value, nil
	}

	switch strings.ToLower(typ) {
	case "string":
		return value, nil
	case "number", "integer", "int", "float":
		var n json.Number
		if err := json.Unmarshal([]byte(value), &n); err != nil {
			return nil, fmt.Errorf("parameter %q expects %s, got %q", key, typ, value)
		}
		// Preserve integer vs float distinction
		if strings.ToLower(typ) == "integer" || strings.ToLower(typ) == "int" {
			if i, err := n.Int64(); err == nil {
				return i, nil
			}
		}
		if f, err := n.Float64(); err == nil {
			return f, nil
		}
		return nil, fmt.Errorf("parameter %q expects %s, got %q", key, typ, value)
	case "boolean", "bool":
		switch strings.ToLower(value) {
		case "true", "1", "yes":
			return true, nil
		case "false", "0", "no":
			return false, nil
		default:
			return nil, fmt.Errorf("parameter %q expects boolean, got %q", key, value)
		}
	case "object":
		var obj map[string]any
		if err := json.Unmarshal([]byte(value), &obj); err != nil {
			return nil, fmt.Errorf("parameter %q expects object (JSON), got %q", key, value)
		}
		return obj, nil
	case "array":
		var arr []any
		if err := json.Unmarshal([]byte(value), &arr); err != nil {
			return nil, fmt.Errorf("parameter %q expects array (JSON), got %q", key, value)
		}
		return arr, nil
	default:
		// Unknown type — try JSON, fall back to string
		var jsonVal any
		if err := json.Unmarshal([]byte(value), &jsonVal); err == nil {
			return jsonVal, nil
		}
		return value, nil
	}
}

// parseUnknownFlags parses unknown CLI flags into params with duplicate detection.
// Handles --key=value, --key value, bare key=value, and boolean --flag forms.
// Kebab-case flag names (e.g. --my-param) are converted to underscores (my_param).
func parseUnknownFlags(unknown []string, params map[string]any, paramSources map[string]string, paramTypes map[string]string) error {
	for i := 0; i < len(unknown); i++ {
		arg := unknown[i]

		var key, value string
		var isBoolFlag bool

		switch {
		case strings.HasPrefix(arg, "--"):
			flagStr := arg[2:]
			if before, after, ok := strings.Cut(flagStr, "="); ok {
				key = before
				value = after
			} else {
				key = flagStr
				// Next arg is the value unless it looks like another flag.
				// But allow negative numbers (e.g. -5, -3.14) as values.
				if i+1 < len(unknown) {
					next := unknown[i+1]
					if !strings.HasPrefix(next, "-") || looksLikeNumber(next) {
						i++
						value = next
					} else {
						isBoolFlag = true
					}
				} else {
					isBoolFlag = true
				}
			}

		case strings.HasPrefix(arg, "-"):
			flagStr := arg[1:]
			if before, after, ok := strings.Cut(flagStr, "="); ok {
				key = before
				value = after
			} else {
				key = flagStr
				// Next arg is the value unless it looks like another flag.
				// But allow negative numbers (e.g. -5, -3.14) as values.
				if i+1 < len(unknown) {
					next := unknown[i+1]
					if !strings.HasPrefix(next, "-") || looksLikeNumber(next) {
						i++
						value = next
					} else {
						isBoolFlag = true
					}
				} else {
					isBoolFlag = true
				}
			}

		default:
			// Bare arg — treat as key=value if it contains '='
			if before, after, ok := strings.Cut(arg, "="); ok {
				key = before
				value = after
			} else {
				return fmt.Errorf("unexpected argument in flags: %s", arg)
			}
		}

		// If the key contains hyphens, check whether the underscore form
		// matches a known param and use that instead. This lets users write
		// --my-param on the CLI when the actual param is my_param, without
		// clobbering keys that legitimately contain hyphens.
		if strings.ContainsRune(key, '-') {
			alt := strings.ReplaceAll(key, "-", "_")
			if _, ok := paramTypes[alt]; ok {
				key = alt
			}
		}

		if source, ok := paramSources[key]; ok {
			return fmt.Errorf("parameter %q specified multiple times (as %s and as --%s flag)", key, source, key)
		}

		var parsed any
		if isBoolFlag {
			parsed = true
		} else {
			var err error
			parsed, err = parseParamValue(key, value, paramTypes)
			if err != nil {
				return err
			}
		}

		paramSources[key] = "flag"
		params[key] = parsed
	}

	return nil
}

// hasHelpFlag reports whether the unknown-flag slice contains a help flag
// (--help, -h, or --help=…/-h=…) so it can be intercepted before
// parseUnknownFlags rejects it as an unknown parameter. Bare "help" is
// intentionally not matched — it could be a legitimate value for another
// flag (e.g. --topic help).
func hasHelpFlag(unknown []string) bool {
	for _, arg := range unknown {
		switch arg {
		case "--help", "-h":
			return true
		}
		if strings.HasPrefix(arg, "--help=") || strings.HasPrefix(arg, "-h=") {
			return true
		}
	}
	return false
}

// looksLikeNumber checks if a string starting with "-" is a negative number
// rather than a flag. Returns true for values like "-5", "-3.14", "-.5".
func looksLikeNumber(s string) bool {
	if !strings.HasPrefix(s, "-") {
		return false
	}
	// Try parsing as integer first
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return true
	}
	// Try parsing as float
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return true
	}
	return false
}

// validateAdminCall fetches method introspection and validates the method and parameters
func validateAdminCall(ctx *Context, client *admin_v1alpha.AdminClient, app, method string, params map[string]interface{}) error {
	// Fetch available methods
	result, err := client.ListMethods(ctx, app)
	if err != nil {
		// If introspection fails, skip validation silently
		return nil
	}

	if result.HasError() && result.Error() != "" {
		// Introspection not available, skip validation
		return nil
	}

	if !result.HasMethods() || len(result.Methods()) == 0 {
		// No methods discovered, skip validation
		return nil
	}

	// Find the method in the list
	var methodInfo *admin_v1alpha.AdminMethod
	var availableMethods []string
	for _, m := range result.Methods() {
		availableMethods = append(availableMethods, m.Name())
		if m.Name() == method {
			methodInfo = m
			break
		}
	}

	// Check if method exists
	if methodInfo == nil {
		sort.Strings(availableMethods)
		return fmt.Errorf("unknown method %q\n\nAvailable methods:\n  %s\n\nUse --list for details",
			method, strings.Join(availableMethods, "\n  "))
	}

	// Validate parameters when the method declares them (even a declared-empty
	// list — that's a positive assertion of "no params" and any supplied param
	// is unknown). If params are not advertised at all, skip validation and let
	// the server decide.
	if methodInfo.HasParams() {
		expectedParams := make(map[string]*admin_v1alpha.AdminMethodParam)
		var requiredParams []string

		for _, p := range methodInfo.Params() {
			expectedParams[p.Name()] = p
			if p.HasRequired() && p.Required() {
				requiredParams = append(requiredParams, p.Name())
			}
		}

		// Check for missing required parameters
		var missingRequired []string
		for _, reqName := range requiredParams {
			if _, ok := params[reqName]; !ok {
				missingRequired = append(missingRequired, reqName)
			}
		}
		if len(missingRequired) > 0 {
			return fmt.Errorf("missing required parameter(s): %s\n\n%s",
				strings.Join(missingRequired, ", "),
				renderMethodHelpString(app, methodInfo))
		}

		// Check for unknown parameters
		var unknownParams []string
		for key := range params {
			if _, ok := expectedParams[key]; !ok {
				unknownParams = append(unknownParams, key)
			}
		}
		if len(unknownParams) > 0 {
			return fmt.Errorf("unknown parameter(s): %s\n\n%s",
				strings.Join(unknownParams, ", "),
				renderMethodHelpString(app, methodInfo))
		}
	}

	return nil
}

// methodDefinition converts an AdminMethod into a ui.Definition, populating the
// description (with optional category suffix) and one DefinitionDetail per param.
func methodDefinition(m *admin_v1alpha.AdminMethod) ui.Definition {
	def := ui.Definition{
		Term: m.Name(),
	}

	if m.HasDescription() && m.Description() != "" {
		desc := m.Description()
		if m.HasCategory() && m.Category() != "" {
			desc = fmt.Sprintf("%s (%s)", desc, m.Category())
		}
		def.Description = desc
	}

	if m.HasParams() && len(m.Params()) > 0 {
		for _, param := range m.Params() {
			paramType := "string"
			if param.HasParamType() && param.ParamType() != "" {
				paramType = param.ParamType()
			}

			def.Details = append(def.Details, ui.DefinitionDetail{
				Name:     param.Name(),
				Type:     paramType,
				Required: param.HasRequired() && param.Required(),
			})
		}
	}

	return def
}

// paramShapeNote returns a short faint line clarifying the method's parameter
// shape so callers can tell "method takes no parameters" apart from "method
// did not advertise its parameters". Returns the empty string when params are
// listed (the tree already speaks for itself).
func paramShapeNote(m *admin_v1alpha.AdminMethod) string {
	faint := lipgloss.NewStyle().Foreground(theme.Muted)
	switch {
	case !m.HasParams():
		return faint.Render("  (parameters not advertised by this method)")
	case len(m.Params()) == 0:
		return faint.Render("  (no parameters)")
	default:
		return ""
	}
}

// renderMethodHelpString returns the rendered method-help block (definition list
// plus usage hint) as a string. Callers that print to ctx should use
// renderMethodHelp; this form is used to embed help in error messages.
func renderMethodHelpString(appName string, m *admin_v1alpha.AdminMethod) string {
	defList := ui.NewDefinitionList([]ui.Definition{methodDefinition(m)})
	var sb strings.Builder
	sb.WriteString(defList.Render())
	if note := paramShapeNote(m); note != "" {
		sb.WriteString("\n")
		sb.WriteString(note)
	}
	sb.WriteString("\n")
	hint := ui.NewHint(fmt.Sprintf("Usage: miren admin -a %s %s [key=value ...]", appName, m.Name()))
	sb.WriteString(hint.Render())
	return sb.String()
}

// renderMethodHelp prints method help (definition list + usage hint) to ctx.
func renderMethodHelp(ctx *Context, appName string, m *admin_v1alpha.AdminMethod) {
	ctx.Printf("%s\n", renderMethodHelpString(appName, m))
}

func adminListMethods(ctx *Context, adminClient *admin_v1alpha.AdminClient, appName string) error {
	result, err := adminClient.ListMethods(ctx, appName)
	if err != nil {
		return fmt.Errorf("RPC error: %w", err)
	}

	if result.HasError() && result.Error() != "" {
		return fmt.Errorf("failed to list methods: %s", result.Error())
	}

	if !result.HasMethods() || len(result.Methods()) == 0 {
		ctx.Printf("No admin methods found (app may not support method discovery)\n")
		return nil
	}

	methods := result.Methods()
	sort.Slice(methods, func(i, j int) bool {
		return methods[i].Name() < methods[j].Name()
	})

	definitions := make([]ui.Definition, len(methods))
	for i, method := range methods {
		definitions[i] = methodDefinition(method)
	}

	defList := ui.NewDefinitionList(definitions,
		ui.WithDefinitionListTitle(fmt.Sprintf("Admin methods for %s", appName)),
	)
	ctx.Printf("%s\n", defList.Render())

	hint := ui.NewHint(fmt.Sprintf("Usage: miren admin -a %s <method> [key=value ...]", appName))
	ctx.Printf("%s\n", hint.Render())

	return nil
}

func adminMethodHelp(ctx *Context, adminClient *admin_v1alpha.AdminClient, appName, method string) error {
	result, err := adminClient.DescribeMethods(ctx, appName, []string{method})
	if err != nil {
		return fmt.Errorf("RPC error: %w", err)
	}

	if result.HasError() && result.Error() != "" {
		return fmt.Errorf("failed to discover methods: %s", result.Error())
	}

	if !result.HasMethods() || len(result.Methods()) == 0 {
		return fmt.Errorf("unknown method %q (use --list to see available methods)", method)
	}

	renderMethodHelp(ctx, appName, result.Methods()[0])
	return nil
}

// JSON syntax highlighting styles (similar to jq)
var (
	jsonKeyStyle    = lipgloss.NewStyle().Foreground(theme.Info)    // Blue for keys
	jsonStringStyle = lipgloss.NewStyle().Foreground(theme.Success) // Green for strings
	jsonNumberStyle = lipgloss.NewStyle().Foreground(theme.Info)    // Blue for numbers
	jsonBoolStyle   = lipgloss.NewStyle().Foreground(theme.Warning) // Yellow for booleans
	jsonNullStyle   = lipgloss.NewStyle().Foreground(theme.Muted)   // Gray for null
	jsonBracket     = lipgloss.NewStyle().Foreground(theme.Muted)   // Muted for brackets
)

// Pretty rendering styles
var (
	tableTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(theme.Info)
)

// highlightJSON recursively renders a JSON value with syntax highlighting
func highlightJSON(v interface{}, indent int) string {
	indentStr := strings.Repeat("  ", indent)
	nextIndent := strings.Repeat("  ", indent+1)

	switch val := v.(type) {
	case nil:
		return jsonNullStyle.Render("null")

	case bool:
		if val {
			return jsonBoolStyle.Render("true")
		}
		return jsonBoolStyle.Render("false")

	case float64:
		// Format nicely - no decimal for integers
		if val == float64(int64(val)) {
			return jsonNumberStyle.Render(fmt.Sprintf("%d", int64(val)))
		}
		return jsonNumberStyle.Render(fmt.Sprintf("%g", val))

	case string:
		escaped, _ := json.Marshal(val)
		return jsonStringStyle.Render(string(escaped))

	case []interface{}:
		if len(val) == 0 {
			return jsonBracket.Render("[]")
		}

		var sb strings.Builder
		sb.WriteString(jsonBracket.Render("["))
		sb.WriteString("\n")

		for i, item := range val {
			sb.WriteString(nextIndent)
			sb.WriteString(highlightJSON(item, indent+1))
			if i < len(val)-1 {
				sb.WriteString(",")
			}
			sb.WriteString("\n")
		}

		sb.WriteString(indentStr)
		sb.WriteString(jsonBracket.Render("]"))
		return sb.String()

	case map[string]interface{}:
		if len(val) == 0 {
			return jsonBracket.Render("{}")
		}

		// Sort keys for consistent output
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var sb strings.Builder
		sb.WriteString(jsonBracket.Render("{"))
		sb.WriteString("\n")

		for i, key := range keys {
			escapedKey, _ := json.Marshal(key)
			sb.WriteString(nextIndent)
			sb.WriteString(jsonKeyStyle.Render(string(escapedKey)))
			sb.WriteString(": ")
			sb.WriteString(highlightJSON(val[key], indent+1))
			if i < len(keys)-1 {
				sb.WriteString(",")
			}
			sb.WriteString("\n")
		}

		sb.WriteString(indentStr)
		sb.WriteString(jsonBracket.Render("}"))
		return sb.String()

	default:
		// Fallback for unexpected types
		b, _ := json.Marshal(val)
		return string(b)
	}
}

// renderPretty renders data in a human-friendly format
func renderPretty(v interface{}) string {
	var sb strings.Builder

	switch val := v.(type) {
	case map[string]interface{}:
		// Separate simple values from tables
		var simpleKeys []string
		var tableKeys []string
		tableData := make(map[string]struct {
			arr  []interface{}
			keys []string
		})

		for key, value := range val {
			if arr, ok := value.([]interface{}); ok {
				if keys := getUniformObjectKeys(arr); keys != nil {
					tableKeys = append(tableKeys, key)
					tableData[key] = struct {
						arr  []interface{}
						keys []string
					}{arr, keys}
					continue
				}
			}
			simpleKeys = append(simpleKeys, key)
		}

		// Sort both lists alphabetically
		sort.Strings(simpleKeys)
		sort.Strings(tableKeys)

		// Render simple values first using NamedValueList
		if len(simpleKeys) > 0 {
			items := make([]ui.NamedValue, len(simpleKeys))
			for i, key := range simpleKeys {
				items[i] = ui.NewNamedValue(formatKeyAsTitle(key), val[key])
			}
			nvl := ui.NewNamedValueList(items)
			sb.WriteString(nvl.Render())
			sb.WriteString("\n")
		}

		// Add spacing if we have both simple values and tables
		if len(simpleKeys) > 0 && len(tableKeys) > 0 {
			sb.WriteString("\n")
		}

		// Render tables
		for i, key := range tableKeys {
			data := tableData[key]
			sb.WriteString(tableTitleStyle.Render(formatKeyAsTitle(key)))
			sb.WriteString("\n")
			sb.WriteString(renderTable(data.arr, data.keys))
			if i < len(tableKeys)-1 {
				sb.WriteString("\n")
			}
		}

	case []interface{}:
		// Check if it's an array of uniform objects
		if keys := getUniformObjectKeys(val); keys != nil {
			sb.WriteString(renderTable(val, keys))
		} else {
			// Render as list
			for i, item := range val {
				fmt.Fprintf(&sb, "%d. ", i+1)
				sb.WriteString(renderPrettyValue(item))
				sb.WriteString("\n")
			}
		}

	default:
		sb.WriteString(renderPrettyValue(v))
		sb.WriteString("\n")
	}

	return sb.String()
}

// getUniformObjectKeys checks if all items in an array are objects with the same keys
// Returns the sorted keys if uniform, nil otherwise
func getUniformObjectKeys(arr []interface{}) []string {
	if len(arr) == 0 {
		return nil
	}

	var referenceKeys []string

	for i, item := range arr {
		obj, ok := item.(map[string]interface{})
		if !ok {
			return nil // Not an object
		}

		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		if i == 0 {
			referenceKeys = keys
		} else {
			// Compare keys
			if len(keys) != len(referenceKeys) {
				return nil
			}
			for j, k := range keys {
				if k != referenceKeys[j] {
					return nil
				}
			}
		}
	}

	return referenceKeys
}

// renderTable renders an array of uniform objects as a table using ui.Table
func renderTable(arr []interface{}, keys []string) string {
	if len(arr) == 0 || len(keys) == 0 {
		return ""
	}

	// Build headers from keys
	headers := make([]string, len(keys))
	for i, key := range keys {
		headers[i] = strings.ToUpper(formatKeyAsTitle(key))
	}

	// Build rows
	rows := make([]ui.Row, len(arr))
	for i, item := range arr {
		obj := item.(map[string]interface{})
		row := make(ui.Row, len(keys))
		for j, key := range keys {
			row[j] = formatCellValue(obj[key])
		}
		rows[i] = row
	}

	// Use ui.Table for consistent styling
	columns := ui.AutoSizeColumns(headers, rows, nil)
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	return table.Render() + "\n"
}

// formatKeyAsTitle converts a snake_case or camelCase key to a title
func formatKeyAsTitle(key string) string {
	// Replace underscores with spaces
	result := strings.ReplaceAll(key, "_", " ")

	// Insert space before capital letters (for camelCase)
	var sb strings.Builder
	for i, r := range result {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prev := rune(result[i-1])
			if prev >= 'a' && prev <= 'z' {
				sb.WriteRune(' ')
			}
		}
		sb.WriteRune(r)
	}

	// Title case
	return cases.Title(language.Und).String(sb.String())
}

// formatCellValue formats a value for display in a table cell
func formatCellValue(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return "-"
	case bool:
		if val {
			return "✓"
		}
		return "✗"
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%.2f", val)
	case string:
		return val
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}

// renderPrettyValue renders a single value for pretty output with styling
func renderPrettyValue(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return jsonNullStyle.Render("-")
	case bool:
		if val {
			return jsonBoolStyle.Render("yes")
		}
		return jsonBoolStyle.Render("no")
	case float64:
		if val == float64(int64(val)) {
			return jsonNumberStyle.Render(fmt.Sprintf("%d", int64(val)))
		}
		return jsonNumberStyle.Render(fmt.Sprintf("%g", val))
	case string:
		return jsonStringStyle.Render(val)
	case []interface{}:
		if len(val) == 0 {
			return jsonNullStyle.Render("(empty)")
		}
		// For arrays, show count
		return fmt.Sprintf("(%d items)", len(val))
	case map[string]interface{}:
		// For nested objects, show inline
		parts := make([]string, 0, len(val))
		for k, v := range val {
			parts = append(parts, fmt.Sprintf("%s=%s", k, formatCellValue(v)))
		}
		sort.Strings(parts)
		return strings.Join(parts, ", ")
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}
