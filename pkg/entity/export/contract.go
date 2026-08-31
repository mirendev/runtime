// Package export defines the generated contract for entities that may leave
// the runtime. The source schema owns the contract; this package provides the
// small runtime needed to identify and filter an entity against it.
package export

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"miren.dev/runtime/pkg/entity"
)

const Version1 = 1

var ErrKindNotExported = errors.New("entity kind is not exported")

type Lifecycle string

const (
	LifecycleMirror  Lifecycle = "mirror"
	LifecycleArchive Lifecycle = "archive"
)

// Contract is the language-neutral description generated from schema.yml.
// Kinds and attributes are slices rather than maps so its JSON encoding is a
// stable, reviewable artifact and therefore a suitable digest input.
type Contract struct {
	Version int    `json:"version"`
	Target  string `json:"target"`
	Marker  string `json:"marker"`
	Kinds   []Kind `json:"kinds"`

	digest   string
	policies map[entity.Id]*Policy
}

type Kind struct {
	ID         string      `json:"id"`
	Lifecycle  Lifecycle   `json:"lifecycle"`
	Attributes []Attribute `json:"attributes"`
}

// Attribute describes one allowed value. Parent is set for fields nested in a
// component. Ancestor component attributes are included automatically by the
// generator, so a validator can reconstruct the allowed tree without knowing
// the runtime's source schema.
type Attribute struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	Parent     string   `json:"parent,omitempty"`
	Many       bool     `json:"many,omitempty"`
	EnumValues []string `json:"enum_values,omitempty"`
}

type Policy struct {
	Kind      entity.Id
	Lifecycle Lifecycle
	allowed   map[entity.Id]Attribute
	children  map[entity.Id]map[entity.Id]struct{}
}

// Parse validates and compiles a generated contract.
func Parse(data []byte) (*Contract, error) {
	var contract Contract
	if err := json.Unmarshal(data, &contract); err != nil {
		return nil, fmt.Errorf("decode entity export contract: %w", err)
	}
	if contract.Version != Version1 {
		return nil, fmt.Errorf("unsupported entity export contract version %d", contract.Version)
	}
	if contract.Target == "" || contract.Marker == "" {
		return nil, errors.New("entity export contract requires target and marker")
	}

	contract.policies = make(map[entity.Id]*Policy, len(contract.Kinds))
	for _, kind := range contract.Kinds {
		kindID := entity.Id(kind.ID)
		if kindID == "" {
			return nil, errors.New("entity export contract contains an empty kind")
		}
		if kind.Lifecycle != LifecycleMirror && kind.Lifecycle != LifecycleArchive {
			return nil, fmt.Errorf("entity export kind %s has invalid lifecycle %q", kind.ID, kind.Lifecycle)
		}
		if _, exists := contract.policies[kindID]; exists {
			return nil, fmt.Errorf("entity export contract contains duplicate kind %s", kind.ID)
		}

		policy := &Policy{
			Kind:      kindID,
			Lifecycle: kind.Lifecycle,
			allowed:   make(map[entity.Id]Attribute, len(kind.Attributes)),
			children:  make(map[entity.Id]map[entity.Id]struct{}),
		}
		for _, attr := range kind.Attributes {
			id := entity.Id(attr.ID)
			if id == "" || attr.Type == "" {
				return nil, fmt.Errorf("entity export kind %s contains an incomplete attribute", kind.ID)
			}
			if _, exists := policy.allowed[id]; exists {
				return nil, fmt.Errorf("entity export kind %s contains duplicate attribute %s", kind.ID, id)
			}
			if _, err := valueKind(attr.Type); err != nil {
				return nil, fmt.Errorf("entity export attribute %s: %w", id, err)
			}
			policy.allowed[id] = attr
			if attr.Parent != "" {
				parent := entity.Id(attr.Parent)
				if policy.children[parent] == nil {
					policy.children[parent] = make(map[entity.Id]struct{})
				}
				policy.children[parent][id] = struct{}{}
			}
		}
		for parent := range policy.children {
			attr, ok := policy.allowed[parent]
			if !ok || attr.Type != "component" {
				return nil, fmt.Errorf("entity export kind %s has children for non-component %s", kind.ID, parent)
			}
		}
		contract.policies[kindID] = policy
	}

	canonical, err := contract.CanonicalJSON()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(canonical)
	contract.digest = "sha256:" + hex.EncodeToString(sum[:])
	return &contract, nil
}

func MustParse[T ~string | ~[]byte](data T) *Contract {
	contract, err := Parse([]byte(data))
	if err != nil {
		panic(err)
	}
	return contract
}

// CanonicalJSON returns the stable wire artifact. It deliberately ignores the
// compiled indexes and digest cached on Contract.
func (c *Contract) CanonicalJSON() ([]byte, error) {
	copy := Contract{
		Version: c.Version,
		Target:  c.Target,
		Marker:  c.Marker,
		Kinds:   slices.Clone(c.Kinds),
	}
	slices.SortFunc(copy.Kinds, func(a, b Kind) int { return strings.Compare(a.ID, b.ID) })
	for i := range copy.Kinds {
		copy.Kinds[i].Attributes = slices.Clone(copy.Kinds[i].Attributes)
		slices.SortFunc(copy.Kinds[i].Attributes, func(a, b Attribute) int {
			return strings.Compare(a.ID, b.ID)
		})
		for j := range copy.Kinds[i].Attributes {
			copy.Kinds[i].Attributes[j].EnumValues = slices.Clone(copy.Kinds[i].Attributes[j].EnumValues)
			slices.Sort(copy.Kinds[i].Attributes[j].EnumValues)
		}
	}
	return json.Marshal(copy)
}

func (c *Contract) Digest() string { return c.digest }

func (c *Contract) MarkerID() entity.Id { return entity.Id(c.Marker) }

func (c *Contract) Policy(kind entity.Id) (*Policy, bool) {
	policy, ok := c.policies[kind]
	return policy, ok
}

func (c *Contract) PolicyFor(source entity.AttrGetter) (*Policy, bool) {
	attr, ok := source.Get(entity.EntityKind)
	if !ok || attr.Value.Kind() != entity.KindId {
		return nil, false
	}
	return c.Policy(attr.Value.Id())
}

// Filter returns a complete entity containing the source identity metadata and
// only business attributes admitted by the policy for its kind. A malformed
// allowed value is rejected rather than copied through under an unexpected
// shape.
func (c *Contract) Filter(source *entity.Entity) (*entity.Entity, *Policy, error) {
	policy, ok := c.PolicyFor(source)
	if !ok {
		return nil, nil, ErrKindNotExported
	}

	mandatory := map[entity.Id]struct{}{
		entity.DBId:       {},
		entity.Revision:   {},
		entity.CreatedAt:  {},
		entity.UpdatedAt:  {},
		entity.EntityKind: {},
	}
	filtered := make([]entity.Attr, 0, len(source.Attrs()))
	for _, attr := range source.Attrs() {
		if _, ok := mandatory[attr.ID]; ok {
			filtered = append(filtered, attr.Clone())
			continue
		}
		spec, ok := policy.allowed[attr.ID]
		if !ok {
			continue
		}
		got, err := filterAttribute(attr, spec, policy)
		if err != nil {
			return nil, nil, err
		}
		filtered = append(filtered, got)
	}
	return entity.New(filtered), policy, nil
}

func filterAttribute(attr entity.Attr, spec Attribute, policy *Policy) (entity.Attr, error) {
	want, err := valueKind(spec.Type)
	if err != nil {
		return entity.Attr{}, err
	}
	if attr.Value.Kind() != want {
		return entity.Attr{}, fmt.Errorf("export attribute %s has %s value, want %s", attr.ID, attr.Value.Kind(), want)
	}
	if want != entity.KindComponent {
		return attr.Clone(), nil
	}

	children := policy.children[attr.ID]
	component := attr.Value.Component()
	filtered := make([]entity.Attr, 0, len(component.Attrs()))
	for _, child := range component.Attrs() {
		if _, ok := children[child.ID]; !ok {
			continue
		}
		childSpec := policy.allowed[child.ID]
		got, err := filterAttribute(child, childSpec, policy)
		if err != nil {
			return entity.Attr{}, err
		}
		filtered = append(filtered, got)
	}
	return entity.Component(attr.ID, filtered), nil
}

func valueKind(typ string) (entity.ValueKind, error) {
	switch typ {
	case "bool":
		return entity.KindBool, nil
	case "duration":
		return entity.KindDuration, nil
	case "float":
		return entity.KindFloat64, nil
	case "int":
		return entity.KindInt64, nil
	case "string":
		return entity.KindString, nil
	case "time":
		return entity.KindTime, nil
	case "ref":
		return entity.KindId, nil
	case "enum":
		return entity.KindId, nil
	case "keyword":
		return entity.KindKeyword, nil
	case "component":
		return entity.KindComponent, nil
	case "label":
		return entity.KindLabel, nil
	case "bytes":
		return entity.KindBytes, nil
	default:
		return entity.KindAny, fmt.Errorf("unsupported value type %q", typ)
	}
}
