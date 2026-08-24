package posts

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestServiceCreateAllowsTemporarilyEmptyDraftItem(t *testing.T) {
	repository := newMemoryPostRepository()
	ownerID, accountID := uuid.New(), uuid.New()
	repository.accounts[accountKey{ownerID, accountID}] = "None"
	service := NewService(repository, DefaultTextPolicy())

	created, err := service.Create(context.Background(), CreateCommand{
		OwnerID: ownerID, XAccountID: accountID, CreationMode: CreationModeUser,
		Items: []ItemInput{{Text: ""}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Status != StatusDraft || len(created.Items) != 1 || created.Items[0].Text != "" {
		t.Fatalf("Create() = %+v, want one empty draft item", created)
	}
}

func TestServiceCreateRequiresOneToTwentyFiveItems(t *testing.T) {
	repository := newMemoryPostRepository()
	ownerID, accountID := uuid.New(), uuid.New()
	repository.accounts[accountKey{ownerID, accountID}] = "None"
	service := NewService(repository, DefaultTextPolicy())

	for name, items := range map[string][]ItemInput{
		"zero":       nil,
		"twenty-six": make([]ItemInput, 26),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := service.Create(context.Background(), CreateCommand{OwnerID: ownerID, XAccountID: accountID, CreationMode: CreationModeUser, Items: items})
			var fieldErr *FieldError
			if !errors.As(err, &fieldErr) || fieldErr.Field != "items" {
				t.Fatalf("Create() error = %v, want items FieldError", err)
			}
		})
	}
}

func TestServiceCreateRejectsAccountNotOwnedByUser(t *testing.T) {
	service := NewService(newMemoryPostRepository(), DefaultTextPolicy())
	_, err := service.Create(context.Background(), CreateCommand{
		OwnerID: uuid.New(), XAccountID: uuid.New(), CreationMode: CreationModeUser,
		Items: []ItemInput{{Text: "draft"}},
	})
	if !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("Create() error = %v, want ErrAccountNotFound", err)
	}
}

func TestServiceCreateUsesCachedSubscriptionTextLimit(t *testing.T) {
	ownerID, standardID, premiumID := uuid.New(), uuid.New(), uuid.New()
	repository := newMemoryPostRepository()
	repository.accounts[accountKey{ownerID, standardID}] = "unknown-future-tier"
	repository.accounts[accountKey{ownerID, premiumID}] = "Premium"
	service := NewService(repository, DefaultTextPolicy())
	longText := strings.Repeat("a", 281)

	_, err := service.Create(context.Background(), CreateCommand{OwnerID: ownerID, XAccountID: standardID, CreationMode: CreationModeUser, Items: []ItemInput{{Text: longText}}})
	if !errors.Is(err, ErrTextTooLong) {
		t.Fatalf("standard Create() error = %v, want ErrTextTooLong", err)
	}
	if _, err := service.Create(context.Background(), CreateCommand{OwnerID: ownerID, XAccountID: premiumID, CreationMode: CreationModeUser, Items: []ItemInput{{Text: longText}}}); err != nil {
		t.Fatalf("premium Create() error = %v", err)
	}
}

func TestServiceUpdateRejectsImmutablePost(t *testing.T) {
	repository := newMemoryPostRepository()
	ownerID, accountID, postID := uuid.New(), uuid.New(), uuid.New()
	repository.accounts[accountKey{ownerID, accountID}] = "None"
	repository.posts[postID] = Post{ID: postID, OwnerID: ownerID, XAccountID: accountID, Status: StatusPublished, Items: []Item{{Text: "original"}}}
	service := NewService(repository, DefaultTextPolicy())

	_, err := service.Update(context.Background(), UpdateCommand{OwnerID: ownerID, PostID: postID, Items: []ItemInput{{Text: "changed"}}})
	if !errors.Is(err, ErrNotEditable) {
		t.Fatalf("Update() error = %v, want ErrNotEditable", err)
	}
	if repository.posts[postID].Items[0].Text != "original" {
		t.Fatal("Update() changed immutable post")
	}
}

func TestServiceUpdateAllowsIncompleteDraft(t *testing.T) {
	repository, ownerID, postID := repositoryWithPost(StatusDraft, []Item{{Text: "original"}})
	service := NewService(repository, DefaultTextPolicy())

	updated, err := service.Update(context.Background(), UpdateCommand{OwnerID: ownerID, PostID: postID, Items: []ItemInput{{Text: ""}}})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Items[0].Text != "" {
		t.Fatalf("Update() text = %q, want empty", updated.Items[0].Text)
	}
}

func TestServiceUpdateRequiresScheduledPostToRemainPublishable(t *testing.T) {
	repository, ownerID, postID := repositoryWithPost(StatusScheduled, []Item{{Text: "ready"}})
	service := NewService(repository, DefaultTextPolicy())

	_, err := service.Update(context.Background(), UpdateCommand{OwnerID: ownerID, PostID: postID, Items: []ItemInput{{Text: ""}}})
	var fieldErr *FieldError
	if !errors.As(err, &fieldErr) || fieldErr.Field != "items[0]" || fieldErr.Code != "content_required" {
		t.Fatalf("Update() error = %v, want item-specific content_required error", err)
	}
}

func TestValidatePublishableAcceptsMediaOnlyAndRejectsEmptyItem(t *testing.T) {
	if err := ValidatePublishable(Post{Items: []Item{{Media: []Media{{ID: uuid.New()}}}}}); err != nil {
		t.Fatalf("ValidatePublishable() media-only error = %v", err)
	}
	err := ValidatePublishable(Post{Items: []Item{{Text: "ready"}, {Text: "  "}}})
	var fieldErr *FieldError
	if !errors.As(err, &fieldErr) || fieldErr.Field != "items[1]" || fieldErr.Code != "content_required" {
		t.Fatalf("ValidatePublishable() error = %v", err)
	}
}

func TestDefaultTextPolicyUsesXURLWeight(t *testing.T) {
	policy := DefaultTextPolicy()
	if err := policy.Validate(strings.Repeat("a", 256)+" https://example.com/a/very/long/path", "None"); err != nil {
		t.Fatalf("Validate() 280-weight URL error = %v", err)
	}
	if err := policy.Validate(strings.Repeat("a", 257)+" https://example.com/a/very/long/path", "None"); !errors.Is(err, ErrTextTooLong) {
		t.Fatalf("Validate() 281-weight URL error = %v, want ErrTextTooLong", err)
	}
}

func TestDefaultTextPolicyUsesXEmojiWeight(t *testing.T) {
	policy := DefaultTextPolicy()
	if err := policy.Validate(strings.Repeat("😀", 140), "None"); err != nil {
		t.Fatalf("Validate() 140 emoji error = %v", err)
	}
	if err := policy.Validate(strings.Repeat("😀", 141), "None"); !errors.Is(err, ErrTextTooLong) {
		t.Fatalf("Validate() 141 emoji error = %v, want ErrTextTooLong", err)
	}
}

func TestDefaultTextPolicySupportsPremiumLimit(t *testing.T) {
	policy := DefaultTextPolicy()
	if err := policy.Validate(strings.Repeat("a", 25_000), "PremiumPlus"); err != nil {
		t.Fatalf("Validate() premium boundary error = %v", err)
	}
	if err := policy.Validate(strings.Repeat("a", 25_001), "PremiumPlus"); !errors.Is(err, ErrTextTooLong) {
		t.Fatalf("Validate() premium over-limit error = %v, want ErrTextTooLong", err)
	}
}

type accountKey struct{ ownerID, accountID uuid.UUID }

type memoryPostRepository struct {
	accounts map[accountKey]string
	posts    map[uuid.UUID]Post
}

func newMemoryPostRepository() *memoryPostRepository {
	return &memoryPostRepository{accounts: make(map[accountKey]string), posts: make(map[uuid.UUID]Post)}
}

func (r *memoryPostRepository) AccountSubscription(_ context.Context, ownerID, accountID uuid.UUID) (string, error) {
	subscription, ok := r.accounts[accountKey{ownerID, accountID}]
	if !ok {
		return "", ErrAccountNotFound
	}
	return subscription, nil
}

func (r *memoryPostRepository) Create(_ context.Context, command CreateCommand) (Post, error) {
	post := Post{ID: uuid.New(), OwnerID: command.OwnerID, XAccountID: command.XAccountID, CreationMode: command.CreationMode, Status: StatusDraft}
	post.Items = itemsFromInput(command.Items)
	r.posts[post.ID] = post
	return post, nil
}

func (r *memoryPostRepository) Get(_ context.Context, ownerID, postID uuid.UUID) (Post, error) {
	post, ok := r.posts[postID]
	if !ok || post.OwnerID != ownerID {
		return Post{}, ErrNotFound
	}
	return post, nil
}

func (r *memoryPostRepository) List(_ context.Context, ownerID uuid.UUID) ([]Post, error) {
	var result []Post
	for _, post := range r.posts {
		if post.OwnerID == ownerID {
			result = append(result, post)
		}
	}
	return result, nil
}

func (r *memoryPostRepository) ReplaceItems(_ context.Context, ownerID, postID uuid.UUID, items []ItemInput) (Post, error) {
	post, err := r.Get(context.Background(), ownerID, postID)
	if err != nil {
		return Post{}, err
	}
	post.Items = itemsFromInput(items)
	r.posts[postID] = post
	return post, nil
}

func itemsFromInput(inputs []ItemInput) []Item {
	items := make([]Item, len(inputs))
	for index, input := range inputs {
		items[index] = Item{ID: input.ID, Position: index, Text: input.Text, Media: append([]Media(nil), input.Media...)}
		if items[index].ID == uuid.Nil {
			items[index].ID = uuid.New()
		}
	}
	return items
}

func repositoryWithPost(status Status, items []Item) (*memoryPostRepository, uuid.UUID, uuid.UUID) {
	repository := newMemoryPostRepository()
	ownerID, accountID, postID := uuid.New(), uuid.New(), uuid.New()
	repository.accounts[accountKey{ownerID, accountID}] = "None"
	repository.posts[postID] = Post{ID: postID, OwnerID: ownerID, XAccountID: accountID, CreationMode: CreationModeUser, Status: status, Items: items}
	return repository, ownerID, postID
}
