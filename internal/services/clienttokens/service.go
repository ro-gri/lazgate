package clienttokens

import (
	"time"

	commontokens "laz/internal/common/tokens"
	"laz/internal/model"
)

type Store interface {
	CreateAccessToken(model.AccessToken) (model.AccessToken, error)
	ListAccessTokens() []model.AccessToken
}

type Service struct {
	store Store
}

func New(st Store) *Service {
	return &Service{store: st}
}

func (s *Service) GetOrCreate(accountID, clientID string, expiresAt time.Time) (string, model.AccessToken, error) {
	if expiresAt.IsZero() {
		now := time.Now().UTC()
		for _, item := range s.store.ListAccessTokens() {
			if item.AccountID != accountID || item.ClientID != clientID || item.Purpose != model.TokenPurposeClient {
				continue
			}
			if item.Status != model.StatusActive || item.Token == "" {
				continue
			}
			if !item.ExpiresAt.IsZero() && now.After(item.ExpiresAt) {
				continue
			}
			return item.Token, item, nil
		}
	}

	token, err := commontokens.New()
	if err != nil {
		return "", model.AccessToken{}, err
	}
	record, err := s.store.CreateAccessToken(model.AccessToken{
		AccountID: accountID,
		ClientID:  clientID,
		Token:     token,
		TokenHash: commontokens.Hash(token),
		Purpose:   model.TokenPurposeClient,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return "", model.AccessToken{}, err
	}
	return token, record, nil
}
