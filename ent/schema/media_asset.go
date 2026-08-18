package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

type MediaAsset struct {
	ent.Schema
}

func (MediaAsset) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.Int("position").
			NonNegative(),
		field.UUID("owner_id", uuid.UUID{}).
			Immutable(),
		field.UUID("post_item_id", uuid.UUID{}).
			Immutable(),
		field.String("storage_key").
			NotEmpty().
			Unique().
			Immutable(),
		field.String("original_filename").
			NotEmpty(),
		field.String("mime_type").
			NotEmpty(),
		field.Int64("size_bytes").
			NonNegative(),
		field.Bytes("sha256_checksum").
			MinLen(32).
			MaxLen(32),
		field.String("alt_text").
			Optional().
			Nillable(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
	}
}

func (MediaAsset) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("media_assets").
			Field("owner_id").
			Unique().
			Required().
			Immutable(),
		edge.From("post_item", PostItem.Type).
			Ref("media_assets").
			Field("post_item_id").
			Unique().
			Required().
			Immutable(),
	}
}

func (MediaAsset) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("post_item_id", "position").Unique(),
	}
}
