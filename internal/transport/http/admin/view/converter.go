package view

import "laz/internal/model"

//go:generate go run github.com/jmattheis/goverter/cmd/goverter gen -build-tags '' .

// goverter:converter
// goverter:output:file ./converter_gen.go
// goverter:skipCopySameType
type Converter interface {
	// goverter:ignoreMissing
	ToAccount(model.Account) Account
	ToAccounts([]model.Account) []Account
	// goverter:ignoreMissing
	ToClient(model.Client) Client
	ToClients([]model.Client) []Client
	// goverter:ignoreMissing
	ToNode(model.Node) Node
	ToNodes([]model.Node) []Node
	// goverter:ignoreMissing
	ToConnection(model.Connection) Connection
	ToConnections([]model.Connection) []Connection
	// goverter:ignoreMissing
	ToConnectionSummary(model.ConnectionSummary) ConnectionSummary
	ToConnectionSummaries([]model.ConnectionSummary) []ConnectionSummary
	// goverter:ignoreMissing
	ToIssuedConfig(model.IssuedConfig) IssuedConfig
	ToIssuedConfigs([]model.IssuedConfig) []IssuedConfig
	// goverter:ignoreMissing
	ToConfigProfile(model.ConfigProfile) ConfigProfile
	ToConfigProfiles([]model.ConfigProfile) []ConfigProfile
	// goverter:ignoreMissing
	ToAccessToken(model.AccessToken) AccessToken
	ToAccessTokens([]model.AccessToken) []AccessToken
	// goverter:ignoreMissing
	ToPolicyTag(model.PolicyTag) PolicyTag
	ToPolicyTags([]model.PolicyTag) []PolicyTag
	// goverter:ignoreMissing
	ToAccountPolicyTag(model.AccountPolicyTag) AccountPolicyTag
	ToAccountPolicyTags([]model.AccountPolicyTag) []AccountPolicyTag
	// goverter:ignoreMissing
	ToAuditLog(model.AuditLog) AuditLog
	ToAuditLogs([]model.AuditLog) []AuditLog
}
