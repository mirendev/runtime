package schema

import (
	"fmt"

	"miren.dev/runtime/pkg/entity"
)

// ConvergencePlan derives historical-to-canonical value rewrites from every
// encoded schema registered by the running binary. Encoded schemas carry read
// aliases, while installed attribute schemas describe only the canonical
// target state.
func ConvergencePlan() (entity.ConvergencePlan, error) {
	var rules []entity.ConvergenceRule

	var collectFields func(*entity.EncodedSchema) error
	collectFields = func(encoded *entity.EncodedSchema) error {
		for _, field := range encoded.Fields {
			for member, aliases := range field.EnumLegacyValues {
				canonicalID, ok := field.EnumValues[member]
				if !ok {
					return fmt.Errorf("enum field %s has legacy values for unknown member %q", field.Id, member)
				}
				canonical := entity.RefValue(canonicalID)
				for _, alias := range aliases {
					if alias.Equal(canonical) {
						continue
					}
					rules = append(rules, entity.ConvergenceRule{
						Attribute: field.Id,
						From:      alias,
						To:        canonical,
					})
				}
			}

			if field.Component != nil {
				if err := collectFields(field.Component); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for _, versions := range encodedRegistry {
		for _, entry := range versions {
			for _, encoded := range entry.schema.Kinds {
				if err := collectFields(encoded); err != nil {
					return entity.ConvergencePlan{}, err
				}
			}
		}
	}

	return entity.BuildConvergencePlan(rules)
}
