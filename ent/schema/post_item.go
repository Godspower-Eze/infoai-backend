package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

type PostItem struct {
	ent.Schema
}

func (PostItem) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.Int("position").
			NonNegative(),
		field.UUID("post_id", uuid.UUID{}).
			Immutable(),
		field.String("text").
			Default(""),
		field.String("x_post_id").
			Optional().
			Nillable().
			Unique(),
		field.Enum("submission_state").
			Values("not_started", "submitting", "published", "outcome_unknown").
			Default("not_started"),
		field.Time("submission_started_at").
			Optional().
			Nillable(),
		field.Time("outcome_confirmed_at").
			Optional().
			Nillable(),
		field.UUID("outcome_confirmed_by", uuid.UUID{}).
			Optional().
			Nillable(),
		field.String("confirmed_x_post_url").
			Optional().
			Nillable().
			MaxLen(2048),
		field.Time("published_at").
			Optional().
			Nillable(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

func (PostItem) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("post", Post.Type).
			Ref("items").
			Field("post_id").
			Unique().
			Required().
			Immutable(),
		edge.To("media_assets", MediaAsset.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (PostItem) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("post_id", "position").Unique(),
	}
}
