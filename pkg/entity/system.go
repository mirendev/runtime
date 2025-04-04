package entity

import (
	"errors"

	"miren.dev/runtime/pkg/entity/types"
)

const (
	DBId           Id = "db/id"
	Ident          Id = "db/ident"
	Doc            Id = "db/doc"
	Uniq           Id = "db/uniq"
	Cardinality    Id = "db/cardinality"
	Type           Id = "db/type"
	EnumValues     Id = "db/enumValues"
	EntityElemType Id = "db/elementType"

	UniqueId    Id = "db/unique.identity"
	UniqueValue Id = "db/unique.value"

	CardinalityOne  Id = "db/cardinality.one"
	CardinalityMany Id = "db/cardinality.many"

	TypeAny       Id = "db/type.any"
	TypeRef       Id = "db/type.ref"
	TypeStr       Id = "db/type.str"
	TypeKeyword   Id = "db/type.keyword"
	TypeInt       Id = "db/type.int"
	TypeFloat     Id = "db/type.float"
	TypeBool      Id = "db/type.bool"
	TypeTime      Id = "db/type.time"
	TypeEnum      Id = "db/type.enum"
	TypeArray     Id = "db/type.array"
	TypeDuration  Id = "db/type.duration"
	TypeComponent Id = "db/type.component"
	TypeLabel     Id = "db/type.label"

	Index Id = "db/index"

	EntityAttrs Id = "db/entity.attrs"
	EntityPreds Id = "db/entity.preds"

	Ensure Id = "db/ensure"

	AttrPred Id = "db/attr.pred"
	Program  Id = "db/program"

	EntityKind Id = "entity/kind"

	PredIP   Id = "db/pred.ip"
	PredCIDR Id = "db/pred.cidr"
)

func InitSystemEntities(save func(*Entity) error) error {
	ident := &Entity{
		Attrs: Attrs(
			Named(string(Ident)),
			Doc, "Entity identifier",
			Uniq, UniqueId,
			Cardinality, CardinalityOne,
			Type, TypeKeyword,
		),
	}

	doc := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(Doc),
			Doc, "Entity documentation",
			Cardinality, CardinalityOne,
			Type, TypeStr,
		),
	}

	uniq := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(Uniq),
			Doc, "Unique attribute value",
			Cardinality, CardinalityOne,
			Type, TypeRef,
		),
	}

	card := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(Cardinality),
			Doc, "Cardinality of an attribute",
			Cardinality, CardinalityOne,
			Type, TypeEnum,
			EntityElemType, TypeRef,
			EnumValues, ArrayValue(CardinalityOne, CardinalityMany),
		),
	}

	xtypes := ArrayValue(
		TypeAny, TypeRef, TypeStr, TypeKeyword,
		TypeInt, TypeFloat, TypeBool, TypeTime,
		TypeEnum, TypeArray, TypeLabel,
	)

	typ := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(Type),
			Doc, "Type of an attribute",
			Cardinality, CardinalityOne,
			Type, TypeRef,
		),
	}

	enumValues := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(EnumValues),
			Doc, "Enum values",
			Cardinality, CardinalityMany,
			Type, TypeArray,
			EntityElemType, TypeAny,
		),
	}

	enumType := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(EntityElemType),
			Doc, "Enum type",
			Cardinality, CardinalityOne,
			Type, TypeEnum,
			EntityElemType, TypeRef,
			EnumValues, xtypes,
		),
	}

	index := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(Index),
			Doc, "Index",
			Cardinality, CardinalityOne,
			Type, TypeBool,
		),
	}

	entityKind := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(EntityKind),
			Doc, "Entity kind",
			Cardinality, CardinalityOne,
			Type, TypeKeyword,
			Index, true,
		),
	}

	id := func(id Id, doc string) *Entity {
		return &Entity{
			Attrs: Attrs(
				Ident, types.Keyword(id),
				Doc, doc,
			),
		}
	}

	uniqueIdentity := id(UniqueId, "Unique identity")
	uniqueValue := id(UniqueValue, "Unique value")
	cardOne := id(CardinalityOne, "Cardinality one")
	cardMany := id(CardinalityMany, "Cardinality many")

	typeAny := id(TypeAny, "Any type")
	typeRef := id(TypeRef, "Reference type")
	typeStr := id(TypeStr, "String type")
	typeKW := id(TypeKeyword, "Keyword type")
	typeInt := id(TypeInt, "Integer type")
	typeFloat := id(TypeFloat, "Float type")
	typeBool := id(TypeBool, "Boolean type")
	typeTime := id(TypeTime, "Time type")
	typeEnum := id(TypeEnum, "Enum type")
	typeArray := id(TypeArray, "Array type")
	typeDuration := id(TypeDuration, "Duration type")
	typeComponent := id(TypeComponent, "Component type")
	typeLabel := id(TypeLabel, "Label type")

	attrPred := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(AttrPred),
			Doc, "Attribute predicate",
			Cardinality, CardinalityMany,
			Type, TypeRef,
		),
	}

	predIP := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(PredIP),
			Doc, "A program that checks if a value is an IP address",
			Program, "isIP(value)",
		),
	}

	predCidr := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(PredCIDR),
			Doc, "A program that checks if a value is an IP CIDR address",
			Program, "isCIDR(value)",
		),
	}

	entityAttrs := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(EntityAttrs),
			Doc, "Entity attributes",
			Cardinality, CardinalityOne,
			Type, TypeArray,
			EntityElemType, TypeRef,
		),
	}

	entityPreds := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(EntityPreds),
			Doc, "Entity predicates",
			Cardinality, CardinalityOne,
			Type, TypeArray,
			EntityElemType, TypeRef,
		),
	}

	entityEnsure := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(Ensure),
			Doc, "Ensure entity",
			Cardinality, CardinalityOne,
			Type, TypeRef,
		),
	}

	entities := []*Entity{
		ident, doc, uniq, card, typ, enumValues, enumType,
		uniqueIdentity, uniqueValue, cardOne, cardMany,
		typeAny, typeRef, typeStr, typeKW, typeInt, typeFloat, typeBool, typeTime, typeEnum,
		typeArray, typeDuration, typeComponent, typeLabel, index,
		attrPred, predIP, predCidr, entityAttrs, entityPreds, entityEnsure,
		entityKind,
	}

	for _, entity := range entities {
		if err := save(entity); err != nil {
			if !errors.Is(err, ErrEntityAlreadyExists) {
				return err
			}
		}
	}

	return nil
}
