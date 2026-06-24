package mapping

import (
	"laz/internal/server/model"
	"laz/internal/server/storage/record"
)

//go:generate go run github.com/jmattheis/goverter/cmd/goverter gen -build-tags '' .

// goverter:converter
// goverter:output:file ./converter_gen.go
// goverter:skipCopySameType
type Converter interface {
	// goverter:useUnderlyingTypeMethods
	AccountRecord(model.Account) record.Account
	// goverter:useUnderlyingTypeMethods
	AccountModel(record.Account) model.Account
	// goverter:useUnderlyingTypeMethods
	NodeRecord(model.Node) record.Node
	// goverter:useUnderlyingTypeMethods
	NodeModel(record.Node) model.Node
	// goverter:useUnderlyingTypeMethods
	ConnectionRecord(model.Connection) record.Connection
	// goverter:useUnderlyingTypeMethods
	ConnectionModel(record.Connection) model.Connection
	// goverter:useUnderlyingTypeMethods
	IssuedConfigRecord(model.IssuedConfig) record.IssuedConfig
	// goverter:useUnderlyingTypeMethods
	IssuedConfigModel(record.IssuedConfig) model.IssuedConfig
	// goverter:useUnderlyingTypeMethods
	ConfigProfileRecord(model.ConfigProfile) record.ConfigProfile
	// goverter:useUnderlyingTypeMethods
	ConfigProfileModel(record.ConfigProfile) model.ConfigProfile
	// goverter:useUnderlyingTypeMethods
	AccountPortalTokenRecord(model.AccountPortalToken) record.AccountPortalToken
	// goverter:useUnderlyingTypeMethods
	AccountPortalTokenModel(record.AccountPortalToken) model.AccountPortalToken
	// goverter:useUnderlyingTypeMethods
	ConnectionSubscriptionTokenRecord(model.ConnectionSubscriptionToken) record.ConnectionSubscriptionToken
	// goverter:useUnderlyingTypeMethods
	ConnectionSubscriptionTokenModel(record.ConnectionSubscriptionToken) model.ConnectionSubscriptionToken

	// goverter:useUnderlyingTypeMethods
	PolicyTagRecord(model.PolicyTag) record.PolicyTag
	// goverter:useUnderlyingTypeMethods
	PolicyTagModel(record.PolicyTag) model.PolicyTag
	// goverter:useUnderlyingTypeMethods
	AccountPolicyTagRecord(model.AccountPolicyTag) record.AccountPolicyTag
	// goverter:useUnderlyingTypeMethods
	AccountPolicyTagModel(record.AccountPolicyTag) model.AccountPolicyTag
	// goverter:useUnderlyingTypeMethods
	ShortLinkRecord(model.ShortLink) record.ShortLink
	// goverter:useUnderlyingTypeMethods
	ShortLinkModel(record.ShortLink) model.ShortLink
	// goverter:useUnderlyingTypeMethods
	AuditLogRecord(model.AuditLog) record.AuditLog
	// goverter:useUnderlyingTypeMethods
	AuditLogModel(record.AuditLog) model.AuditLog
}
