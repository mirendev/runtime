package appconfig

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestPublishedSchemaFieldsMatchConfig keeps the public editor schema in step
// with the TOML structs. Validation rules still belong to Validate; this test
// guards the easier-to-miss failure mode where a field is added to one source
// of truth and omitted from the other.
func TestPublishedSchemaFieldsMatchConfig(t *testing.T) {
	data, err := os.ReadFile("../docs/static/app-toml.schema.json")
	if err != nil {
		t.Fatal(err)
	}

	var schema struct {
		Properties map[string]any `json:"properties"`
		Defs       map[string]struct {
			Properties map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("invalid app.toml JSON Schema: %v", err)
	}

	tests := []struct {
		name   string
		typeOf reflect.Type
		got    map[string]any
		skip   map[string]bool
	}{
		{name: "root", typeOf: reflect.TypeFor[AppConfig](), got: schema.Properties, skip: map[string]bool{"post_import": true}},
		{name: "env", typeOf: reflect.TypeFor[AppEnvVar](), got: schema.Defs["env"].Properties},
		{name: "build", typeOf: reflect.TypeFor[BuildConfig](), got: schema.Defs["build"].Properties},
		{name: "service", typeOf: reflect.TypeFor[ServiceConfig](), got: schema.Defs["service"].Properties},
		{name: "concurrency", typeOf: reflect.TypeFor[ServiceConcurrencyConfig](), got: schema.Defs["concurrency"].Properties},
		{name: "port", typeOf: reflect.TypeFor[PortConfig](), got: schema.Defs["port"].Properties},
		{name: "disk", typeOf: reflect.TypeFor[DiskConfig](), got: schema.Defs["disk"].Properties},
		{name: "task", typeOf: reflect.TypeFor[TaskConfig](), got: schema.Defs["task"].Properties},
		{name: "addon", typeOf: reflect.TypeFor[AddonConfig](), got: schema.Defs["addon"].Properties},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := tomlFields(tt.typeOf, tt.skip)
			got := make([]string, 0, len(tt.got))
			for name := range tt.got {
				got = append(got, name)
			}
			sort.Strings(got)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("schema fields = %v, config fields = %v", got, want)
			}
		})
	}
}

func tomlFields(typ reflect.Type, skip map[string]bool) []string {
	fields := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("toml"), ",")[0]
		if name != "" && name != "-" && !skip[name] {
			fields = append(fields, name)
		}
	}
	sort.Strings(fields)
	return fields
}
