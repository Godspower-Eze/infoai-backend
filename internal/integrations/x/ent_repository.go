package xintegration

import (
	"context"
	"fmt"

	"github.com/Godspower-Eze/infoai-backend/ent"
	entuser "github.com/Godspower-Eze/infoai-backend/ent/user"
	entxaccount "github.com/Godspower-Eze/infoai-backend/ent/xaccount"
	"github.com/google/uuid"
)

type EntAccountRepository struct {
	client *ent.Client
}

func NewEntAccountRepository(client *ent.Client) *EntAccountRepository {
	return &EntAccountRepository{client: client}
}

func (r *EntAccountRepository) Upsert(ctx context.Context, ownerID uuid.UUID, profile Profile, grant EncryptedGrant) (Account, error) {
	stored, err := r.client.XAccount.Query().
		Where(entxaccount.XUserIDEQ(profile.ID)).
		WithOwner().
		Only(ctx)
	if err == nil {
		owner, edgeErr := stored.Edges.OwnerOrErr()
		if edgeErr != nil {
			return Account{}, fmt.Errorf("load X account owner: %w", edgeErr)
		}
		if owner.ID != ownerID {
			return Account{}, ErrAccountOwned
		}
		return r.updateExisting(ctx, stored, profile, grant)
	}
	if !ent.IsNotFound(err) {
		return Account{}, fmt.Errorf("find X account: %w", err)
	}

	create := r.client.XAccount.Create().
		SetOwnerID(ownerID).
		SetXUserID(profile.ID).
		SetUsername(profile.Username).
		SetDisplayName(profile.DisplayName).
		SetNillableProfileImageURL(profile.ProfileImageURL).
		SetSubscriptionType(profile.SubscriptionType).
		SetNillableSubscriptionCheckedAt(profile.SubscriptionCheckedAt).
		SetAccessToken(grant.AccessToken).
		SetTokenExpiry(grant.Expiry).
		SetScopes(grant.Scopes)
	if len(grant.RefreshToken) > 0 {
		create.SetRefreshToken(grant.RefreshToken)
	}
	created, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			existing, lookupErr := r.client.XAccount.Query().Where(entxaccount.XUserIDEQ(profile.ID)).WithOwner().Only(ctx)
			if lookupErr == nil {
				owner, edgeErr := existing.Edges.OwnerOrErr()
				if edgeErr == nil && owner.ID != ownerID {
					return Account{}, ErrAccountOwned
				}
			}
		}
		return Account{}, fmt.Errorf("create X account: %w", err)
	}
	return accountFromEnt(created, ownerID), nil
}

func (r *EntAccountRepository) List(ctx context.Context, ownerID uuid.UUID) ([]Account, error) {
	stored, err := r.client.XAccount.Query().
		Where(entxaccount.HasOwnerWith(entuser.IDEQ(ownerID))).
		Order(ent.Asc(entxaccount.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list X accounts: %w", err)
	}
	accounts := make([]Account, len(stored))
	for index, item := range stored {
		accounts[index] = accountFromEnt(item, ownerID)
	}
	return accounts, nil
}

func (r *EntAccountRepository) Grant(ctx context.Context, ownerID, accountID uuid.UUID) (StoredGrant, error) {
	stored, err := r.ownedQuery(ownerID, accountID).WithOwner().Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return StoredGrant{}, ErrAccountNotFound
		}
		return StoredGrant{}, fmt.Errorf("load X grant: %w", err)
	}
	return grantFromEnt(stored, ownerID), nil
}

func (r *EntAccountRepository) UpdateGrant(ctx context.Context, ownerID, accountID uuid.UUID, grant EncryptedGrant) error {
	stored, err := r.ownedQuery(ownerID, accountID).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrAccountNotFound
		}
		return fmt.Errorf("find X grant for update: %w", err)
	}
	update := stored.Update().
		SetAccessToken(grant.AccessToken).
		SetTokenExpiry(grant.Expiry).
		SetScopes(grant.Scopes)
	if len(grant.RefreshToken) == 0 {
		update.ClearRefreshToken()
	} else {
		update.SetRefreshToken(grant.RefreshToken)
	}
	if _, err := update.Save(ctx); err != nil {
		return fmt.Errorf("update X grant: %w", err)
	}
	return nil
}

func (r *EntAccountRepository) Delete(ctx context.Context, ownerID, accountID uuid.UUID) (StoredGrant, error) {
	stored, err := r.Grant(ctx, ownerID, accountID)
	if err != nil {
		return StoredGrant{}, err
	}
	if err := r.client.XAccount.DeleteOneID(accountID).Exec(ctx); err != nil {
		return StoredGrant{}, fmt.Errorf("delete X account: %w", err)
	}
	return stored, nil
}

func (r *EntAccountRepository) updateExisting(ctx context.Context, stored *ent.XAccount, profile Profile, grant EncryptedGrant) (Account, error) {
	update := stored.Update().
		SetUsername(profile.Username).
		SetDisplayName(profile.DisplayName).
		SetSubscriptionType(profile.SubscriptionType).
		SetAccessToken(grant.AccessToken).
		SetTokenExpiry(grant.Expiry).
		SetScopes(grant.Scopes)
	if profile.ProfileImageURL == nil {
		update.ClearProfileImageURL()
	} else {
		update.SetProfileImageURL(*profile.ProfileImageURL)
	}
	if profile.SubscriptionCheckedAt == nil {
		update.ClearSubscriptionCheckedAt()
	} else {
		update.SetSubscriptionCheckedAt(*profile.SubscriptionCheckedAt)
	}
	if len(grant.RefreshToken) == 0 {
		update.ClearRefreshToken()
	} else {
		update.SetRefreshToken(grant.RefreshToken)
	}
	updated, err := update.Save(ctx)
	if err != nil {
		return Account{}, fmt.Errorf("update X account: %w", err)
	}
	owner, err := stored.Edges.OwnerOrErr()
	if err != nil {
		return Account{}, fmt.Errorf("load X account owner: %w", err)
	}
	return accountFromEnt(updated, owner.ID), nil
}

func (r *EntAccountRepository) ownedQuery(ownerID, accountID uuid.UUID) *ent.XAccountQuery {
	return r.client.XAccount.Query().Where(
		entxaccount.IDEQ(accountID),
		entxaccount.HasOwnerWith(entuser.IDEQ(ownerID)),
	)
}

func accountFromEnt(stored *ent.XAccount, ownerID uuid.UUID) Account {
	return Account{
		ID:                    stored.ID,
		OwnerID:               ownerID,
		XUserID:               stored.XUserID,
		Username:              stored.Username,
		DisplayName:           stored.DisplayName,
		ProfileImageURL:       stored.ProfileImageURL,
		SubscriptionType:      stored.SubscriptionType,
		SubscriptionCheckedAt: stored.SubscriptionCheckedAt,
		CreatedAt:             stored.CreatedAt,
		UpdatedAt:             stored.UpdatedAt,
	}
}

func grantFromEnt(stored *ent.XAccount, ownerID uuid.UUID) StoredGrant {
	var refresh []byte
	if stored.RefreshToken != nil {
		refresh = append([]byte(nil), (*stored.RefreshToken)...)
	}
	return StoredGrant{
		Account: accountFromEnt(stored, ownerID),
		EncryptedGrant: EncryptedGrant{
			AccessToken:  append([]byte(nil), stored.AccessToken...),
			RefreshToken: refresh,
			Expiry:       stored.TokenExpiry,
			Scopes:       append([]string(nil), stored.Scopes...),
		},
	}
}

var _ AccountRepository = (*EntAccountRepository)(nil)
