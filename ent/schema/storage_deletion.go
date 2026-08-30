package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type StorageDeletion struct {
	ent.Schema
}

func (StorageDeletion) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("storage_key").
			NotEmpty().
			Unique().
			Immutable(),
		field.Int("attempt_count").
			Default(0).
			NonNegative(),
		field.Time("next_attempt_at").
			Optional().
			Nillable(),
		field.String("last_error_code").
			Optional().
			Nillable().
			MaxLen(100),
		field.String("last_error_message").
			Optional().
			Nillable().
			MaxLen(1000),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}
