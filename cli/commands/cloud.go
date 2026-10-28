package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"

	"miren.dev/mflags"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/ui"
)

// CloudOptions selects the logged-in identity (and hence the cloud origin).
// Embed it in an endpoint's options alongside normal mflags option tags.
type CloudOptions struct {
	FormatOptions
	Identity string `short:"i" long:"identity" description:"Cloud identity to use (required when more than one is configured)"`
}

func (o CloudOptions) CloudSettings() CloudOptions { return o }

type CloudDeleteOptions struct {
	CloudOptions
	Yes bool `short:"y" long:"yes" description:"Confirm this destructive operation without prompting"`
}

func (o CloudDeleteOptions) CloudYes() bool { return o.Yes }

// CloudEndpoint maps a CLI command to an HTTP operation. URL placeholders name
// required inputs. Inputs maps long option names (including positional fields)
// to "query:name" or "body:name"; "json:name" decodes a JSON value into the body, allowing arrays,
// objects, false, zero and explicit null. Add ",required" to require input, or
// ",include-empty" to send an empty string rather than omit it.
// "clear:name" maps a true boolean flag to an empty string in the body.
// Preserve maps omitted body fields to GET response selectors for replacement
// APIs. It preserves the fetched snapshot, not concurrent changes.
// JSON output retains the API document, including pagination/sync metadata.
// List and Columns select the text table; other responses use named values.
// Text selects a string to emit verbatim, suitable for scripts and config files.
type CloudEndpoint struct {
	Name, Synopsis string
	Method, Path   string
	Inputs         map[string]string
	Preserve       map[string]string
	List           string
	Text           string
	Columns        []CloudColumn
	Render         func(*Context, any) error // optional text renderer for composite responses
	Confirmation   string                    // destructive endpoints use CloudDeleteOptions
}

type CloudColumn struct {
	Header string
	Field  string // dot-separated JSON field path
}

// CloudCommand lets an endpoint and its conversions live in one definition
// file without a per-command authentication, HTTP, or rendering handler.
func CloudCommand[O interface{ CloudSettings() CloudOptions }](endpoint CloudEndpoint) *Cmd {
	cmd := Infer(endpoint.Name, endpoint.Synopsis, func(ctx *Context, opts O) error {
		// Validate inputs before organization resolution can send an HTTP request.
		if _, _, _, err := cloudInput(endpoint, reflect.ValueOf(opts), nil); err != nil {
			return err
		}
		settings := opts.CloudSettings()
		stdout := ctx.Stdout
		if settings.Format != "" && !strings.EqualFold(settings.Format, "text") && !settings.IsJSON() {
			return fmt.Errorf("unsupported cloud output format %q (use text or json)", settings.Format)
		}
		if settings.IsJSON() || endpoint.Text != "" {
			defer ctx.ProgressToStderr()()
		}
		var config *clientconfig.Config
		var err error
		if loader, ok := any(&opts).(interface {
			LoadConfig() (*clientconfig.Config, error)
		}); ok {
			config, err = loader.LoadConfig()
		} else {
			config, err = clientconfig.LoadConfig()
		}
		if err != nil && err != clientconfig.ErrNoConfig {
			return fmt.Errorf("failed to load configuration: %w", err)
		}
		name, err := pickCloudIdentity(ctx, config, settings.Identity, "")
		if err != nil {
			return err
		}
		identity, err := lookupIdentity(config, name)
		if err != nil {
			return err
		}
		client, err := cloudClient(config, name, identity)
		if err != nil {
			return err
		}
		needsConfirmation := false
		if endpoint.Confirmation != "" {
			confirmation, ok := any(opts).(interface{ CloudYes() bool })
			if !ok {
				return fmt.Errorf("destructive cloud endpoint requires CloudDeleteOptions")
			}
			if !confirmation.CloudYes() {
				if !ui.IsInteractive() {
					return fmt.Errorf("%s requires confirmation; use --yes to authorize it non-interactively", endpoint.Name)
				}
				needsConfirmation = true
			}
		}
		path, query, body, err := cloudInput(endpoint, reflect.ValueOf(opts), func(organization string) (string, error) {
			var response struct {
				Organizations []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"organizations"`
			}
			if err := client.Request(ctx, http.MethodGet, "/api/v1/organizations", nil, nil, &response); err != nil {
				return "", fmt.Errorf("failed to resolve organization %q: %w", organization, err)
			}
			if organization == "" {
				switch len(response.Organizations) {
				case 0:
					return "", fmt.Errorf("no Cloud organizations available; specify --organization (-o)")
				case 1:
					return response.Organizations[0].ID, nil
				default:
					if !ui.IsInteractive() {
						return "", fmt.Errorf("multiple Cloud organizations available; specify --organization (-o)")
					}
					items := make([]ui.PickerItem, 0, len(response.Organizations))
					for _, org := range response.Organizations {
						items = append(items, ui.TablePickerItem{ItemID: org.ID, Columns: []string{org.Name, org.ID}})
					}
					selected, err := ui.RunPicker(items, ui.WithTitle("Select a Cloud organization"), ui.WithHeaders([]string{"NAME", "ID"}))
					if err != nil {
						return "", err
					}
					if selected == nil {
						return "", fmt.Errorf("organization selection cancelled")
					}
					return selected.ID(), nil
				}
			}
			var id string
			for _, org := range response.Organizations {
				if org.Name != organization {
					continue
				}
				if id != "" {
					return "", fmt.Errorf("organization name %q is ambiguous; use an org- ID", organization)
				}
				id = org.ID
			}
			if id == "" {
				return "", fmt.Errorf("organization %q not found", organization)
			}
			return id, nil
		})
		if err != nil {
			return err
		}
		if needsConfirmation {
			confirmed, err := ui.Confirm(ui.WithMessage(endpoint.Confirmation+"\n"+path), ui.WithDefault(false))
			if err != nil {
				return err
			}
			if !confirmed {
				ctx.Info("Operation cancelled")
				return nil
			}
		}
		method := endpoint.Method
		if method == "" {
			method = http.MethodGet
		}
		if len(endpoint.Preserve) > 0 {
			fields := body.(map[string]any)
			var current any
			loaded := false
			for key, selector := range endpoint.Preserve {
				if _, supplied := fields[key]; supplied {
					continue
				}
				if !loaded {
					if err := client.Request(ctx, http.MethodGet, path, nil, nil, &current); err != nil {
						return err
					}
					loaded = true
				}
				parts := strings.Split(selector, ".")
				parent := current
				for _, part := range parts[:len(parts)-1] {
					parent = cloudField(parent, part)
				}
				object, ok := parent.(map[string]any)
				if !ok {
					return fmt.Errorf("unexpected response shape from GET %s for %q; refusing to overwrite", path, selector)
				}
				value := object[parts[len(parts)-1]]
				if value == nil {
					value = ""
				}
				fields[key] = value
			}
		}
		var response any
		if err := client.Request(ctx, method, path, query, body, &response); err != nil {
			return err
		}
		if settings.IsJSON() {
			return PrintJSONTo(stdout, response)
		}
		if endpoint.Text != "" {
			value, ok := cloudField(response, endpoint.Text).(string)
			if !ok || value == "" {
				return fmt.Errorf("cloud response field %q is not a nonempty string", endpoint.Text)
			}
			_, err := io.WriteString(stdout, value)
			return err
		}
		if endpoint.Render != nil {
			return endpoint.Render(ctx, response)
		}
		return endpoint.render(ctx, response)
	}, WithGroup(GroupClient))
	if cluster := cmd.FlagSet().Lookup("cluster"); cluster != nil {
		cluster.Usage = "Cloud cluster XID (not a local config name)"
	}
	return cmd
}

func cloudInput(endpoint CloudEndpoint, opts reflect.Value, resolveOrganization func(string) (string, error)) (string, url.Values, any, error) {
	path := endpoint.Path
	query := make(url.Values)
	var body map[string]any
	var visit func(reflect.Value) error
	visit = func(value reflect.Value) error {
		for i := 0; i < value.NumField(); i++ {
			field, info := value.Field(i), value.Type().Field(i)
			if info.Anonymous && field.Kind() == reflect.Struct {
				if err := visit(field); err != nil {
					return err
				}
				continue
			}
			flag := info.Tag.Get("long")
			tag := endpoint.Inputs[flag]
			if flag != "" && strings.Contains(endpoint.Path, "{"+flag+"}") {
				tag = "path:" + flag
			}
			if tag == "" {
				continue
			}
			mapping, required, _ := strings.Cut(tag, ",")
			kind, key, ok := strings.Cut(mapping, ":")
			if !ok || key == "" {
				return fmt.Errorf("invalid cloud mapping %q", tag)
			}
			if field.IsZero() && flag != "organization" {
				if required == "required" || kind == "path" {
					if info.Tag.Get("position") != "" {
						return fmt.Errorf("%s argument is required", flag)
					}
					return fmt.Errorf("--%s is required", info.Tag.Get("long"))
				}
				if required != "include-empty" {
					continue
				}
			}
			if flag == "organization" && resolveOrganization != nil && !strings.HasPrefix(field.String(), "org-") {
				id, err := resolveOrganization(field.String())
				if err != nil {
					return err
				}
				field = reflect.ValueOf(id)
			}
			switch kind {
			case "path":
				text := fmt.Sprint(field.Interface())
				if flag != "organization" && (text == "." || text == "..") {
					return fmt.Errorf("--%s cannot be a dot path segment", info.Tag.Get("long"))
				}
				path = strings.ReplaceAll(path, "{"+key+"}", url.PathEscape(text))
			case "query":
				query.Set(key, fmt.Sprint(field.Interface()))
			case "body", "json", "clear":
				input := field.Interface()
				if kind == "clear" {
					input = ""
				}
				if kind == "json" {
					decoder := json.NewDecoder(strings.NewReader(fmt.Sprint(input)))
					decoder.UseNumber()
					if err := decoder.Decode(&input); err != nil {
						return fmt.Errorf("--%s must be valid JSON: %w", flag, err)
					}
					var trailing any
					if err := decoder.Decode(&trailing); err != io.EOF {
						return fmt.Errorf("--%s must contain exactly one JSON value", flag)
					}
				}
				if body == nil {
					body = make(map[string]any)
				}
				if _, exists := body[key]; exists {
					return fmt.Errorf("--%s conflicts with another flag for %s", flag, key)
				}
				body[key] = input
			default:
				return fmt.Errorf("unknown cloud input mapping %q", kind)
			}
		}
		return nil
	}
	if err := visit(opts); err != nil {
		return "", nil, nil, err
	}
	if strings.ContainsAny(path, "{}") {
		return "", nil, nil, fmt.Errorf("unresolved cloud path %q", path)
	}
	if len(endpoint.Preserve) > 0 && len(body) == 0 {
		return "", nil, nil, fmt.Errorf("specify at least one field to update")
	}
	if body == nil {
		return path, query, nil, nil
	}
	return path, query, body, nil
}

func cloudField(value any, path string) any {
	for _, key := range strings.Split(path, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[key]
	}
	return value
}

func (e CloudEndpoint) render(ctx *Context, response any) error {
	if len(e.Columns) == 0 {
		if e.List != "" {
			response = cloudField(response, e.List)
		}
		return renderCloudValue(ctx, response)
	}
	items := []any{response}
	if e.List != "" {
		value := cloudField(response, e.List)
		if value == nil {
			items = nil
		} else {
			var ok bool
			items, ok = value.([]any)
			if !ok {
				return fmt.Errorf("cloud response field %q is not a list", e.List)
			}
		}
	}
	if len(items) == 0 {
		ctx.Printf("No results\n")
	} else {
		headers := make([]string, len(e.Columns))
		for i, col := range e.Columns {
			headers[i] = col.Header
		}
		rows := make([]ui.Row, 0, len(items))
		for _, item := range items {
			row := make(ui.Row, len(e.Columns))
			for i, col := range e.Columns {
				value := cloudField(item, col.Field)
				if value == nil {
					row[i] = "-"
				} else if text, ok := value.(string); ok {
					row[i] = text
				} else {
					data, err := json.Marshal(value)
					if err != nil {
						return err
					}
					row[i] = string(data)
				}
			}
			rows = append(rows, row)
		}
		ctx.Printf("%s\n", ui.NewTable(ui.WithColumns(ui.AutoSizeColumns(headers, rows, nil)), ui.WithRows(rows)).Render())
	}
	if cloudField(response, "truncated") == true {
		ctx.Printf("Results truncated by cloud\n")
	}
	if cursor := cloudField(response, "next_cursor"); cursor != nil && cursor != "" {
		ctx.Printf("Next cursor: %s\n", cursor)
	}
	return nil
}

func renderCloudValue(ctx *Context, value any) error {
	switch value := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var fields []ui.NamedValue
		for _, key := range keys {
			switch value[key].(type) {
			case map[string]any, []any:
				continue
			}
			fields = append(fields, ui.NewNamedValue(strings.ReplaceAll(key, "_", " "), value[key]))
		}
		if len(fields) > 0 {
			ctx.Printf("%s\n", ui.NewNamedValueList(fields).Render())
		}
		for _, key := range keys {
			switch child := value[key].(type) {
			case map[string]any, []any:
				ctx.Printf("\n%s\n", strings.ReplaceAll(key, "_", " "))
				if err := renderCloudValue(ctx, child); err != nil {
					return err
				}
			}
		}
	case []any:
		keys := make(map[string]bool)
		for _, item := range value {
			if object, ok := item.(map[string]any); ok {
				for key := range object {
					keys[key] = true
				}
			}
		}
		var headers []string
		for key := range keys {
			headers = append(headers, key)
		}
		sort.Strings(headers)
		if len(headers) > 0 {
			columns := make([]CloudColumn, 0, len(headers))
			for _, key := range headers {
				columns = append(columns, CloudColumn{strings.ToUpper(strings.ReplaceAll(key, "_", " ")), key})
			}
			return (CloudEndpoint{List: "items", Columns: columns}).render(ctx, map[string]any{"items": value})
		}
		if len(value) == 0 {
			ctx.Printf("No results\n")
		}
		for _, item := range value {
			ctx.Printf("%s\n", ui.NewNamedValueList([]ui.NamedValue{ui.NewNamedValue("Value", item)}).Render())
		}
	default:
		ctx.Printf("%s\n", ui.NewNamedValueList([]ui.NamedValue{ui.NewNamedValue("Result", value)}).Render())
	}
	return nil
}

type CloudOrganizationOptions struct {
	CloudOptions
	Organization string `short:"o" long:"organization" description:"Organization name or org- ID (defaults to your only organization)"`
}

type CloudAppOptions struct {
	CloudOrganizationOptions
	AppCentric
}

const cloudAppPath = "/api/v1/organizations/{organization}/apps/{cluster}/{app}"

func registerCloudCommands(d *mflags.Dispatcher) {
	registerCloudResourceCommands(d)
	d.Dispatch("cloud", Section("cloud", "Miren Cloud HTTP API commands", "", WithSectionGroup(GroupClient)))
	d.Dispatch("org", Section("org", "Cloud organization commands", "", WithSectionGroup(GroupClient)))
	d.Dispatch("cloud cluster", Section("cloud cluster", "Cloud cluster commands", ""))
	d.Dispatch("cloud app", Section("cloud app", "Cloud app visibility commands", ""))
	d.Dispatch("org list", CloudCommand[CloudOptions](CloudEndpoint{
		Name: "org list", Synopsis: "List your Miren Cloud organizations",
		Path: "/api/v1/organizations", List: "organizations",
		Columns: []CloudColumn{{"ID", "id"}, {"NAME", "name"}, {"ROLE", "role"}},
	}))
	d.Dispatch("org show", CloudCommand[struct {
		CloudOptions
		Organization string `position:"0" long:"organization" usage:"Organization name or org- ID (defaults to your only organization)"`
	}](CloudEndpoint{
		Name: "org show", Synopsis: "Show an organization and its members", Path: cloudOrgPath,
	}))
	d.Dispatch("cloud cluster list", CloudCommand[CloudOptions](CloudEndpoint{
		Name: "cloud cluster list", Synopsis: "List your clusters in Miren Cloud",
		Path: "/api/v1/users/clusters", List: "clusters",
		Columns: []CloudColumn{{"ID", "xid"}, {"NAME", "name"}, {"ORGANIZATION", "organization_name"}},
	}))
	d.Dispatch("cloud cluster approve", CloudCommand[struct {
		CloudOptions
		Registration string `position:"0" long:"registration" usage:"Registration ID from the server approval URL"`
		Token        string `long:"registration-token" description:"Signed token from the server approval URL"`
		Organization string `short:"o" long:"organization" description:"Organization name or org- ID (defaults to your only organization)"`
		Name         string `long:"name" description:"Optional cluster name override"`
	}](CloudEndpoint{
		Name: "cloud cluster approve", Synopsis: "Approve a pending cluster registration in Miren Cloud",
		Method: http.MethodPost, Path: "/api/v1/clusters/register/{registration}/approve",
		Inputs:  map[string]string{"registration-token": "body:registration_token,required", "organization": "body:organization_id,required", "name": "body:cluster_name"},
		Columns: []CloudColumn{{"STATUS", "status"}, {"ID", "cluster_id"}, {"NAME", "cluster_name"}, {"ORGANIZATION", "organization_id"}},
	}))
	appColumns := []CloudColumn{{"APP", "name"}, {"CLUSTER", "cluster_name"}, {"HEALTH", "sample.health"}, {"READY", "sample.ready_instances"}, {"DESIRED", "sample.desired_instances"}, {"OBSERVED", "sample.observed_at"}}
	d.Dispatch("cloud app list", CloudCommand[CloudOrganizationOptions](CloudEndpoint{
		Name: "cloud app list", Synopsis: "List mirrored apps across an organization",
		Path: "/api/v1/organizations/{organization}/apps", List: "apps", Columns: appColumns,
	}))
	d.Dispatch("cloud cluster apps", CloudCommand[CloudClusterOptions](CloudEndpoint{
		Name: "cloud cluster apps", Synopsis: "List mirrored apps in a cloud cluster",
		Path: "/api/v1/organizations/{organization}/clusters/{cluster}/apps", List: "apps", Columns: appColumns,
	}))
	d.Dispatch("cloud app show", CloudCommand[CloudAppOptions](CloudEndpoint{
		Name: "cloud app show", Synopsis: "Show mirrored app details, deployments and health",
		Path: cloudAppPath,
	}))
	d.Dispatch("cloud app history", CloudCommand[CloudAppOptions](CloudEndpoint{
		Name: "cloud app history", Synopsis: "List mirrored app deployment history (latest 50)",
		Path: cloudAppPath + "/deployments", List: "deployments",
		Columns: []CloudColumn{{"ID", "id"}, {"VERSION", "app_version_name"}, {"STATUS", "status"}, {"OPERATION", "operation"}, {"DEPLOYED", "deployed_at"}},
	}))
	d.Dispatch("cloud deployment show", CloudCommand[struct {
		CloudAppOptions
		Deployment string `position:"0" long:"deployment" usage:"Deployment ID"`
	}](CloudEndpoint{
		Name: "cloud deployment show", Synopsis: "Show a mirrored app deployment",
		Path: cloudAppPath + "/deployments/{deployment}",
	}))
	d.Dispatch("cloud app routes", CloudCommand[CloudAppOptions](CloudEndpoint{
		Name: "cloud app routes", Synopsis: "Show mirrored app routes and sync state",
		Path: cloudAppPath + "/routes",
	}))
	d.Dispatch("cloud app logs", CloudCommand[struct {
		CloudAppOptions
		Kind    string `long:"kind" description:"Log kind: app or build" default:"app"`
		Version string `long:"version" description:"Build version name (e.g. v3)"`
		Service string `long:"service" description:"Service to filter"`
		Since   string `long:"since" description:"Lookback duration (e.g. 30m, 2h)"`
		Limit   int    `long:"limit" description:"Maximum entries (cloud default 500, maximum 2000)"`
	}](CloudEndpoint{
		Name: "cloud app logs", Synopsis: "Fetch bounded live app or build logs through Miren Cloud",
		Path: cloudAppPath + "/logs", List: "entries",
		Inputs:  map[string]string{"kind": "query:kind", "version": "query:version", "service": "query:service", "since": "query:since", "limit": "query:limit"},
		Columns: []CloudColumn{{"TIMESTAMP", "timestamp"}, {"STREAM", "stream"}, {"LINE", "line"}},
	}))
}
