package query

import (
	"reflect"
	"strings"
	"time"
)

// Capabilities is a versioned, server-generated query reference. Authorization
// is evaluated for the requesting certificate; runtime dependencies are not probed.
type Capabilities struct {
	Version     int                   `json:"version"`
	OS          string                `json:"os"`
	Arch        string                `json:"arch"`
	Syntax      string                `json:"syntax"`
	Notes       []string              `json:"notes"`
	Sources     []SourceCapability    `json:"sources"`
	Aggregates  []AggregateCapability `json:"aggregates"`
	Limits      CapabilityLimits      `json:"limits"`
	EventFields []FieldCapability     `json:"event_fields"`
}

type SourceCapability struct {
	Name              string              `json:"name"`
	Description       string              `json:"description"`
	Modes             []string            `json:"modes"` // events, snapshot, aggregate
	PlatformSupported bool                `json:"platform_supported"`
	Authorized        bool                `json:"authorized"`
	UnavailableReason string              `json:"unavailable_reason,omitempty"`
	Requirements      []string            `json:"requirements"`
	Fields            []FieldCapability   `json:"fields"`
	Filters           []FilterCapability  `json:"filters"`
	GroupByFields     []string            `json:"group_by_fields"`
	NumericFields     []string            `json:"numeric_fields"`
	Examples          []string            `json:"examples"`
	Sampling          *SamplingCapability `json:"sampling,omitempty"`
}

// FieldCapability describes an output JSON path, not necessarily a DSL field.
type FieldCapability struct {
	Path         string `json:"path"`
	Type         string `json:"type"`
	QueryField   string `json:"query_field,omitempty"` // DSL alias when filterable/groupable.
	Optional     bool   `json:"optional,omitempty"`
	Unit         string `json:"unit,omitempty"`
	Description  string `json:"description,omitempty"`
	Aggregatable bool   `json:"aggregatable"` // At least one field-taking aggregate; numeric_fields/semantics constrain which.
}

type FilterCapability struct {
	Field       string   `json:"field"`
	Type        string   `json:"type"`
	Operators   []string `json:"operators"`
	Values      []string `json:"values,omitempty"`
	Modes       []string `json:"modes"`
	Description string   `json:"description"`
}

type AggregateCapability struct {
	Name        string `json:"name"`
	Syntax      string `json:"syntax"`
	FieldType   string `json:"field_type"` // none, scalar, number (event metrics remain integers)
	Description string `json:"description"`
}

type CapabilityLimits struct {
	QueryBytes              int    `json:"query_bytes"`
	MaxWindow               string `json:"max_window"`
	GroupByFields           int    `json:"group_by_fields"`
	Groups                  int    `json:"groups"`
	RetainedAggregateValues int    `json:"retained_aggregate_values"`
	AggregateMetrics        int    `json:"aggregate_metrics"`
	TracepointFields        int    `json:"tracepoint_fields"`
	SyscallFilters          int    `json:"syscall_filters"`
	PacketCaptureBytes      int    `json:"packet_capture_bytes"`
	DefaultMonitorTTL       string `json:"default_monitor_ttl"`
	RegisteredMonitors      int    `json:"registered_monitors"`
	MonitorRingEvents       int    `json:"monitor_ring_events"`
	MonitorEventBytes       int    `json:"monitor_event_bytes"`
}

// outputFields derives paths and types from the engine wire models.
func outputFields(value any, prefix string) []FieldCapability {
	var fields []FieldCapability
	var visit func(reflect.Type, string, bool)
	visit = func(typ reflect.Type, path string, optional bool) {
		if typ.Kind() == reflect.Pointer {
			visit(typ.Elem(), path, true)
			return
		}
		if typ == reflect.TypeOf(time.Time{}) {
			fields = append(fields, FieldCapability{Path: path, Type: "timestamp", Optional: optional, Description: "RFC3339 UTC timestamp"})
			return
		}
		if typ.Kind() == reflect.Struct {
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				tag := strings.Split(field.Tag.Get("json"), ",")
				if tag[0] == "" || tag[0] == "-" {
					continue
				}
				name := tag[0]
				if path != "" {
					name = path + "." + name
				}
				visit(field.Type, name, optional || strings.Contains(field.Tag.Get("json"), ",omitempty"))
			}
			return
		}
		if typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Struct {
			visit(typ.Elem(), path+"[]", optional)
			return
		}
		kind := "integer"
		switch typ.Kind() {
		case reflect.String:
			kind = "string"
		case reflect.Float32, reflect.Float64:
			kind = "number"
		case reflect.Slice:
			kind = "array<string>"
			if typ.Elem().Kind() == reflect.Uint8 {
				kind = "base64"
			}
		case reflect.Map:
			kind = "object<integer>"
		}
		fields = append(fields, FieldCapability{Path: path, Type: kind, Optional: optional})
	}
	visit(reflect.TypeOf(value), prefix, false)
	return fields
}
