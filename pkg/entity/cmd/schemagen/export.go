package main

import (
	"fmt"
	"slices"
	"strings"

	entityexport "miren.dev/runtime/pkg/entity/export"
	"miren.dev/runtime/pkg/mapx"
)

type catalogEntry struct {
	attribute entityexport.Attribute
	ancestors []entityexport.Attribute
}

// GenerateExportContracts turns every exports.<target> declaration into the
// canonical artifact consumed by both runtime and cloud.
func GenerateExportContracts(sf *schemaFile) (map[string][]byte, error) {
	catalog, err := exportCatalog(sf)
	if err != nil {
		return nil, err
	}

	contracts := make(map[string][]byte, len(sf.Exports))
	for targetName, target := range mapx.StableOrder(sf.Exports) {
		if target.Marker == "" {
			return nil, fmt.Errorf("export target %s requires marker", targetName)
		}
		contract := entityexport.Contract{
			Version: entityexport.Version1,
			Target:  targetName,
			Marker:  target.Marker,
		}
		for kindName, policy := range mapx.StableOrder(target.Kinds) {
			if _, ok := sf.Kinds[kindName]; !ok {
				return nil, fmt.Errorf("export target %s references unknown kind %s", targetName, kindName)
			}
			lifecycle := entityexport.Lifecycle(policy.Lifecycle)
			if lifecycle != entityexport.LifecycleMirror && lifecycle != entityexport.LifecycleArchive {
				return nil, fmt.Errorf("export target %s kind %s has invalid lifecycle %q", targetName, kindName, policy.Lifecycle)
			}

			allowed := make(map[string]entityexport.Attribute)
			for _, id := range policy.Include {
				entry, ok := catalog[id]
				if !ok {
					return nil, fmt.Errorf("export target %s kind %s references unknown attribute %s", targetName, kindName, id)
				}
				if entry.attribute.Type == "component" {
					return nil, fmt.Errorf("export target %s kind %s must select component fields, not whole component %s", targetName, kindName, id)
				}
				for _, ancestor := range entry.ancestors {
					allowed[ancestor.ID] = ancestor
				}
				allowed[entry.attribute.ID] = entry.attribute
			}

			kind := entityexport.Kind{
				ID:         sf.Domain + "/kind." + kindName,
				Lifecycle:  lifecycle,
				Attributes: make([]entityexport.Attribute, 0, len(allowed)),
			}
			for _, attr := range allowed {
				kind.Attributes = append(kind.Attributes, attr)
			}
			slices.SortFunc(kind.Attributes, func(a, b entityexport.Attribute) int {
				return strings.Compare(a.ID, b.ID)
			})
			contract.Kinds = append(contract.Kinds, kind)
		}

		parsed, err := entityexport.Parse(mustJSON(contract))
		if err != nil {
			return nil, fmt.Errorf("compile export target %s: %w", targetName, err)
		}
		data, err := parsed.CanonicalJSON()
		if err != nil {
			return nil, fmt.Errorf("encode export target %s: %w", targetName, err)
		}
		contracts[targetName] = data
	}
	return contracts, nil
}

func mustJSON(contract entityexport.Contract) []byte {
	data, err := contract.CanonicalJSON()
	if err != nil {
		panic(err)
	}
	return data
}

func exportCatalog(sf *schemaFile) (map[string]catalogEntry, error) {
	catalog := map[string]catalogEntry{
		"db/short-id": {
			attribute: entityexport.Attribute{ID: "db/short-id", Type: "string"},
		},
	}
	for kindName, attrs := range mapx.StableOrder(sf.Kinds) {
		if err := collectExportAttrs(sf, catalog, attrs, kindName, false, "", nil); err != nil {
			return nil, err
		}
	}
	for componentName, attrs := range mapx.StableOrder(sf.Components) {
		if err := collectExportAttrs(sf, catalog, attrs, "component."+componentName, true, "", nil); err != nil {
			return nil, err
		}
	}
	return catalog, nil
}

func collectExportAttrs(
	sf *schemaFile,
	catalog map[string]catalogEntry,
	attrs schemaAttrs,
	defaultPrefix string,
	componentContext bool,
	parent string,
	ancestors []entityexport.Attribute,
) error {
	for name, attr := range mapx.StableOrder(attrs) {
		path := attr.Attr
		if path == "" {
			path = defaultPrefix + "." + name
		}
		id := sf.Domain + "/" + path
		entry := entityexport.Attribute{
			ID:     id,
			Type:   attr.Type,
			Parent: parent,
			Many:   attr.Many,
		}
		if attr.Type == "enum" {
			for _, choice := range attr.Choices {
				choicePrefix := name
				if componentContext {
					choicePrefix = path
				}
				entry.EnumValues = append(entry.EnumValues, sf.Domain+"/"+choicePrefix+"."+choice)
			}
		}
		if _, exists := catalog[id]; exists {
			return fmt.Errorf("duplicate export attribute %s", id)
		}
		catalog[id] = catalogEntry{attribute: entry, ancestors: slices.Clone(ancestors)}

		if attr.Type != "component" {
			continue
		}
		childPrefix := name
		if componentContext {
			childPrefix = path
		}
		childAncestors := append(slices.Clone(ancestors), entry)
		if err := collectExportAttrs(sf, catalog, attr.Attrs, childPrefix, componentContext, id, childAncestors); err != nil {
			return err
		}
	}
	return nil
}
