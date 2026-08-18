package schema

import (
	"slices"
	"testing"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type fieldDescriptor interface {
	Descriptor() *field.Descriptor
}

type edgeDescriptor interface {
	Descriptor() *edge.Descriptor
}

type indexDescriptor interface {
	Descriptor() *index.Descriptor
}

func fieldsByName(fields []ent.Field) map[string]*field.Descriptor {
	descriptors := make(map[string]*field.Descriptor, len(fields))
	for _, item := range fields {
		descriptor := item.(fieldDescriptor).Descriptor()
		descriptors[descriptor.Name] = descriptor
	}
	return descriptors
}

func edgesByName(edges []ent.Edge) map[string]*edge.Descriptor {
	descriptors := make(map[string]*edge.Descriptor, len(edges))
	for _, item := range edges {
		descriptor := item.(edgeDescriptor).Descriptor()
		descriptors[descriptor.Name] = descriptor
	}
	return descriptors
}

func hasUniqueIndex(indexes []ent.Index, fields, edges []string) bool {
	for _, item := range indexes {
		descriptor := item.(indexDescriptor).Descriptor()
		if descriptor.Unique && slices.Equal(descriptor.Fields, fields) && slices.Equal(descriptor.Edges, edges) {
			return true
		}
	}
	return false
}

func requireImmutableUUID(t *testing.T, schemaFields []ent.Field) {
	t.Helper()
	id := fieldsByName(schemaFields)["id"]
	if id == nil || id.Info == nil || id.Info.Type != field.TypeUUID || !id.Immutable {
		t.Fatal("id must be an immutable UUID")
	}
}

func requireOwnedEdge(t *testing.T, schemaEdges []ent.Edge, name string) {
	t.Helper()
	owner := edgesByName(schemaEdges)[name]
	if owner == nil || !owner.Required || !owner.Unique || !owner.Immutable {
		t.Fatalf("%s must be a required, unique, immutable edge", name)
	}
}

func TestUserSchemaProtectsIdentityAndPassword(t *testing.T) {
	fields := fieldsByName((User{}).Fields())

	if !fields["email"].Unique {
		t.Fatal("email must be unique")
	}
	if !fields["password_hash"].Sensitive {
		t.Fatal("password_hash must be sensitive")
	}
	if fields["created_at"].Default == nil || !fields["created_at"].Immutable {
		t.Fatal("created_at must default on creation and be immutable")
	}
	if fields["updated_at"].Default == nil || fields["updated_at"].UpdateDefault == nil {
		t.Fatal("updated_at must be maintained automatically")
	}

	edges := edgesByName((User{}).Edges())
	if _, ok := edges["x_accounts"]; !ok {
		t.Fatal("user must expose the x_accounts relationship")
	}
}

func TestXAccountSchemaEnforcesExclusiveOwnershipAndProtectsTokens(t *testing.T) {
	fields := fieldsByName((XAccount{}).Fields())

	if !fields["x_user_id"].Unique || !fields["x_user_id"].Immutable {
		t.Fatal("x_user_id must be globally unique and immutable")
	}
	for _, name := range []string{"access_token", "refresh_token"} {
		if !fields[name].Sensitive {
			t.Fatalf("%s must be sensitive", name)
		}
	}
	if !fields["refresh_token"].Optional {
		t.Fatal("refresh_token must tolerate providers omitting a refresh token")
	}
	for _, name := range []string{"rate_limit_remaining", "rate_limit_reset_at"} {
		if descriptor := fields[name]; descriptor == nil || !descriptor.Optional || !descriptor.Nillable {
			t.Fatalf("%s must be nullable shared provider state", name)
		}
	}

	owner := edgesByName((XAccount{}).Edges())["owner"]
	if owner == nil || !owner.Required || !owner.Unique || !owner.Immutable {
		t.Fatal("owner must be a required, unique, immutable edge")
	}
}

func TestPostSchemaDefinesLifecycleOwnershipAndLease(t *testing.T) {
	requireImmutableUUID(t, (Post{}).Fields())
	fields := fieldsByName((Post{}).Fields())
	for _, name := range []string{
		"creation_mode", "status", "scheduled_at", "publish_requested_at", "published_at",
		"next_attempt_at", "active_river_job_id", "attempt_count", "last_error_code",
		"last_error_message", "lease_token", "lease_owner", "lease_expires_at",
		"lease_version", "created_at", "updated_at",
	} {
		if fields[name] == nil {
			t.Errorf("post field %s is missing", name)
		}
	}
	if fields["attempt_count"].Default == nil || fields["lease_version"].Default == nil {
		t.Fatal("attempt_count and lease_version must have defaults")
	}
	requireOwnedEdge(t, (Post{}).Edges(), "owner")
	requireOwnedEdge(t, (Post{}).Edges(), "x_account")
}

func TestPostChildrenUseStableOrderingAndSafeMetadata(t *testing.T) {
	requireImmutableUUID(t, (PostItem{}).Fields())
	requireImmutableUUID(t, (MediaAsset{}).Fields())
	requireImmutableUUID(t, (PublicationAttempt{}).Fields())

	if !hasUniqueIndex((PostItem{}).Indexes(), []string{"post_id", "position"}, nil) {
		t.Fatal("post items must be unique by post and position")
	}
	if !hasUniqueIndex((MediaAsset{}).Indexes(), []string{"post_item_id", "position"}, nil) {
		t.Fatal("media assets must be unique by post item and position")
	}

	mediaFields := fieldsByName((MediaAsset{}).Fields())
	for _, name := range []string{"storage_key", "original_filename", "mime_type", "size_bytes", "sha256_checksum", "alt_text"} {
		descriptor := mediaFields[name]
		if descriptor == nil {
			t.Errorf("media field %s is missing", name)
			continue
		}
		if descriptor.Sensitive {
			t.Errorf("media metadata field %s must not contain secret storage credentials", name)
		}
	}
	if !mediaFields["storage_key"].Unique {
		t.Fatal("opaque media storage keys must be unique")
	}
	requireOwnedEdge(t, (MediaAsset{}).Edges(), "owner")
	requireOwnedEdge(t, (MediaAsset{}).Edges(), "post_item")
}

func TestStorageDeletionPersistsOpaqueCleanupWork(t *testing.T) {
	requireImmutableUUID(t, (StorageDeletion{}).Fields())
	fields := fieldsByName((StorageDeletion{}).Fields())
	if fields["storage_key"] == nil || !fields["storage_key"].Unique || fields["storage_key"].Sensitive {
		t.Fatal("storage deletion must retain a unique opaque, non-secret storage key")
	}
	for _, name := range []string{"attempt_count", "next_attempt_at", "last_error_code", "last_error_message", "created_at", "updated_at"} {
		if fields[name] == nil {
			t.Errorf("storage deletion field %s is missing", name)
		}
	}
	if len((StorageDeletion{}).Edges()) != 0 {
		t.Fatal("storage deletion work must survive deletion of the post aggregate")
	}
}

func TestSCSSessionSchemaMatchesLibraryStoreContract(t *testing.T) {
	fields := fieldsByName((SCSSession{}).Fields())

	if fields["id"].StorageKey != "token" {
		t.Fatalf("session id storage key = %q, want token", fields["id"].StorageKey)
	}
	if fields["data"] == nil || fields["expiry"] == nil {
		t.Fatal("session store requires data and expiry columns")
	}

	var table string
	for _, annotation := range (SCSSession{}).Annotations() {
		if sqlAnnotation, ok := annotation.(entsql.Annotation); ok {
			table = sqlAnnotation.Table
		}
	}
	if table != "sessions" {
		t.Fatalf("session table = %q, want sessions", table)
	}
}
