package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"laz/internal/server/model"
)

const encryptedPrefix = "enc:v1:"

type SecureStore struct {
	inner Store
	aead  cipher.AEAD
}

func WrapSecrets(inner Store, key string) (Store, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return inner, nil
	}
	raw, err := secretKeyBytes(key)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecureStore{inner: inner, aead: aead}, nil
}

func (s *SecureStore) Close() error {
	return s.inner.Close()
}

func secretKeyBytes(value string) ([]byte, error) {
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	sum := sha256.Sum256([]byte(value))
	return sum[:], nil
}

func (s *SecureStore) encrypt(value string) string {
	if value == "" || strings.HasPrefix(value, encryptedPrefix) {
		return value
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	sealed := s.aead.Seal(nil, nonce, []byte(value), nil)
	payload := append(nonce, sealed...)
	return encryptedPrefix + base64.RawStdEncoding.EncodeToString(payload)
}

func (s *SecureStore) decrypt(value string) string {
	if !strings.HasPrefix(value, encryptedPrefix) {
		return value
	}
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, encryptedPrefix))
	if err != nil || len(payload) < s.aead.NonceSize() {
		return ""
	}
	nonce := payload[:s.aead.NonceSize()]
	sealed := payload[s.aead.NonceSize():]
	plain, err := s.aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return ""
	}
	return string(plain)
}

func (s *SecureStore) encryptConfig(c model.IssuedConfig) model.IssuedConfig {
	c.Config = s.encrypt(c.Config)
	return c
}

func (s *SecureStore) decryptConfig(c model.IssuedConfig) model.IssuedConfig {
	c.Config = s.decrypt(c.Config)
	return c
}

func (s *SecureStore) CreateAuthIdentity(identity model.AuthIdentity) (model.AuthIdentity, error) {
	return s.inner.CreateAuthIdentity(identity)
}
func (s *SecureStore) GetAuthIdentity(id string) (model.AuthIdentity, error) {
	return s.inner.GetAuthIdentity(id)
}
func (s *SecureStore) UpdateAuthIdentity(identity model.AuthIdentity) (model.AuthIdentity, error) {
	return s.inner.UpdateAuthIdentity(identity)
}
func (s *SecureStore) ListAdminIdentityProfiles() []model.AdminIdentityProfile {
	return s.inner.ListAdminIdentityProfiles()
}
func (s *SecureStore) FindActiveAdminIdentityByLogin(login string) (model.AuthIdentity, error) {
	return s.inner.FindActiveAdminIdentityByLogin(login)
}
func (s *SecureStore) FindActiveUserIdentityByAccountID(accountID string) (model.AuthIdentity, error) {
	return s.inner.FindActiveUserIdentityByAccountID(accountID)
}
func (s *SecureStore) CreateAdminInvite(invite model.AdminInvite) (model.AdminInvite, error) {
	return s.inner.CreateAdminInvite(invite)
}
func (s *SecureStore) ListAdminInvites() []model.AdminInvite {
	return s.inner.ListAdminInvites()
}
func (s *SecureStore) GetAdminInvite(id string) (model.AdminInvite, error) {
	return s.inner.GetAdminInvite(id)
}
func (s *SecureStore) GetAdminInviteByTokenHash(tokenHash string) (model.AdminInvite, error) {
	return s.inner.GetAdminInviteByTokenHash(tokenHash)
}
func (s *SecureStore) FindActiveSystemOwnerInvite(now time.Time) (model.AdminInvite, error) {
	return s.inner.FindActiveSystemOwnerInvite(now)
}
func (s *SecureStore) MarkAdminInviteUsed(inviteID string, identityID string) (model.AdminInvite, error) {
	return s.inner.MarkAdminInviteUsed(inviteID, identityID)
}
func (s *SecureStore) RevokeAdminInvite(inviteID string) (model.AdminInvite, error) {
	return s.inner.RevokeAdminInvite(inviteID)
}
func (s *SecureStore) CreateAuthCredential(credential model.AuthCredential) (model.AuthCredential, error) {
	return s.inner.CreateAuthCredential(credential)
}
func (s *SecureStore) UpdateAuthCredential(credential model.AuthCredential) (model.AuthCredential, error) {
	return s.inner.UpdateAuthCredential(credential)
}
func (s *SecureStore) GetAuthCredential(identityID string, credentialType model.AuthCredentialType) (model.AuthCredential, error) {
	return s.inner.GetAuthCredential(identityID, credentialType)
}
func (s *SecureStore) CreateAuthSession(session model.AuthSession) (model.AuthSession, error) {
	return s.inner.CreateAuthSession(session)
}
func (s *SecureStore) GetAuthSessionByTokenHash(tokenHash string) (model.AuthSession, error) {
	return s.inner.GetAuthSessionByTokenHash(tokenHash)
}
func (s *SecureStore) GetActiveAuthSessionByTokenHash(tokenHash string) (model.AuthSession, error) {
	return s.inner.GetActiveAuthSessionByTokenHash(tokenHash)
}
func (s *SecureStore) GetActiveAuthSessionByRefreshHash(refreshHash string) (model.AuthSession, error) {
	return s.inner.GetActiveAuthSessionByRefreshHash(refreshHash)
}
func (s *SecureStore) GetAuthSessionByRefreshHashForRotation(refreshHash string, graceWindow time.Duration) (model.AuthSession, error) {
	return s.inner.GetAuthSessionByRefreshHashForRotation(refreshHash, graceWindow)
}
func (s *SecureStore) RotateAuthRefreshToken(oldRefreshHash string, tokenHash string, expiresAt time.Time, refreshHash string, refreshExpiresAt time.Time, graceWindow time.Duration) (model.RefreshTokenRotation, error) {
	return s.inner.RotateAuthRefreshToken(oldRefreshHash, tokenHash, expiresAt, refreshHash, refreshExpiresAt, graceWindow)
}
func (s *SecureStore) UpdateAuthSessionTokens(sessionID string, tokenHash string, expiresAt time.Time, refreshHash string, refreshExpiresAt time.Time) (model.AuthSession, error) {
	return s.inner.UpdateAuthSessionTokens(sessionID, tokenHash, expiresAt, refreshHash, refreshExpiresAt)
}
func (s *SecureStore) UpdateAuthSessionPresence(sessionID string, lastSeenAt time.Time, onlineUntil time.Time) (model.AuthSession, error) {
	return s.inner.UpdateAuthSessionPresence(sessionID, lastSeenAt, onlineUntil)
}
func (s *SecureStore) RevokeAuthSessionsByIdentityID(identityID string) error {
	return s.inner.RevokeAuthSessionsByIdentityID(identityID)
}
func (s *SecureStore) RevokeOtherAuthSessionsByIdentityID(identityID string, keepSessionID string) error {
	return s.inner.RevokeOtherAuthSessionsByIdentityID(identityID, keepSessionID)
}
func (s *SecureStore) CreateAuthRecoveryCode(code model.AuthRecoveryCode) (model.AuthRecoveryCode, error) {
	return s.inner.CreateAuthRecoveryCode(code)
}
func (s *SecureStore) ListAuthRecoveryCodes(identityID string) []model.AuthRecoveryCode {
	return s.inner.ListAuthRecoveryCodes(identityID)
}
func (s *SecureStore) MarkAuthRecoveryCodeUsed(id string) (model.AuthRecoveryCode, error) {
	return s.inner.MarkAuthRecoveryCodeUsed(id)
}
func (s *SecureStore) CreateAdminProfile(profile model.AdminProfile) (model.AdminProfile, error) {
	return s.inner.CreateAdminProfile(profile)
}
func (s *SecureStore) GetAdminProfile(identityID string) (model.AdminProfile, error) {
	return s.inner.GetAdminProfile(identityID)
}
func (s *SecureStore) UpdateAdminProfile(profile model.AdminProfile) (model.AdminProfile, error) {
	return s.inner.UpdateAdminProfile(profile)
}

func (s *SecureStore) CreatePolicyTag(tag model.PolicyTag) (model.PolicyTag, error) {
	return s.inner.CreatePolicyTag(tag)
}
func (s *SecureStore) ListPolicyTags() []model.PolicyTag { return s.inner.ListPolicyTags() }
func (s *SecureStore) AssignPolicyTag(tag model.AccountPolicyTag) (model.AccountPolicyTag, error) {
	return s.inner.AssignPolicyTag(tag)
}
func (s *SecureStore) ListAccountPolicyTags(accountID string) []model.AccountPolicyTag {
	return s.inner.ListAccountPolicyTags(accountID)
}

func (s *SecureStore) CreateShortLink(link model.ShortLink) (model.ShortLink, error) {
	return s.inner.CreateShortLink(link)
}
func (s *SecureStore) GetShortLink(id string) (model.ShortLink, error) {
	return s.inner.GetShortLink(id)
}
func (s *SecureStore) GetShortLinkByTokenProfile(tokenID, profile string) (model.ShortLink, error) {
	return s.inner.GetShortLinkByTokenProfile(tokenID, profile)
}

func (s *SecureStore) CreateAuditLog(log model.AuditLog) (model.AuditLog, error) {
	return s.inner.CreateAuditLog(log)
}
func (s *SecureStore) ListAuditLogs() []model.AuditLog { return s.inner.ListAuditLogs() }

func (s *SecureStore) CreateRuntimeCommand(command model.RuntimeCommand) (model.RuntimeCommand, error) {
	return s.inner.CreateRuntimeCommand(command)
}
func (s *SecureStore) ListPendingRuntimeCommands(agentInstallationID string) []model.RuntimeCommand {
	return s.inner.ListPendingRuntimeCommands(agentInstallationID)
}
func (s *SecureStore) ListRuntimeCommands(agentInstallationID string, limit int) []model.RuntimeCommand {
	return s.inner.ListRuntimeCommands(agentInstallationID, limit)
}
func (s *SecureStore) CompleteRuntimeCommandForInstallation(agentInstallationID, id string, status model.RuntimeCommandStatus, result, errMsg string) error {
	return s.inner.CompleteRuntimeCommandForInstallation(agentInstallationID, id, status, result, errMsg)
}
