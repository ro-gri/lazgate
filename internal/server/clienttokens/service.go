package clienttokens

import (
	"time"

	"laz/internal/server/model"
	commontokens "laz/internal/server/security/tokens"
)

type Store interface {
	CreateAccountPortalToken(model.AccountPortalToken) (model.AccountPortalToken, error)
	ListAccountPortalTokens() []model.AccountPortalToken
	CreateConnectionSubscriptionToken(model.ConnectionSubscriptionToken) (model.ConnectionSubscriptionToken, error)
	ListConnectionSubscriptionTokens() []model.ConnectionSubscriptionToken
}

type Service struct{ store Store }

func New(st Store) *Service { return &Service{store: st} }

func (s *Service) GetOrCreatePortal(accountID string, expiresAt time.Time) (string, model.AccountPortalToken, error) {
	if expiresAt.IsZero() {
		now := time.Now().UTC()
		for _, item := range s.store.ListAccountPortalTokens() {
			if item.AccountID == accountID && reusable(item.Status, item.Token, item.ExpiresAt, now) {
				return item.Token, item, nil
			}
		}
	}
	raw, err := commontokens.New()
	if err != nil {
		return "", model.AccountPortalToken{}, err
	}
	record, err := s.store.CreateAccountPortalToken(model.AccountPortalToken{AccountID: accountID, Token: raw, TokenHash: commontokens.Hash(raw), ExpiresAt: expiresAt})
	return raw, record, err
}

func (s *Service) GetOrCreateSubscription(connectionID string, expiresAt time.Time) (string, model.ConnectionSubscriptionToken, error) {
	if expiresAt.IsZero() {
		now := time.Now().UTC()
		for _, item := range s.store.ListConnectionSubscriptionTokens() {
			if item.ConnectionID == connectionID && reusable(item.Status, item.Token, item.ExpiresAt, now) {
				return item.Token, item, nil
			}
		}
	}
	raw, err := commontokens.New()
	if err != nil {
		return "", model.ConnectionSubscriptionToken{}, err
	}
	record, err := s.store.CreateConnectionSubscriptionToken(model.ConnectionSubscriptionToken{ConnectionID: connectionID, Token: raw, TokenHash: commontokens.Hash(raw), ExpiresAt: expiresAt})
	return raw, record, err
}

func reusable(status model.Status, token string, expiresAt, now time.Time) bool {
	return status == model.StatusActive && token != "" && (expiresAt.IsZero() || !now.After(expiresAt))
}
