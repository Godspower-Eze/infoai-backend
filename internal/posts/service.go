package posts

import (
	"bufio"
	"bytes"
	"context"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/apparentlymart/go-textseg/v15/textseg"
	"github.com/google/uuid"
)

const (
	maxPostItems         = 25
	standardTextLimit    = 280
	premiumTextLimit     = 25_000
	transformedURLLength = 23
)

type TextPolicy interface {
	Validate(text, subscriptionType string) error
}

type Service struct {
	repository Repository
	textPolicy TextPolicy
}

func NewService(repository Repository, textPolicy TextPolicy) *Service {
	return &Service{repository: repository, textPolicy: textPolicy}
}

func (s *Service) Create(ctx context.Context, command CreateCommand) (Post, error) {
	if command.OwnerID == uuid.Nil || command.XAccountID == uuid.Nil {
		return Post{}, &FieldError{Field: "owner_id", Code: "required"}
	}
	if command.CreationMode != CreationModeUser && command.CreationMode != CreationModeAgent {
		return Post{}, &FieldError{Field: "creation_mode", Code: "invalid"}
	}
	if err := validateItemCount(command.Items); err != nil {
		return Post{}, err
	}
	subscription, err := s.repository.AccountSubscription(ctx, command.OwnerID, command.XAccountID)
	if err != nil {
		return Post{}, err
	}
	if err := s.validateText(command.Items, subscription); err != nil {
		return Post{}, err
	}
	return s.repository.Create(ctx, command)
}

func (s *Service) Get(ctx context.Context, ownerID, postID uuid.UUID) (Post, error) {
	return s.repository.Get(ctx, ownerID, postID)
}

func (s *Service) List(ctx context.Context, ownerID uuid.UUID) ([]Post, error) {
	return s.repository.List(ctx, ownerID)
}

func (s *Service) Update(ctx context.Context, command UpdateCommand) (Post, error) {
	if err := validateItemCount(command.Items); err != nil {
		return Post{}, err
	}
	current, err := s.repository.Get(ctx, command.OwnerID, command.PostID)
	if err != nil {
		return Post{}, err
	}
	if current.Status != StatusDraft && current.Status != StatusScheduled {
		return Post{}, ErrNotEditable
	}
	subscription, err := s.repository.AccountSubscription(ctx, command.OwnerID, current.XAccountID)
	if err != nil {
		return Post{}, err
	}
	if err := s.validateText(command.Items, subscription); err != nil {
		return Post{}, err
	}
	if current.Status == StatusScheduled {
		candidate := current
		candidate.Items = domainItems(command.Items)
		if err := ValidatePublishable(candidate); err != nil {
			return Post{}, err
		}
	}
	return s.repository.ReplaceItems(ctx, command.OwnerID, command.PostID, command.Items)
}

func ValidatePublishable(post Post) error {
	for index, item := range post.Items {
		if strings.TrimSpace(item.Text) == "" && len(item.Media) == 0 {
			return &FieldError{Field: "items[" + strconv.Itoa(index) + "]", Code: "content_required"}
		}
	}
	return nil
}

func (s *Service) validateText(items []ItemInput, subscription string) error {
	for index, item := range items {
		if err := s.textPolicy.Validate(item.Text, subscription); err != nil {
			return &FieldError{Field: "items[" + strconv.Itoa(index) + "].text", Code: "too_long", Err: err}
		}
	}
	return nil
}

func validateItemCount(items []ItemInput) error {
	if len(items) < 1 || len(items) > maxPostItems {
		return &FieldError{Field: "items", Code: "invalid_count"}
	}
	return nil
}

func domainItems(inputs []ItemInput) []Item {
	items := make([]Item, len(inputs))
	for index, input := range inputs {
		items[index] = Item{ID: input.ID, Position: index, Text: input.Text, Media: input.Media}
	}
	return items
}

type defaultTextPolicy struct{}

func DefaultTextPolicy() TextPolicy {
	return defaultTextPolicy{}
}

func (defaultTextPolicy) Validate(text, subscriptionType string) error {
	limit := standardTextLimit
	switch strings.ToLower(strings.TrimSpace(subscriptionType)) {
	case "basic", "premium", "premiumplus":
		limit = premiumTextLimit
	}
	if weightedLength(text) > limit {
		return ErrTextTooLong
	}
	return nil
}

func weightedLength(text string) int {
	weight := 0
	for len(text) > 0 {
		urlStart, urlEnd := nextURL(text)
		if urlStart < 0 {
			return weight + weightedPlainText(text)
		}
		weight += weightedPlainText(text[:urlStart]) + transformedURLLength
		text = text[urlEnd:]
	}
	return weight
}

func weightedPlainText(text string) int {
	scanner := bufio.NewScanner(bytes.NewBufferString(text))
	scanner.Split(textseg.ScanGraphemeClusters)
	weight := 0
	for scanner.Scan() {
		cluster := scanner.Text()
		if containsEmoji(cluster) {
			weight += 2
			continue
		}
		for _, value := range cluster {
			weight += runeWeight(value)
		}
	}
	return weight
}

func runeWeight(value rune) int {
	if value <= 4351 || value >= 8192 && value <= 8205 || value >= 8208 && value <= 8223 || value >= 8242 && value <= 8247 {
		return 1
	}
	return 2
}

func containsEmoji(cluster string) bool {
	for _, value := range cluster {
		if value >= 0x1F000 && value <= 0x1FAFF || value >= 0x2600 && value <= 0x27BF {
			return true
		}
	}
	return false
}

func nextURL(text string) (int, int) {
	start := -1
	for _, prefix := range []string{"https://", "http://"} {
		if candidate := strings.Index(text, prefix); candidate >= 0 && (start < 0 || candidate < start) {
			start = candidate
		}
	}
	if start < 0 {
		return -1, -1
	}
	end := start
	for end < len(text) {
		value, size := utf8.DecodeRuneInString(text[end:])
		if value == utf8.RuneError && size == 0 || value == ' ' || value == '\n' || value == '\t' || value == '\r' {
			break
		}
		end += size
	}
	return start, end
}
