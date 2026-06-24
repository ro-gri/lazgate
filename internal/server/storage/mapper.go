package store

import (
	"laz/internal/server/model"
	"laz/internal/server/storage/mapping"
	"laz/internal/server/storage/record"
)

var records mapping.Converter = &mapping.ConverterImpl{}

func accountRecord(v model.Account) record.Account          { return records.AccountRecord(v) }
func accountModel(v record.Account) model.Account           { return records.AccountModel(v) }
func nodeRecord(v model.Node) record.Node                   { return records.NodeRecord(v) }
func nodeModel(v record.Node) model.Node                    { return records.NodeModel(v) }
func connectionRecord(v model.Connection) record.Connection { return records.ConnectionRecord(v) }
func connectionModel(v record.Connection) model.Connection  { return records.ConnectionModel(v) }
func issuedConfigRecord(v model.IssuedConfig) record.IssuedConfig {
	return records.IssuedConfigRecord(v)
}
func issuedConfigModel(v record.IssuedConfig) model.IssuedConfig { return records.IssuedConfigModel(v) }
func configProfileRecord(v model.ConfigProfile) record.ConfigProfile {
	return records.ConfigProfileRecord(v)
}
func configProfileModel(v record.ConfigProfile) model.ConfigProfile {
	return records.ConfigProfileModel(v)
}
func accountPortalTokenRecord(v model.AccountPortalToken) record.AccountPortalToken {
	return records.AccountPortalTokenRecord(v)
}
func accountPortalTokenModel(v record.AccountPortalToken) model.AccountPortalToken {
	return records.AccountPortalTokenModel(v)
}
func connectionSubscriptionTokenRecord(v model.ConnectionSubscriptionToken) record.ConnectionSubscriptionToken {
	return records.ConnectionSubscriptionTokenRecord(v)
}
func connectionSubscriptionTokenModel(v record.ConnectionSubscriptionToken) model.ConnectionSubscriptionToken {
	return records.ConnectionSubscriptionTokenModel(v)
}

func policyTagRecord(v model.PolicyTag) record.PolicyTag { return records.PolicyTagRecord(v) }
func policyTagModel(v record.PolicyTag) model.PolicyTag  { return records.PolicyTagModel(v) }
func accountPolicyTagRecord(v model.AccountPolicyTag) record.AccountPolicyTag {
	return records.AccountPolicyTagRecord(v)
}
func accountPolicyTagModel(v record.AccountPolicyTag) model.AccountPolicyTag {
	return records.AccountPolicyTagModel(v)
}
func shortLinkRecord(v model.ShortLink) record.ShortLink { return records.ShortLinkRecord(v) }
func shortLinkModel(v record.ShortLink) model.ShortLink  { return records.ShortLinkModel(v) }
func auditLogRecord(v model.AuditLog) record.AuditLog    { return records.AuditLogRecord(v) }
func auditLogModel(v record.AuditLog) model.AuditLog     { return records.AuditLogModel(v) }
