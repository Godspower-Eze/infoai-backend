package schema

import (
	"testing"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type fieldDescriptor interface {
	Descriptor() *field.Descriptor
}

type edgeDescriptor interface {
	Descriptor() *edge.Descriptor
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

	owner := edgesByName((XAccount{}).Edges())["owner"]
	if owner == nil || !owner.Required || !owner.Unique || !owner.Immutable {
		t.Fatal("owner must be a required, unique, immutable edge")
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
