package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	entschema "entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type XAccount struct {
	ent.Schema
}

func (XAccount) Annotations() []entschema.Annotation {
	return []entschema.Annotation{
		entsql.Annotation{Table: "x_accounts"},
	}
}

func (XAccount) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("x_user_id").
			NotEmpty().
			Unique().
			Immutable(),
		field.String("username").
			NotEmpty(),
		field.String("display_name").
			Default(""),
		field.String("profile_image_url").
			Optional().
			Nillable(),
		field.Bytes("access_token").
			Sensitive(),
		field.Bytes("refresh_token").
			Optional().
			Nillable().
			Sensitive(),
		field.Time("token_expiry"),
		field.JSON("scopes", []string{}).
			Default([]string{}),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

func (XAccount) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("x_accounts").
			Unique().
			Required().
			Immutable(),
	}
}
