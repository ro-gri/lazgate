package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"laz/internal/persistence"
	"laz/internal/persistence/sqlutil"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"laz/internal/server/model"
	"laz/internal/server/storage/record"

	_ "modernc.org/sqlite"
)

type SQLStore struct {
	rebinder func(string) string
	db       *sql.DB
	authDB   *sql.DB
	dialect  persistence.Dialect
}

func OpenSQLite(path string) (*SQLStore, error) {
	return openSQLite(path, "")
}

func OpenSQLiteWithAuth(path, authPath string) (*SQLStore, error) {
	if strings.TrimSpace(authPath) == "" {
		return nil, errors.New("auth sqlite database path is required")
	}
	return openSQLite(path, authPath)
}

func openSQLite(path, authPath string) (*SQLStore, error) {
	db, err := openSQLiteDB(path)
	if err != nil {
		return nil, err
	}
	st := &SQLStore{db: db, dialect: persistence.DialectSQLite}
	st.rebinder, err = sqlutil.NewRebinder(st.dialect)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if authPath != "" {
		authDB, err := openSQLiteDB(authPath)
		if err != nil {
			_ = db.Close()
			return nil, err
		}
		st.authDB = authDB
	}
	if err := st.migrate(); err != nil {
		_ = st.Close()
		return nil, err
	}
	return st, nil
}

func openSQLiteDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`pragma foreign_keys = on; pragma busy_timeout = 5000;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func (s *SQLStore) Close() error {
	var err error
	if s.authDB != nil {
		err = s.authDB.Close()
	}
	if closeErr := s.db.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	return err
}

func (s *SQLStore) migrate() error {
	switch s.dialect {
	case persistence.DialectSQLite, persistence.DialectPostgres:
		if err := applyMigrations(s.db, s.dialect); err != nil {
			return err
		}
		if s.authDB != nil {
			if err := applyAuthMigrations(s.authDB, s.dialect); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported sql dialect %q", s.dialect)
	}
}

func (s *SQLStore) exec(query string, args ...any) (sql.Result, error) {
	return s.db.Exec(s.rebind(query), args...)
}

func (s *SQLStore) authExec(query string, args ...any) (sql.Result, error) {
	return s.authDBOrMain().Exec(s.rebind(query), args...)
}

func (s *SQLStore) query(query string, args ...any) (*sql.Rows, error) {
	return s.db.Query(s.rebind(query), args...)
}

func (s *SQLStore) authQuery(query string, args ...any) (*sql.Rows, error) {
	return s.authDBOrMain().Query(s.rebind(query), args...)
}

func (s *SQLStore) queryRow(query string, args ...any) *sql.Row {
	return s.db.QueryRow(s.rebind(query), args...)
}

func (s *SQLStore) authQueryRow(query string, args ...any) *sql.Row {
	return s.authDBOrMain().QueryRow(s.rebind(query), args...)
}

func (s *SQLStore) authDBOrMain() *sql.DB {
	if s.authDB != nil {
		return s.authDB
	}
	return s.db
}

func (s *SQLStore) rebind(query string) string { return s.rebinder(query) }

func (s *SQLStore) CreateAccount(u model.Account) (model.Account, error) {
	t := now()
	u.ID = NewID("usr")
	u.Status = model.AccountStatusActive
	u.CreatedAt = t
	u.UpdatedAt = t
	_, err := s.exec(`insert into accounts(id, username, display_name, status, created_at, updated_at) values(?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.DisplayName, u.Status, ts(u.CreatedAt), ts(u.UpdatedAt))
	return u, err
}

func (s *SQLStore) GetAccount(id string) (model.Account, error) {
	row := s.queryRow(`select id, username, display_name, status, created_at, updated_at from accounts where id = ?`, id)
	return scanAccount(row)
}

func (s *SQLStore) ListAccounts() []model.Account {
	rows, err := s.query(`select id, username, display_name, status, created_at, updated_at from accounts order by username`)
	if err != nil {
		return []model.Account{}
	}
	defer rows.Close()
	out := []model.Account{}
	for rows.Next() {
		u, err := scanAccount(rows)
		if err == nil {
			out = append(out, u)
		}
	}
	return out
}

func (s *SQLStore) UpdateAccountStatus(id string, status model.AccountStatus) (model.Account, error) {
	if status != model.AccountStatusActive && status != model.AccountStatusHeld && status != model.AccountStatusDeleted {
		return model.Account{}, fmt.Errorf("unsupported account status %q", status)
	}
	_, err := s.exec(`update accounts set status = ?, updated_at = ? where id = ?`, status, ts(now()), id)
	if err != nil {
		return model.Account{}, err
	}
	return s.GetAccount(id)
}

func (s *SQLStore) CreateIssuedConfig(c model.IssuedConfig) (model.IssuedConfig, error) {
	t := now()
	c.ID = NewID("cfg")
	c.Status = model.StatusActive
	c.CreatedAt = t
	c.UpdatedAt = t
	_, err := s.exec(`insert into issued_configs(id, connection_id, kind, slug, name, client, content_type, config_encrypted, status, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.ConnectionID, c.Kind, c.Slug, c.Name, c.Client, c.ContentType, c.Config, c.Status, ts(c.CreatedAt), ts(c.UpdatedAt))
	return c, err
}

func (s *SQLStore) ListIssuedConfigs() []model.IssuedConfig {
	rows, err := s.query(`select id, connection_id, kind, slug, name, client, content_type, config_encrypted, status, created_at, updated_at from issued_configs order by created_at`)
	if err != nil {
		return []model.IssuedConfig{}
	}
	defer rows.Close()
	out := []model.IssuedConfig{}
	for rows.Next() {
		c, err := scanConfig(rows)
		if err == nil {
			out = append(out, c)
		}
	}
	return out
}

func (s *SQLStore) CreateConfigProfile(p model.ConfigProfile) (model.ConfigProfile, error) {
	t := now()
	p.ID = NewID("prf")
	p.Status = model.StatusActive
	p.CreatedAt = t
	p.UpdatedAt = t
	_, err := s.exec(`insert into config_profiles(id, protocol, kind, slug, name, client, content_type, config_template, status, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Protocol, p.Kind, p.Slug, p.Name, p.Client, p.ContentType, p.ConfigTemplate, p.Status, ts(p.CreatedAt), ts(p.UpdatedAt))
	return p, err
}

func (s *SQLStore) ListConfigProfiles() []model.ConfigProfile {
	rows, err := s.query(`select id, protocol, kind, slug, name, client, content_type, config_template, status, created_at, updated_at from config_profiles order by protocol, slug`)
	if err != nil {
		return []model.ConfigProfile{}
	}
	defer rows.Close()
	out := []model.ConfigProfile{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err == nil {
			out = append(out, p)
		}
	}
	return out
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return true
	}
	type sqlStateError interface {
		SQLState() string
	}
	var stateErr sqlStateError
	if errors.As(err, &stateErr) && stateErr.SQLState() == "23505" {
		return true
	}
	type codeError interface {
		Code() string
	}
	var coded codeError
	if errors.As(err, &coded) && coded.Code() == "23505" {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") || strings.Contains(message, "duplicate key")
}

func (s *SQLStore) CreateAuthIdentity(identity model.AuthIdentity) (model.AuthIdentity, error) {
	now := now()
	if identity.ID == "" {
		identity.ID = NewID("aid")
	}
	if identity.Status == "" {
		identity.Status = model.StatusActive
	}
	identity.CreatedAt = now
	identity.UpdatedAt = now
	_, err := s.authExec(`insert into auth_identities(id, kind, login, display_name, account_id, status, must_change_password, verified_at, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		identity.ID, identity.Kind, identity.Login, identity.DisplayName, identity.AccountID, identity.Status, boolInt(identity.MustChangePassword), tsOrNil(identity.VerifiedAt), ts(identity.CreatedAt), ts(identity.UpdatedAt))
	return identity, err
}

func (s *SQLStore) GetAuthIdentity(id string) (model.AuthIdentity, error) {
	return scanAuthIdentity(s.authQueryRow(`select id, kind, login, display_name, account_id, status, must_change_password, verified_at, created_at, updated_at from auth_identities where id = ?`, id))
}

func (s *SQLStore) UpdateAuthIdentity(identity model.AuthIdentity) (model.AuthIdentity, error) {
	identity.UpdatedAt = now()
	_, err := s.authExec(`update auth_identities set login = ?, display_name = ?, account_id = ?, status = ?, must_change_password = ?, verified_at = ?, updated_at = ? where id = ?`,
		identity.Login, identity.DisplayName, identity.AccountID, identity.Status, boolInt(identity.MustChangePassword), tsOrNil(identity.VerifiedAt), ts(identity.UpdatedAt), identity.ID)
	if err != nil {
		return model.AuthIdentity{}, err
	}
	return s.GetAuthIdentity(identity.ID)
}

func (s *SQLStore) ListAdminIdentityProfiles() []model.AdminIdentityProfile {
	rows, err := s.authQuery(`select i.id, i.kind, i.login, i.display_name, i.account_id, i.status, i.must_change_password, i.verified_at, i.created_at, i.updated_at, p.id, p.identity_id, p.role, p.status, p.created_at, p.updated_at, max(s.last_seen_at), max(s.online_until) from auth_identities i join admin_profiles p on p.identity_id = i.id left join auth_sessions s on s.identity_id = i.id and s.status = ? where i.kind = ? group by i.id, p.id order by i.created_at`, model.StatusActive, model.AuthIdentityKindAdmin)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []model.AdminIdentityProfile{}
	now := now()
	for rows.Next() {
		item, err := scanAdminIdentityProfile(rows)
		if err == nil {
			item.Online = !item.OnlineUntil.IsZero() && now.Before(item.OnlineUntil)
			out = append(out, item)
		}
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	return out
}

func (s *SQLStore) FindActiveAdminIdentityByLogin(login string) (model.AuthIdentity, error) {
	return scanAuthIdentity(s.authQueryRow(`select id, kind, login, display_name, account_id, status, must_change_password, verified_at, created_at, updated_at from auth_identities where kind = ? and login = ? and status = ? limit 1`, model.AuthIdentityKindAdmin, login, model.StatusActive))
}

func (s *SQLStore) FindActiveUserIdentityByAccountID(accountID string) (model.AuthIdentity, error) {
	return scanAuthIdentity(s.authQueryRow(`select id, kind, login, display_name, account_id, status, must_change_password, verified_at, created_at, updated_at from auth_identities where kind = ? and account_id = ? and status = ? limit 1`, model.AuthIdentityKindUser, accountID, model.StatusActive))
}

func (s *SQLStore) CreateAdminInvite(invite model.AdminInvite) (model.AdminInvite, error) {
	now := now()
	if invite.ID == "" {
		invite.ID = NewID("ain")
	}
	if invite.Status == "" {
		invite.Status = model.AdminInviteStatusActive
	}
	invite.CreatedAt = now
	invite.UpdatedAt = now
	_, err := s.authExec(`insert into admin_invites(id, token_hash, code_hash, role, status, expires_at, invited_by_identity_id, invited_by_login_snapshot, used_by_identity_id, used_at, revoked_at, note, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		invite.ID, invite.TokenHash, invite.CodeHash, invite.Role, invite.Status, ts(invite.ExpiresAt), nilIfEmpty(invite.InvitedByIdentityID), invite.InvitedByLoginSnapshot, nilIfEmpty(invite.UsedByIdentityID), tsOrNil(invite.UsedAt), tsOrNil(invite.RevokedAt), invite.Note, ts(invite.CreatedAt), ts(invite.UpdatedAt))
	return invite, err
}

func (s *SQLStore) ListAdminInvites() []model.AdminInvite {
	rows, err := s.authQuery(adminInviteListSQL + ` order by ai.created_at desc`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.AdminInvite
	for rows.Next() {
		invite, err := scanAdminInviteWithUsedBy(rows)
		if err == nil {
			out = append(out, invite)
		}
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	return out
}

func (s *SQLStore) GetAdminInvite(id string) (model.AdminInvite, error) {
	return scanAdminInvite(s.authQueryRow(adminInviteSelectSQL+` where id = ? limit 1`, id))
}

func (s *SQLStore) GetAdminInviteByTokenHash(tokenHash string) (model.AdminInvite, error) {
	return scanAdminInvite(s.authQueryRow(adminInviteSelectSQL+` where token_hash = ? limit 1`, tokenHash))
}

func (s *SQLStore) FindActiveSystemOwnerInvite(at time.Time) (model.AdminInvite, error) {
	return scanAdminInvite(s.authQueryRow(adminInviteSelectSQL+` where status = ? and role = ? and invited_by_identity_id is null and invited_by_login_snapshot = ? and expires_at > ? order by created_at desc limit 1`, model.AdminInviteStatusActive, model.AdminRoleOwner, "system", ts(at)))
}

func (s *SQLStore) MarkAdminInviteUsed(inviteID string, identityID string) (model.AdminInvite, error) {
	now := now()
	result, err := s.authExec(`update admin_invites set status = ?, used_by_identity_id = ?, used_at = ?, updated_at = ? where id = ? and status = ?`, model.AdminInviteStatusUsed, identityID, ts(now), ts(now), inviteID, model.AdminInviteStatusActive)
	if err != nil {
		return model.AdminInvite{}, err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return model.AdminInvite{}, ErrNotFound
	}
	return s.GetAdminInvite(inviteID)
}

func (s *SQLStore) RevokeAdminInvite(inviteID string) (model.AdminInvite, error) {
	now := now()
	result, err := s.authExec(`update admin_invites set status = ?, revoked_at = ?, updated_at = ? where id = ? and status = ?`, model.AdminInviteStatusRevoked, ts(now), ts(now), inviteID, model.AdminInviteStatusActive)
	if err != nil {
		return model.AdminInvite{}, err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return model.AdminInvite{}, ErrNotFound
	}
	return s.GetAdminInvite(inviteID)
}

func (s *SQLStore) CreateAuthCredential(credential model.AuthCredential) (model.AuthCredential, error) {
	now := now()
	if credential.ID == "" {
		credential.ID = NewID("acr")
	}
	if credential.Status == "" {
		credential.Status = model.StatusActive
	}
	credential.CreatedAt = now
	credential.UpdatedAt = now
	_, err := s.authExec(`insert into auth_credentials(id, identity_id, type, secret_hash, failed_attempts, locked_until, status, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		credential.ID, credential.IdentityID, credential.Type, credential.SecretHash, credential.FailedAttempts, tsOrNil(credential.LockedUntil), credential.Status, ts(credential.CreatedAt), ts(credential.UpdatedAt))
	return credential, err
}

func (s *SQLStore) UpdateAuthCredential(credential model.AuthCredential) (model.AuthCredential, error) {
	credential.UpdatedAt = now()
	_, err := s.authExec(`update auth_credentials set secret_hash = ?, failed_attempts = ?, locked_until = ?, status = ?, updated_at = ? where id = ?`,
		credential.SecretHash, credential.FailedAttempts, tsOrNil(credential.LockedUntil), credential.Status, ts(credential.UpdatedAt), credential.ID)
	if err != nil {
		return model.AuthCredential{}, err
	}
	return s.GetAuthCredential(credential.IdentityID, credential.Type)
}

func (s *SQLStore) GetAuthCredential(identityID string, credentialType model.AuthCredentialType) (model.AuthCredential, error) {
	return scanAuthCredential(s.authQueryRow(`select id, identity_id, type, secret_hash, failed_attempts, locked_until, status, created_at, updated_at from auth_credentials where identity_id = ? and type = ? and status != ? limit 1`, identityID, credentialType, model.StatusDeleted))
}

func (s *SQLStore) CreateAuthSession(session model.AuthSession) (model.AuthSession, error) {
	now := now()
	if session.ID == "" {
		session.ID = NewID("ase")
	}
	if session.Status == "" {
		session.Status = model.StatusActive
	}
	session.CreatedAt = now
	session.UpdatedAt = now
	db := s.authDBOrMain()
	tx, err := db.Begin()
	if err != nil {
		return model.AuthSession{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.Exec(s.rebind(`insert into auth_sessions(id, identity_id, account_id, token_hash, refresh_token_hash, refresh_expires_at, refresh_absolute_until, status, expires_at, last_used_at, last_seen_at, online_until, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		session.ID, session.IdentityID, session.AccountID, session.TokenHash, session.RefreshTokenHash, tsOrNil(session.RefreshExpiresAt), tsOrNil(session.RefreshAbsoluteUntil), session.Status, tsOrNil(session.ExpiresAt), tsOrNil(session.LastUsedAt), tsOrNil(session.LastSeenAt), tsOrNil(session.OnlineUntil), ts(session.CreatedAt), ts(session.UpdatedAt)); err != nil {
		return model.AuthSession{}, err
	}
	if session.RefreshTokenHash != "" {
		if _, err = tx.Exec(s.rebind(`insert into auth_refresh_tokens(id, token_hash, session_id, identity_id, family_id, status, expires_at, absolute_expires_at, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			NewID("art"), session.RefreshTokenHash, session.ID, session.IdentityID, NewID("arf"), model.AuthRefreshTokenStatusActive, tsOrNil(session.RefreshExpiresAt), tsOrNil(session.RefreshAbsoluteUntil), ts(now), ts(now)); err != nil {
			return model.AuthSession{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return model.AuthSession{}, err
	}
	return session, nil
}

func (s *SQLStore) GetAuthSessionByTokenHash(tokenHash string) (model.AuthSession, error) {
	return scanAuthSession(s.authQueryRow(authSessionSelectSQL+` where token_hash = ? limit 1`, tokenHash))
}

func (s *SQLStore) GetActiveAuthSessionByTokenHash(tokenHash string) (model.AuthSession, error) {
	return scanAuthSession(s.authQueryRow(authSessionSelectSQL+` where token_hash = ? and status = ? limit 1`, tokenHash, model.StatusActive))
}

func (s *SQLStore) GetActiveAuthSessionByRefreshHash(refreshHash string) (model.AuthSession, error) {
	return scanAuthSession(s.authQueryRow(authSessionSelectSQL+` join auth_refresh_tokens rt on rt.session_id = auth_sessions.id where rt.token_hash = ? and rt.status = ? and auth_sessions.status = ? limit 1`, refreshHash, model.AuthRefreshTokenStatusActive, model.StatusActive))
}

func (s *SQLStore) GetAuthSessionByRefreshHashForRotation(refreshHash string, graceWindow time.Duration) (model.AuthSession, error) {
	return scanAuthSession(s.authQueryRow(authSessionSelectSQL+` join auth_refresh_tokens rt on rt.session_id = auth_sessions.id where rt.token_hash = ? and auth_sessions.status = ? and (rt.status = ? or (rt.status = ? and rt.rotated_at is not null and rt.rotated_at >= ?)) limit 1`, refreshHash, model.StatusActive, model.AuthRefreshTokenStatusActive, model.AuthRefreshTokenStatusRotated, ts(now().Add(-graceWindow))))
}

func (s *SQLStore) RotateAuthRefreshToken(oldRefreshHash string, tokenHash string, expiresAt time.Time, refreshHash string, refreshExpiresAt time.Time, graceWindow time.Duration) (model.RefreshTokenRotation, error) {
	db := s.authDBOrMain()
	tx, err := db.Begin()
	if err != nil {
		return model.RefreshTokenRotation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	now := now()
	oldToken, err := scanAuthRefreshToken(tx.QueryRow(s.rebind(authRefreshTokenSelectSQL+` where token_hash = ? limit 1`), oldRefreshHash))
	if err != nil {
		return model.RefreshTokenRotation{}, err
	}
	session, err := scanAuthSession(tx.QueryRow(s.rebind(authSessionSelectSQL+` where id = ? limit 1`), oldToken.SessionID))
	if err != nil {
		return model.RefreshTokenRotation{}, err
	}
	if session.Status != model.StatusActive {
		return model.RefreshTokenRotation{}, ErrNotFound
	}
	if oldToken.Status == model.AuthRefreshTokenStatusActive {
		oldToken.RotatedAt = now
		oldToken.LastUsedAt = now
		if _, err = tx.Exec(s.rebind(`update auth_refresh_tokens set status = ?, rotated_at = ?, last_used_at = ?, updated_at = ? where id = ?`), model.AuthRefreshTokenStatusRotated, ts(now), ts(now), ts(now), oldToken.ID); err != nil {
			return model.RefreshTokenRotation{}, err
		}
		if _, err = tx.Exec(s.rebind(`update auth_refresh_tokens set status = ?, updated_at = ? where session_id = ? and status = ? and id != ?`), model.AuthRefreshTokenStatusRevoked, ts(now), session.ID, model.AuthRefreshTokenStatusActive, oldToken.ID); err != nil {
			return model.RefreshTokenRotation{}, err
		}
		session.TokenHash = tokenHash
		session.ExpiresAt = expiresAt
		session.RefreshTokenHash = refreshHash
		session.RefreshExpiresAt = refreshExpiresAt
		session.LastUsedAt = now
		session.UpdatedAt = now
		if _, err = tx.Exec(s.rebind(`update auth_sessions set token_hash = ?, expires_at = ?, refresh_token_hash = ?, refresh_expires_at = ?, last_used_at = ?, updated_at = ? where id = ?`), tokenHash, tsOrNil(expiresAt), refreshHash, tsOrNil(refreshExpiresAt), ts(now), ts(now), session.ID); err != nil {
			return model.RefreshTokenRotation{}, err
		}
		newToken := model.AuthRefreshToken{ID: NewID("art"), TokenHash: refreshHash, SessionID: session.ID, IdentityID: session.IdentityID, FamilyID: oldToken.FamilyID, Status: model.AuthRefreshTokenStatusActive, ExpiresAt: refreshExpiresAt, AbsoluteExpiresAt: oldToken.AbsoluteExpiresAt, CreatedAt: now, UpdatedAt: now}
		if _, err = tx.Exec(s.rebind(`insert into auth_refresh_tokens(id, token_hash, session_id, identity_id, family_id, status, expires_at, absolute_expires_at, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`), newToken.ID, newToken.TokenHash, newToken.SessionID, newToken.IdentityID, newToken.FamilyID, newToken.Status, tsOrNil(newToken.ExpiresAt), tsOrNil(newToken.AbsoluteExpiresAt), ts(newToken.CreatedAt), ts(newToken.UpdatedAt)); err != nil {
			return model.RefreshTokenRotation{}, err
		}
		if err = tx.Commit(); err != nil {
			return model.RefreshTokenRotation{}, err
		}
		return model.RefreshTokenRotation{Session: session, RefreshToken: newToken}, nil
	}
	if oldToken.Status == model.AuthRefreshTokenStatusRotated && !oldToken.RotatedAt.IsZero() && !now.After(oldToken.RotatedAt.Add(graceWindow)) {
		if _, err = tx.Exec(s.rebind(`update auth_refresh_tokens set last_used_at = ?, updated_at = ? where id = ?`), ts(now), ts(now), oldToken.ID); err != nil {
			return model.RefreshTokenRotation{}, err
		}
		currentToken, err := scanAuthRefreshToken(tx.QueryRow(s.rebind(authRefreshTokenSelectSQL+` where session_id = ? and status = ? order by created_at desc limit 1`), session.ID, model.AuthRefreshTokenStatusActive))
		if err != nil {
			return model.RefreshTokenRotation{}, err
		}
		session.RefreshTokenHash = currentToken.TokenHash
		session.RefreshExpiresAt = currentToken.ExpiresAt
		if err = tx.Commit(); err != nil {
			return model.RefreshTokenRotation{}, err
		}
		return model.RefreshTokenRotation{Session: session, RefreshToken: oldToken, GraceRefreshToken: currentToken, Grace: true}, nil
	}
	if _, err = tx.Exec(s.rebind(`update auth_refresh_tokens set status = case when id = ? then ? else ? end, last_used_at = case when id = ? then ? else last_used_at end, updated_at = ? where family_id = ?`), oldToken.ID, model.AuthRefreshTokenStatusReused, model.AuthRefreshTokenStatusRevoked, oldToken.ID, ts(now), ts(now), oldToken.FamilyID); err != nil {
		return model.RefreshTokenRotation{}, err
	}
	if _, err = tx.Exec(s.rebind(`update auth_sessions set status = ?, updated_at = ? where id = ?`), model.StatusDeleted, ts(now), session.ID); err != nil {
		return model.RefreshTokenRotation{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.RefreshTokenRotation{}, err
	}
	return model.RefreshTokenRotation{}, ErrNotFound
}

func (s *SQLStore) UpdateAuthSessionTokens(sessionID string, tokenHash string, expiresAt time.Time, refreshHash string, refreshExpiresAt time.Time) (model.AuthSession, error) {
	_, err := s.authExec(`update auth_sessions set token_hash = ?, expires_at = ?, refresh_token_hash = ?, refresh_expires_at = ?, last_used_at = ?, updated_at = ? where id = ?`,
		tokenHash, tsOrNil(expiresAt), refreshHash, tsOrNil(refreshExpiresAt), ts(now()), ts(now()), sessionID)
	if err != nil {
		return model.AuthSession{}, err
	}
	return scanAuthSession(s.authQueryRow(authSessionSelectSQL+` where id = ?`, sessionID))
}

func (s *SQLStore) UpdateAuthSessionPresence(sessionID string, lastSeenAt time.Time, onlineUntil time.Time) (model.AuthSession, error) {
	_, err := s.authExec(`update auth_sessions set last_seen_at = ?, online_until = ?, updated_at = ? where id = ? and status = ?`, tsOrNil(lastSeenAt), tsOrNil(onlineUntil), ts(now()), sessionID, model.StatusActive)
	if err != nil {
		return model.AuthSession{}, err
	}
	return scanAuthSession(s.authQueryRow(authSessionSelectSQL+` where id = ?`, sessionID))
}

func (s *SQLStore) RevokeAuthSessionsByIdentityID(identityID string) error {
	now := now()
	db := s.authDBOrMain()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.Exec(s.rebind(`update auth_sessions set status = ?, updated_at = ? where identity_id = ? and status != ?`), model.StatusDeleted, ts(now), identityID, model.StatusDeleted); err != nil {
		return err
	}
	if _, err = tx.Exec(s.rebind(`update auth_refresh_tokens set status = ?, updated_at = ? where identity_id = ? and status in (?, ?)`), model.AuthRefreshTokenStatusRevoked, ts(now), identityID, model.AuthRefreshTokenStatusActive, model.AuthRefreshTokenStatusRotated); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStore) RevokeOtherAuthSessionsByIdentityID(identityID string, keepSessionID string) error {
	now := now()
	db := s.authDBOrMain()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.Exec(s.rebind(`update auth_sessions set status = ?, updated_at = ? where identity_id = ? and id != ? and status != ?`), model.StatusDeleted, ts(now), identityID, keepSessionID, model.StatusDeleted); err != nil {
		return err
	}
	if _, err = tx.Exec(s.rebind(`update auth_refresh_tokens set status = ?, updated_at = ? where identity_id = ? and session_id != ? and status in (?, ?)`), model.AuthRefreshTokenStatusRevoked, ts(now), identityID, keepSessionID, model.AuthRefreshTokenStatusActive, model.AuthRefreshTokenStatusRotated); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStore) CreateAuthRecoveryCode(code model.AuthRecoveryCode) (model.AuthRecoveryCode, error) {
	now := now()
	if code.ID == "" {
		code.ID = NewID("arc")
	}
	if code.Status == "" {
		code.Status = model.StatusActive
	}
	code.CreatedAt = now
	code.UpdatedAt = now
	_, err := s.authExec(`insert into auth_recovery_codes(id, identity_id, code_hash, status, used_at, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?)`,
		code.ID, code.IdentityID, code.CodeHash, code.Status, tsOrNil(code.UsedAt), ts(code.CreatedAt), ts(code.UpdatedAt))
	return code, err
}

func (s *SQLStore) ListAuthRecoveryCodes(identityID string) []model.AuthRecoveryCode {
	rows, err := s.authQuery(`select id, identity_id, code_hash, status, used_at, created_at, updated_at from auth_recovery_codes where identity_id = ? order by created_at`, identityID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.AuthRecoveryCode
	for rows.Next() {
		code, err := scanAuthRecoveryCode(rows)
		if err == nil {
			out = append(out, code)
		}
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	return out
}

func (s *SQLStore) MarkAuthRecoveryCodeUsed(id string) (model.AuthRecoveryCode, error) {
	now := now()
	_, err := s.authExec(`update auth_recovery_codes set status = ?, used_at = ?, updated_at = ? where id = ?`, model.StatusDeleted, ts(now), ts(now), id)
	if err != nil {
		return model.AuthRecoveryCode{}, err
	}
	return scanAuthRecoveryCode(s.authQueryRow(`select id, identity_id, code_hash, status, used_at, created_at, updated_at from auth_recovery_codes where id = ?`, id))
}

func (s *SQLStore) CreateAdminProfile(profile model.AdminProfile) (model.AdminProfile, error) {
	now := now()
	if profile.ID == "" {
		profile.ID = NewID("apr")
	}
	if profile.Status == "" {
		profile.Status = model.StatusActive
	}
	profile.CreatedAt = now
	profile.UpdatedAt = now
	_, err := s.authExec(`insert into admin_profiles(id, identity_id, role, status, created_at, updated_at) values(?, ?, ?, ?, ?, ?)`,
		profile.ID, profile.IdentityID, profile.Role, profile.Status, ts(profile.CreatedAt), ts(profile.UpdatedAt))
	return profile, err
}

func (s *SQLStore) GetAdminProfile(identityID string) (model.AdminProfile, error) {
	return scanAdminProfile(s.authQueryRow(`select id, identity_id, role, status, created_at, updated_at from admin_profiles where identity_id = ?`, identityID))
}

func (s *SQLStore) UpdateAdminProfile(profile model.AdminProfile) (model.AdminProfile, error) {
	profile.UpdatedAt = now()
	_, err := s.authExec(`update admin_profiles set role = ?, status = ?, updated_at = ? where id = ?`, profile.Role, profile.Status, ts(profile.UpdatedAt), profile.ID)
	if err != nil {
		return model.AdminProfile{}, err
	}
	return s.GetAdminProfile(profile.IdentityID)
}

func (s *SQLStore) CreatePolicyTag(tag model.PolicyTag) (model.PolicyTag, error) {
	now := now()
	tag.ID = NewID("tag")
	tag.Status = model.StatusActive
	tag.CreatedAt = now
	tag.UpdatedAt = now
	if tag.ConnectionLimit == 0 {
		tag.ConnectionLimit = model.ConnectionLimitUnlimited
	}
	allowed, err := json.Marshal(tag.AllowedNodeIDs)
	if err != nil {
		return model.PolicyTag{}, err
	}
	_, err = s.exec(`insert into policy_tags(id, slug, name, allowed_node_ids, connection_limit, status, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?)`,
		tag.ID, tag.Slug, tag.Name, string(allowed), tag.ConnectionLimit, tag.Status, ts(tag.CreatedAt), ts(tag.UpdatedAt))
	return tag, err
}

func (s *SQLStore) ListPolicyTags() []model.PolicyTag {
	rows, err := s.query(`select id, slug, name, allowed_node_ids, connection_limit, status, created_at, updated_at from policy_tags order by slug`)
	if err != nil {
		return []model.PolicyTag{}
	}
	defer rows.Close()
	out := []model.PolicyTag{}
	for rows.Next() {
		tag, err := scanPolicyTag(rows)
		if err == nil {
			out = append(out, tag)
		}
	}
	return out
}

func (s *SQLStore) AssignPolicyTag(userTag model.AccountPolicyTag) (model.AccountPolicyTag, error) {
	if _, err := s.GetAccount(userTag.AccountID); err != nil {
		return model.AccountPolicyTag{}, err
	}
	now := now()
	userTag.ID = NewID("utag")
	userTag.CreatedAt = now
	_, err := s.exec(`insert into account_policy_tags(id, account_id, tag_id, created_at) values(?, ?, ?, ?) on conflict(account_id, tag_id) do nothing`,
		userTag.ID, userTag.AccountID, userTag.TagID, ts(userTag.CreatedAt))
	if err != nil {
		return model.AccountPolicyTag{}, err
	}
	return userTag, nil
}

func (s *SQLStore) ListAccountPolicyTags(accountID string) []model.AccountPolicyTag {
	rows, err := s.query(`select id, account_id, tag_id, created_at from account_policy_tags where account_id = ? order by created_at`, accountID)
	if err != nil {
		return []model.AccountPolicyTag{}
	}
	defer rows.Close()
	out := []model.AccountPolicyTag{}
	for rows.Next() {
		userTag, err := scanAccountPolicyTag(rows)
		if err == nil {
			out = append(out, userTag)
		}
	}
	return out
}

func (s *SQLStore) CreateAuditLog(log model.AuditLog) (model.AuditLog, error) {
	log.ID = NewID("aud")
	log.CreatedAt = now()
	_, err := s.exec(`insert into audit_logs(id, actor, action, entity_type, entity_id, details, created_at) values(?, ?, ?, ?, ?, ?, ?)`,
		log.ID, log.Actor, log.Action, log.EntityType, log.EntityID, log.Details, ts(log.CreatedAt))
	return log, err
}

func (s *SQLStore) ListAuditLogs() []model.AuditLog {
	rows, err := s.query(`select id, actor, action, entity_type, entity_id, details, created_at from audit_logs order by created_at desc`)
	if err != nil {
		return []model.AuditLog{}
	}
	defer rows.Close()
	out := []model.AuditLog{}
	for rows.Next() {
		log, err := scanAuditLog(rows)
		if err == nil {
			out = append(out, log)
		}
	}
	return out
}

func scanAccount(row scanner) (model.Account, error) {
	var u record.Account
	var created, updated string
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Status, &created, &updated)
	u.CreatedAt = parseTS(created)
	u.UpdatedAt = parseTS(updated)
	return accountModel(u), convertErr(err)
}

func scanConfig(row scanner) (model.IssuedConfig, error) {
	var c record.IssuedConfig
	var created, updated string
	err := row.Scan(&c.ID, &c.ConnectionID, &c.Kind, &c.Slug, &c.Name, &c.Client, &c.ContentType, &c.Config, &c.Status, &created, &updated)
	c.CreatedAt = parseTS(created)
	c.UpdatedAt = parseTS(updated)
	return issuedConfigModel(c), convertErr(err)
}

func scanProfile(row scanner) (model.ConfigProfile, error) {
	var p record.ConfigProfile
	var created, updated string
	err := row.Scan(&p.ID, &p.Protocol, &p.Kind, &p.Slug, &p.Name, &p.Client, &p.ContentType, &p.ConfigTemplate, &p.Status, &created, &updated)
	p.CreatedAt = parseTS(created)
	p.UpdatedAt = parseTS(updated)
	return configProfileModel(p), convertErr(err)
}

func scanAuthIdentity(row scanner) (model.AuthIdentity, error) {
	var identity record.AuthIdentity
	var mustChangePassword int
	var verifiedAt sql.NullString
	var created, updated string
	err := row.Scan(&identity.ID, &identity.Kind, &identity.Login, &identity.DisplayName, &identity.AccountID, &identity.Status, &mustChangePassword, &verifiedAt, &created, &updated)
	identity.MustChangePassword = mustChangePassword != 0
	identity.VerifiedAt = parseNullTS(verifiedAt)
	identity.CreatedAt = parseTS(created)
	identity.UpdatedAt = parseTS(updated)
	return model.AuthIdentity{
		ID:                 identity.ID,
		Kind:               model.AuthIdentityKind(identity.Kind),
		Login:              identity.Login,
		DisplayName:        identity.DisplayName,
		AccountID:          identity.AccountID,
		Status:             model.Status(identity.Status),
		MustChangePassword: identity.MustChangePassword,
		VerifiedAt:         identity.VerifiedAt,
		CreatedAt:          identity.CreatedAt,
		UpdatedAt:          identity.UpdatedAt,
	}, convertErr(err)
}

const adminInviteColumnsSQL = `id, token_hash, code_hash, role, status, expires_at, invited_by_identity_id, invited_by_login_snapshot, used_by_identity_id, used_at, revoked_at, note, created_at, updated_at`

const adminInviteSelectSQL = `select ` + adminInviteColumnsSQL + ` from admin_invites`

const adminInviteListSQL = `select ai.id, ai.token_hash, ai.code_hash, ai.role, ai.status, ai.expires_at, ai.invited_by_identity_id, ai.invited_by_login_snapshot, ai.used_by_identity_id, ai.used_at, ai.revoked_at, ai.note, ai.created_at, ai.updated_at, coalesce(used.login, ''), coalesce(used.display_name, '') from admin_invites ai left join auth_identities used on used.id = ai.used_by_identity_id`

func scanAdminInvite(row scanner) (model.AdminInvite, error) {
	invite, err := scanAdminInviteBase(row)
	return invite, convertErr(err)
}

func scanAdminInviteWithUsedBy(row scanner) (model.AdminInvite, error) {
	var usedByLogin, usedByDisplayName string
	invite, err := scanAdminInviteBase(row, &usedByLogin, &usedByDisplayName)
	invite.UsedByLogin = usedByLogin
	invite.UsedByDisplayName = usedByDisplayName
	return invite, convertErr(err)
}

func scanAdminInviteBase(row scanner, extra ...any) (model.AdminInvite, error) {
	var invite model.AdminInvite
	var invitedBy, usedBy, usedAt, revokedAt sql.NullString
	var expiresAt, createdAt, updatedAt string
	dest := []any{&invite.ID, &invite.TokenHash, &invite.CodeHash, &invite.Role, &invite.Status, &expiresAt, &invitedBy, &invite.InvitedByLoginSnapshot, &usedBy, &usedAt, &revokedAt, &invite.Note, &createdAt, &updatedAt}
	dest = append(dest, extra...)
	err := row.Scan(dest...)
	invite.InvitedByIdentityID = invitedBy.String
	invite.UsedByIdentityID = usedBy.String
	invite.ExpiresAt = parseTS(expiresAt)
	invite.UsedAt = parseNullTS(usedAt)
	invite.RevokedAt = parseNullTS(revokedAt)
	invite.CreatedAt = parseTS(createdAt)
	invite.UpdatedAt = parseTS(updatedAt)
	return invite, err
}

func scanAdminIdentityProfile(row scanner) (model.AdminIdentityProfile, error) {
	var identity record.AuthIdentity
	var verifiedAt sql.NullString
	var identityCreated, identityUpdated string
	var profile record.AdminProfile
	var profileCreated, profileUpdated string
	var lastSeen, onlineUntil sql.NullString
	err := row.Scan(
		&identity.ID, &identity.Kind, &identity.Login, &identity.DisplayName, &identity.AccountID, &identity.Status, &identity.MustChangePassword, &verifiedAt, &identityCreated, &identityUpdated,
		&profile.ID, &profile.IdentityID, &profile.Role, &profile.Status, &profileCreated, &profileUpdated, &lastSeen, &onlineUntil,
	)
	identity.VerifiedAt = parseNullTS(verifiedAt)
	identity.CreatedAt = parseTS(identityCreated)
	identity.UpdatedAt = parseTS(identityUpdated)
	profile.CreatedAt = parseTS(profileCreated)
	profile.UpdatedAt = parseTS(profileUpdated)
	return model.AdminIdentityProfile{
		Identity: model.AuthIdentity{
			ID:                 identity.ID,
			Kind:               model.AuthIdentityKind(identity.Kind),
			Login:              identity.Login,
			DisplayName:        identity.DisplayName,
			AccountID:          identity.AccountID,
			Status:             model.Status(identity.Status),
			MustChangePassword: identity.MustChangePassword,
			VerifiedAt:         identity.VerifiedAt,
			CreatedAt:          identity.CreatedAt,
			UpdatedAt:          identity.UpdatedAt,
		},
		Profile: model.AdminProfile{
			ID:         profile.ID,
			IdentityID: profile.IdentityID,
			Role:       model.AdminRole(profile.Role),
			Status:     model.Status(profile.Status),
			CreatedAt:  profile.CreatedAt,
			UpdatedAt:  profile.UpdatedAt,
		},
		LastSeenAt:  parseNullTS(lastSeen),
		OnlineUntil: parseNullTS(onlineUntil),
	}, convertErr(err)
}

func scanAuthCredential(row scanner) (model.AuthCredential, error) {
	var credential record.AuthCredential
	var lockedUntil sql.NullString
	var created, updated string
	err := row.Scan(&credential.ID, &credential.IdentityID, &credential.Type, &credential.SecretHash, &credential.FailedAttempts, &lockedUntil, &credential.Status, &created, &updated)
	credential.LockedUntil = parseNullTS(lockedUntil)
	credential.CreatedAt = parseTS(created)
	credential.UpdatedAt = parseTS(updated)
	return model.AuthCredential{
		ID:             credential.ID,
		IdentityID:     credential.IdentityID,
		Type:           model.AuthCredentialType(credential.Type),
		SecretHash:     credential.SecretHash,
		FailedAttempts: credential.FailedAttempts,
		LockedUntil:    credential.LockedUntil,
		Status:         model.Status(credential.Status),
		CreatedAt:      credential.CreatedAt,
		UpdatedAt:      credential.UpdatedAt,
	}, convertErr(err)
}

const authSessionSelectSQL = `select auth_sessions.id, auth_sessions.identity_id, auth_sessions.account_id, auth_sessions.token_hash, auth_sessions.refresh_token_hash, auth_sessions.refresh_expires_at, auth_sessions.refresh_absolute_until, auth_sessions.status, auth_sessions.expires_at, auth_sessions.last_used_at, auth_sessions.last_seen_at, auth_sessions.online_until, auth_sessions.created_at, auth_sessions.updated_at from auth_sessions`

func scanAuthSession(row scanner) (model.AuthSession, error) {
	var session record.AuthSession
	var refreshExpires, refreshAbsoluteUntil, expires, lastUsed, lastSeen, onlineUntil sql.NullString
	var created, updated string
	err := row.Scan(&session.ID, &session.IdentityID, &session.AccountID, &session.TokenHash, &session.RefreshTokenHash, &refreshExpires, &refreshAbsoluteUntil, &session.Status, &expires, &lastUsed, &lastSeen, &onlineUntil, &created, &updated)
	session.RefreshExpiresAt = parseNullTS(refreshExpires)
	session.RefreshAbsoluteUntil = parseNullTS(refreshAbsoluteUntil)
	session.ExpiresAt = parseNullTS(expires)
	session.LastUsedAt = parseNullTS(lastUsed)
	session.LastSeenAt = parseNullTS(lastSeen)
	session.OnlineUntil = parseNullTS(onlineUntil)
	session.CreatedAt = parseTS(created)
	session.UpdatedAt = parseTS(updated)
	return model.AuthSession{
		ID:                   session.ID,
		IdentityID:           session.IdentityID,
		AccountID:            session.AccountID,
		TokenHash:            session.TokenHash,
		RefreshTokenHash:     session.RefreshTokenHash,
		RefreshExpiresAt:     session.RefreshExpiresAt,
		RefreshAbsoluteUntil: session.RefreshAbsoluteUntil,
		Status:               model.Status(session.Status),
		ExpiresAt:            session.ExpiresAt,
		LastUsedAt:           session.LastUsedAt,
		LastSeenAt:           session.LastSeenAt,
		OnlineUntil:          session.OnlineUntil,
		CreatedAt:            session.CreatedAt,
		UpdatedAt:            session.UpdatedAt,
	}, convertErr(err)
}

const authRefreshTokenSelectSQL = `select id, token_hash, session_id, identity_id, family_id, status, expires_at, absolute_expires_at, rotated_at, last_used_at, created_at, updated_at from auth_refresh_tokens`

func scanAuthRefreshToken(row scanner) (model.AuthRefreshToken, error) {
	var token record.AuthRefreshToken
	var expires, absoluteExpires, rotated, lastUsed sql.NullString
	var created, updated string
	err := row.Scan(&token.ID, &token.TokenHash, &token.SessionID, &token.IdentityID, &token.FamilyID, &token.Status, &expires, &absoluteExpires, &rotated, &lastUsed, &created, &updated)
	token.ExpiresAt = parseNullTS(expires)
	token.AbsoluteExpiresAt = parseNullTS(absoluteExpires)
	token.RotatedAt = parseNullTS(rotated)
	token.LastUsedAt = parseNullTS(lastUsed)
	token.CreatedAt = parseTS(created)
	token.UpdatedAt = parseTS(updated)
	return model.AuthRefreshToken{
		ID:                token.ID,
		TokenHash:         token.TokenHash,
		SessionID:         token.SessionID,
		IdentityID:        token.IdentityID,
		FamilyID:          token.FamilyID,
		Status:            model.AuthRefreshTokenStatus(token.Status),
		ExpiresAt:         token.ExpiresAt,
		AbsoluteExpiresAt: token.AbsoluteExpiresAt,
		RotatedAt:         token.RotatedAt,
		LastUsedAt:        token.LastUsedAt,
		CreatedAt:         token.CreatedAt,
		UpdatedAt:         token.UpdatedAt,
	}, convertErr(err)
}

func scanAuthRecoveryCode(row scanner) (model.AuthRecoveryCode, error) {
	var code record.AuthRecoveryCode
	var usedAt sql.NullString
	var created, updated string
	err := row.Scan(&code.ID, &code.IdentityID, &code.CodeHash, &code.Status, &usedAt, &created, &updated)
	code.UsedAt = parseNullTS(usedAt)
	code.CreatedAt = parseTS(created)
	code.UpdatedAt = parseTS(updated)
	return model.AuthRecoveryCode{
		ID:         code.ID,
		IdentityID: code.IdentityID,
		CodeHash:   code.CodeHash,
		Status:     model.Status(code.Status),
		UsedAt:     code.UsedAt,
		CreatedAt:  code.CreatedAt,
		UpdatedAt:  code.UpdatedAt,
	}, convertErr(err)
}

func scanAdminProfile(row scanner) (model.AdminProfile, error) {
	var profile record.AdminProfile
	var created, updated string
	err := row.Scan(&profile.ID, &profile.IdentityID, &profile.Role, &profile.Status, &created, &updated)
	profile.CreatedAt = parseTS(created)
	profile.UpdatedAt = parseTS(updated)
	return model.AdminProfile{
		ID:         profile.ID,
		IdentityID: profile.IdentityID,
		Role:       model.AdminRole(profile.Role),
		Status:     model.Status(profile.Status),
		CreatedAt:  profile.CreatedAt,
		UpdatedAt:  profile.UpdatedAt,
	}, convertErr(err)
}

func scanPolicyTag(row scanner) (model.PolicyTag, error) {
	var tag record.PolicyTag
	var allowed string
	var created, updated string
	err := row.Scan(&tag.ID, &tag.Slug, &tag.Name, &allowed, &tag.ConnectionLimit, &tag.Status, &created, &updated)
	_ = json.Unmarshal([]byte(allowed), &tag.AllowedNodeIDs)
	tag.CreatedAt = parseTS(created)
	tag.UpdatedAt = parseTS(updated)
	return policyTagModel(tag), convertErr(err)
}

func scanAccountPolicyTag(row scanner) (model.AccountPolicyTag, error) {
	var userTag record.AccountPolicyTag
	var created string
	err := row.Scan(&userTag.ID, &userTag.AccountID, &userTag.TagID, &created)
	userTag.CreatedAt = parseTS(created)
	return accountPolicyTagModel(userTag), convertErr(err)
}

func scanAuditLog(row scanner) (model.AuditLog, error) {
	var log record.AuditLog
	var created string
	err := row.Scan(&log.ID, &log.Actor, &log.Action, &log.EntityType, &log.EntityID, &log.Details, &created)
	log.CreatedAt = parseTS(created)
	return auditLogModel(log), convertErr(err)
}

func convertErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func ts(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func tsOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return ts(t)
}

func nilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func parseTS(value string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, value)
	return t
}

func parseNullTS(value sql.NullString) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return parseTS(value.String)
}
