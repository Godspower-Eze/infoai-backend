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

type Post struct {
	ent.Schema
}

func (Post) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.Enum("creation_mode").
			Values("user", "agent"),
		field.Enum("status").
			Values("draft", "scheduled", "publishing", "retry_wait", "partially_published", "published", "failed", "cancelled").
			Default("draft"),
		field.UUID("owner_id", uuid.UUID{}).
			Immutable(),
		field.UUID("x_account_id", uuid.UUID{}).
			Immutable(),
		field.Time("scheduled_at").
			Optional().
			Nillable(),
		field.Time("publish_requested_at").
			Optional().
			Nillable(),
		field.Time("published_at").
			Optional().
			Nillable(),
		field.Time("next_attempt_at").
			Optional().
			Nillable(),
		field.Int64("active_river_job_id").
			Optional().
			Nillable().
			NonNegative(),
		field.Int("attempt_count").
			Default(0).
			NonNegative(),
		field.String("last_error_code").
			Optional().
			Nillable().
			MaxLen(100),
		field.String("last_error_message").
			Optional().
			Nillable().
			MaxLen(1000),
		field.UUID("lease_token", uuid.UUID{}).
			Optional().
			Nillable(),
		field.String("lease_owner").
			Optional().
			Nillable().
			MaxLen(255),
		field.Time("lease_expires_at").
			Optional().
			Nillable(),
		field.Int64("lease_version").
			Default(0).
			NonNegative(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

func (Post) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("posts").
			Field("owner_id").
			Unique().
			Required().
			Immutable(),
		edge.From("x_account", XAccount.Type).
			Ref("posts").
			Field("x_account_id").
			Unique().
			Required().
			Immutable(),
		edge.To("items", PostItem.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("attempts", PublicationAttempt.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Post) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("owner_id", "created_at", "id"),
		index.Fields("status", "scheduled_at"),
		index.Fields("status", "next_attempt_at"),
		index.Fields("status", "lease_expires_at"),
	}
}
