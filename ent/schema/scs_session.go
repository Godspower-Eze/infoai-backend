package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type SCSSession struct {
	ent.Schema
}

func (SCSSession) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			StorageKey("token").
			Immutable(),
		field.Bytes("data"),
		field.Time("expiry"),
	}
}

func (SCSSession) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("expiry"),
	}
}

func (SCSSession) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "sessions"},
	}
}
