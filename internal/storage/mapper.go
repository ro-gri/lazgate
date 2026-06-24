package store

import (
	"laz/internal/model"
	"laz/internal/storage/mapping"
	"laz/internal/storage/record"
)

var records mapping.Converter = &mapping.ConverterImpl{}

func accountRecord(v model.Account) record.Account          { return records.AccountRecord(v) }
func accountModel(v record.Account) model.Account           { return records.AccountModel(v) }
func clientRecord(v model.Client) record.Client             { return records.ClientRecord(v) }
func clientModel(v record.Client) model.Client              { return records.ClientModel(v) }
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
func accessTokenRecord(v model.AccessToken) record.AccessToken { return records.AccessTokenRecord(v) }
func accessTokenModel(v record.AccessToken) model.AccessToken  { return records.AccessTokenModel(v) }
func adminSessionRecord(v model.AdminSession) record.AdminSession {
	return records.AdminSessionRecord(v)
}
func adminSessionModel(v record.AdminSession) model.AdminSession {
	return records.AdminSessionModel(v)
}
func clientCredentialRecord(v model.ClientCredential) record.ClientCredential {
	return records.ClientCredentialRecord(v)
}
func clientCredentialModel(v record.ClientCredential) model.ClientCredential {
	return records.ClientCredentialModel(v)
}
func clientSessionRecord(v model.ClientSession) record.ClientSession {
	return records.ClientSessionRecord(v)
}
func clientSessionModel(v record.ClientSession) model.ClientSession {
	return records.ClientSessionModel(v)
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
