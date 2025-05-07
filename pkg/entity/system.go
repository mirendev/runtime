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
	TypeBytes     Id = "db/type.bytes"

	Index   Id = "db/index"
	Session Id = "db/session"

	AttrSession Id = "db/attr.session"

	EntityAttrs Id = "db/entity.attrs"
	EntityPreds Id = "db/entity.preds"

	Ensure Id = "db/ensure"

	AttrPred Id = "db/attr.pred"
	Program  Id = "db/program"

	EntityKind   Id = "entity/kind"
	EntitySchema Id = "entity/schema"

	SchemaKind Id = "schema/kind"

	PredIP   Id = "db/pred.ip"
	PredCIDR Id = "db/pred.cidr"

	Schema Id = "db/schema"

	TTL Id = "db/entity.ttl"
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
		TypeEnum, TypeArray, TypeLabel, TypeBytes,
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

	session := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(Session),
			Doc, "Values of this attribute are stored in a session",
			Cardinality, CardinalityOne,
			Type, TypeBool,
		),
	}

	attrSession := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(AttrSession),
			Doc, "The session id in use for this attribute",
			Cardinality, CardinalityMany,
			Type, TypeStr,
		),
	}

	ttl := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(TTL),
			Doc, "Time to live for this entity",
			Cardinality, CardinalityOne,
			Type, TypeDuration,
		),
	}

	entityKind := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(EntityKind),
			Doc, "Entity kind",
			Cardinality, CardinalityMany,
			Type, TypeRef,
			Index, true,
		),
	}

	schemaKind := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(SchemaKind),
			Doc, "A kind that is defined by the schema entity",
			Cardinality, CardinalityMany,
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
	typeBytes := id(TypeBytes, "Bytes type")

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

	entitySchema := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(Schema),
			Doc, "An encoded Schema",
			Cardinality, CardinalityOne,
			Type, TypeBytes,
		),
	}

	entityESchema := &Entity{
		Attrs: Attrs(
			Ident, types.Keyword(EntitySchema),
			Doc, "A reference to the schema used by the entity",
			Cardinality, CardinalityOne,
			Type, TypeRef,
		),
	}

	entities := []*Entity{
		ident, doc, uniq, card, typ, enumValues, enumType,
		uniqueIdentity, uniqueValue, cardOne, cardMany,
		typeAny, typeRef, typeStr, typeKW, typeInt, typeFloat, typeBool, typeTime, typeEnum,
		typeArray, typeDuration, typeComponent, typeLabel, typeBytes, index, session, ttl,
		attrSession,
		attrPred, predIP, predCidr, entityAttrs, entityPreds, entityEnsure,
		entityKind, entitySchema, entityESchema, schemaKind,
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
