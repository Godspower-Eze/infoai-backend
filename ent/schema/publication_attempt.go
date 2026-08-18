package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

type PublicationAttempt struct {
	ent.Schema
}

func (PublicationAttempt) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.Int("attempt_number").
			Positive().
			Immutable(),
		field.UUID("post_id", uuid.UUID{}).
			Immutable(),
		field.Enum("trigger").
			Values("immediate", "scheduled", "retry", "recovery").
			Immutable(),
		field.Enum("outcome").
			Values("pending", "succeeded", "retryable_failure", "permanent_failure").
			Default("pending"),
		field.Bool("retryable").
			Default(false),
		field.String("error_code").
			Optional().
			Nillable().
			MaxLen(100),
		field.String("error_message").
			Optional().
			Nillable().
			MaxLen(1000),
		field.Time("started_at").
			Default(time.Now).
			Immutable(),
		field.Time("finished_at").
			Optional().
			Nillable(),
	}
}

func (PublicationAttempt) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("post", Post.Type).
			Ref("attempts").
			Field("post_id").
			Unique().
			Required().
			Immutable(),
	}
}

func (PublicationAttempt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("post_id", "attempt_number").Unique(),
	}
}
