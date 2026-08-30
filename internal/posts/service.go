package posts

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
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
	repository         Repository
	deletions          DraftDeletionRepository
	mediaRepository    MediaRepository
	textPolicy         TextPolicy
	mediaPolicy        MediaPolicy
	mediaStorage       MediaStorage
	publication        PublicationRepository
	outcomes           OutcomeRepository
	publishedDeletions PublishedDeletionRepository
	retries            RetryRepository
	now                func() time.Time
}

type PublishingDependencies struct {
	Posts              FullRepository
	Publication        PublicationRepository
	Outcomes           OutcomeRepository
	PublishedDeletions PublishedDeletionRepository
	Retries            RetryRepository
}

func NewServiceWithPublishing(dependencies PublishingDependencies, textPolicy TextPolicy, mediaPolicy MediaPolicy, mediaStorage MediaStorage) *Service {
	service := NewServiceWithMedia(dependencies.Posts, textPolicy, mediaPolicy, mediaStorage)
	service.now = time.Now
	service.publication = dependencies.Publication
	service.outcomes = dependencies.Outcomes
	service.publishedDeletions = dependencies.PublishedDeletions
	service.retries = dependencies.Retries
	return service
}

func (s *Service) Retry(ctx context.Context, command RetryCommand) (Post, error) {
	if s.retries == nil {
		return Post{}, errors.New("publication retry is not configured")
	}
	return s.retries.Retry(ctx, command)
}

func (s *Service) RequestDeletion(ctx context.Context, command DeleteCommand) (bool, error) {
	current, err := s.repository.Get(ctx, command.OwnerID, command.PostID)
	if err != nil {
		return false, err
	}
	hasXContent := false
	for _, item := range current.Items {
		if item.XPostID != nil {
			hasXContent = true
			break
		}
	}
	if !hasXContent {
		if err := s.deletions.Delete(ctx, command.OwnerID, command.PostID); err != nil {
			return false, err
		}
		return false, nil
	}
	if !command.ConfirmXDeletion {
		return false, ErrConfirmationRequired
	}
	if s.publishedDeletions == nil {
		return false, errors.New("published deletion is not configured")
	}
	return s.publishedDeletions.RequestDeletion(ctx, command)
}

func (s *Service) ResolveOutcome(ctx context.Context, command ResolveOutcomeCommand) (Post, error) {
	current, err := s.repository.Get(ctx, command.OwnerID, command.PostID)
	if err != nil {
		return Post{}, err
	}
	foundUnknown := false
	for _, item := range current.Items {
		if item.ID == command.ItemID {
			foundUnknown = item.SubmissionState == SubmissionOutcomeUnknown
			break
		}
	}
	if !foundUnknown {
		return Post{}, ErrInvalidTransition
	}
	if command.Decision == OutcomeUnresolved {
		return current, nil
	}
	xID := ""
	if command.Decision == OutcomePublished {
		parsed, err := url.Parse(command.XURL)
		if err != nil || parsed.Scheme != "https" || (parsed.Hostname() != "x.com" && parsed.Hostname() != "twitter.com") {
			return Post{}, &FieldError{Field: "x_url", Code: "invalid"}
		}
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) < 3 || parts[len(parts)-2] != "status" {
			return Post{}, &FieldError{Field: "x_url", Code: "invalid"}
		}
		xID = parts[len(parts)-1]
		if _, err := strconv.ParseUint(xID, 10, 64); err != nil {
			return Post{}, &FieldError{Field: "x_url", Code: "invalid"}
		}
	} else if command.Decision != OutcomeNotPublished {
		return Post{}, &FieldError{Field: "decision", Code: "invalid"}
	}
	if s.outcomes == nil {
		return Post{}, errors.New("outcome resolution is not configured")
	}
	return s.outcomes.ResolveOutcome(ctx, command, xID)
}

func (s *Service) Publish(ctx context.Context, ownerID, postID uuid.UUID) (Post, error) {
	current, err := s.repository.Get(ctx, ownerID, postID)
	if err != nil {
		return Post{}, err
	}
	if current.Status != StatusDraft {
		return Post{}, ErrNotEditable
	}
	if err := ValidatePublishable(current); err != nil {
		return Post{}, err
	}
	return s.publication.Publish(ctx, ownerID, postID)
}

func (s *Service) Schedule(ctx context.Context, ownerID, postID uuid.UUID, at time.Time) (Post, error) {
	if !at.After(s.now()) {
		return Post{}, &FieldError{Field: "scheduled_at", Code: "must_be_future"}
	}
	current, err := s.repository.Get(ctx, ownerID, postID)
	if err != nil {
		return Post{}, err
	}
	if current.Status != StatusDraft && current.Status != StatusScheduled {
		return Post{}, ErrNotEditable
	}
	if err := ValidatePublishable(current); err != nil {
		return Post{}, err
	}
	return s.publication.Schedule(ctx, ownerID, postID, at)
}

func (s *Service) CancelSchedule(ctx context.Context, ownerID, postID uuid.UUID) (Post, error) {
	return s.publication.CancelSchedule(ctx, ownerID, postID)
}

func NewService(repository Repository, textPolicy TextPolicy) *Service {
	return &Service{repository: repository, textPolicy: textPolicy, now: time.Now}
}

func NewServiceWithMedia(repository FullRepository, textPolicy TextPolicy, mediaPolicy MediaPolicy, mediaStorage MediaStorage) *Service {
	return &Service{repository: repository, deletions: repository, mediaRepository: repository, textPolicy: textPolicy, mediaPolicy: mediaPolicy, mediaStorage: mediaStorage, now: time.Now}
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

func (s *Service) Delete(ctx context.Context, ownerID, postID uuid.UUID) error {
	return s.deletions.Delete(ctx, ownerID, postID)
}

func (s *Service) UploadMedia(ctx context.Context, command UploadMediaCommand) (Post, error) {
	current, err := s.repository.Get(ctx, command.OwnerID, command.PostID)
	if err != nil {
		return Post{}, err
	}
	if current.Status != StatusDraft && current.Status != StatusScheduled {
		return Post{}, ErrNotEditable
	}
	var item *Item
	for index := range current.Items {
		if current.Items[index].ID == command.ItemID {
			item = &current.Items[index]
			break
		}
	}
	if item == nil {
		return Post{}, ErrNotFound
	}
	siblings := make([]MediaMetadata, len(item.Media))
	for index, media := range item.Media {
		siblings[index] = MediaMetadata{Category: categoryForMIME(media.MIMEType)}
	}
	detected, err := s.mediaPolicy.Validate(command.Header, command.Size, siblings)
	if err != nil {
		return Post{}, err
	}
	key := command.PostID.String() + "/" + command.ItemID.String() + "/" + uuid.NewString()
	object, err := s.mediaStorage.Put(ctx, key, command.Source)
	if err != nil {
		return Post{}, fmt.Errorf("store post media: %w", err)
	}
	if _, err := s.mediaPolicy.Validate(command.Header, object.Size, siblings); err != nil {
		return Post{}, errors.Join(err, s.compensateStoredObject(ctx, object.Key))
	}
	media := Media{ID: uuid.New(), Position: len(item.Media), StorageKey: object.Key, OriginalFilename: command.OriginalFilename, MIMEType: detected.MIMEType, Size: object.Size, SHA256: object.SHA256, AltText: command.AltText}
	updated, err := s.mediaRepository.AddMedia(ctx, AddMediaCommand{OwnerID: command.OwnerID, PostID: command.PostID, ItemID: command.ItemID, Media: media, Category: detected.Category})
	if err != nil {
		return Post{}, errors.Join(err, s.compensateStoredObject(ctx, object.Key))
	}
	return updated, nil
}

func (s *Service) RemoveMedia(ctx context.Context, command RemoveMediaCommand) error {
	return s.mediaRepository.RemoveMedia(ctx, command)
}

func (s *Service) compensateStoredObject(ctx context.Context, key string) error {
	if err := s.mediaStorage.Delete(ctx, key); err != nil {
		return errors.Join(err, s.mediaRepository.QueueStorageDeletion(ctx, key))
	}
	return nil
}

func categoryForMIME(mime string) MediaCategory {
	switch mime {
	case "image/gif":
		return MediaGIF
	case "video/mp4":
		return MediaVideo
	default:
		return MediaImage
	}
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
