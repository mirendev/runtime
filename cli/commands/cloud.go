package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
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
	Force bool `short:"f" long:"force" description:"Skip confirmation of this destructive operation"`
}

func (o CloudDeleteOptions) CloudForce() bool { return o.Force }

// CloudEndpoint maps a CLI command to an HTTP operation. URL placeholders name
// required flags. Inputs maps other long flag names to "query:name" or
// "body:name"; "json:name" decodes a JSON value into the body, allowing arrays,
// objects, false, zero and explicit null. Add ",required" to require input, or
// ",include-empty" to send an empty string rather than omit it.
// "clear:name" maps a true boolean flag to an empty string in the body.
// Preserve maps omitted body fields to GET response selectors for replacement
// APIs. It preserves the fetched snapshot, not concurrent changes.
// JSON output retains the API document, including pagination/sync metadata.
// List and Columns select the text table; without Columns, text is pretty JSON.
type CloudEndpoint struct {
	Name, Synopsis string
	Method, Path   string
	Inputs         map[string]string
	Preserve       map[string]string
	List           string
	Columns        []CloudColumn
	Confirmation   string // destructive endpoints use CloudDeleteOptions
}

type CloudColumn struct {
	Header string
	Field  string // dot-separated JSON field path
}

// CloudCommand lets an endpoint and its conversions live in one definition
// file without a per-command authentication, HTTP, or rendering handler.
func CloudCommand[O interface{ CloudSettings() CloudOptions }](endpoint CloudEndpoint) *Cmd {
	return Infer(endpoint.Name, endpoint.Synopsis, func(ctx *Context, opts O) error {
		path, query, body, err := cloudInput(endpoint, reflect.ValueOf(opts))
		if err != nil {
			return err
		}
		settings := opts.CloudSettings()
		stdout := ctx.Stdout
		if settings.Format != "" && !strings.EqualFold(settings.Format, "text") && !settings.IsJSON() {
			return fmt.Errorf("unsupported cloud output format %q (use text or json)", settings.Format)
		}
		if settings.IsJSON() {
			defer ctx.ProgressToStderr()()
		}
		config, err := clientconfig.LoadConfig()
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
		if endpoint.Confirmation != "" {
			force, ok := any(opts).(interface{ CloudForce() bool })
			if !ok {
				return fmt.Errorf("destructive cloud endpoint requires CloudDeleteOptions")
			}
			if !force.CloudForce() {
				if !ui.IsInteractive() {
					return fmt.Errorf("%s requires confirmation; use --force to authorize it non-interactively", endpoint.Name)
				}
				confirmed, err := ui.Confirm(ui.WithMessage(endpoint.Confirmation+"\n"+path), ui.WithDefault(false))
				if err != nil {
					return err
				}
				if !confirmed {
					ctx.Info("Operation cancelled")
					return nil
				}
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
				value := cloudField(current, selector)
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
		if settings.IsJSON() || len(endpoint.Columns) == 0 {
			return PrintJSONTo(stdout, response)
		}
		return endpoint.render(ctx, response)
	}, WithGroup(GroupClient))
}

func cloudInput(endpoint CloudEndpoint, opts reflect.Value) (string, url.Values, any, error) {
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
			if field.IsZero() {
				if required == "required" || kind == "path" {
					return fmt.Errorf("--%s is required", info.Tag.Get("long"))
				}
				if required != "include-empty" {
					continue
				}
			}
			switch kind {
			case "path":
				text := fmt.Sprint(field.Interface())
				if text == "." || text == ".." {
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

type CloudOrganizationOptions struct {
	CloudOptions
	Organization string `long:"organization" description:"Organization XID (from cloud org list)"`
}

type CloudAppOptions struct {
	CloudOrganizationOptions
	Cluster string `long:"cluster" description:"Cloud cluster XID (not a local config name)"`
	App     string `long:"app" description:"Application name"`
}

const cloudAppPath = "/api/v1/organizations/{organization}/apps/{cluster}/{app}"

func registerCloudCommands(d *mflags.Dispatcher) {
	d.Dispatch("cloud", Section("cloud", "Miren Cloud HTTP API commands", "", WithSectionGroup(GroupClient)))
	d.Dispatch("cloud org", Section("cloud org", "Cloud organization commands", ""))
	d.Dispatch("cloud cluster", Section("cloud cluster", "Cloud cluster commands", ""))
	d.Dispatch("cloud app", Section("cloud app", "Cloud app visibility commands", ""))
	d.Dispatch("cloud org list", CloudCommand[CloudOptions](CloudEndpoint{
		Name: "cloud org list", Synopsis: "List your Miren Cloud organizations",
		Path: "/api/v1/organizations", List: "organizations",
		Columns: []CloudColumn{{"ID", "id"}, {"NAME", "name"}, {"ROLE", "role"}},
	}))
	d.Dispatch("cloud cluster list", CloudCommand[CloudOptions](CloudEndpoint{
		Name: "cloud cluster list", Synopsis: "List your clusters in Miren Cloud",
		Path: "/api/v1/users/clusters", List: "clusters",
		Columns: []CloudColumn{{"ID", "xid"}, {"NAME", "name"}, {"ORGANIZATION", "organization_name"}},
	}))
	d.Dispatch("cloud cluster approve", CloudCommand[struct {
		CloudOptions
		Registration string `long:"registration" description:"Registration ID from the server approval URL"`
		Token        string `long:"registration-token" description:"Signed token from the server approval URL"`
		Organization string `long:"organization" description:"Organization XID to register the cluster in"`
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
	d.Dispatch("cloud cluster apps", CloudCommand[struct {
		CloudOrganizationOptions
		Cluster string `long:"cluster" description:"Cloud cluster XID"`
	}](CloudEndpoint{
		Name: "cloud cluster apps", Synopsis: "List mirrored apps in a cloud cluster",
		Path: "/api/v1/organizations/{organization}/clusters/{cluster}/apps", List: "apps", Columns: appColumns,
	}))
	d.Dispatch("cloud app show", CloudCommand[CloudAppOptions](CloudEndpoint{
		Name: "cloud app show", Synopsis: "Show mirrored app details, deployments and health",
		Path: cloudAppPath,
	}))
	d.Dispatch("cloud app deployments", CloudCommand[CloudAppOptions](CloudEndpoint{
		Name: "cloud app deployments", Synopsis: "List mirrored app deployment history (latest 50)",
		Path: cloudAppPath + "/deployments", List: "deployments",
		Columns: []CloudColumn{{"ID", "id"}, {"VERSION", "app_version_name"}, {"STATUS", "status"}, {"OPERATION", "operation"}, {"DEPLOYED", "deployed_at"}},
	}))
	d.Dispatch("cloud app deployment", CloudCommand[struct {
		CloudAppOptions
		Deployment string `long:"deployment" description:"Deployment ID"`
	}](CloudEndpoint{
		Name: "cloud app deployment", Synopsis: "Show a mirrored app deployment",
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
