package view

import "laz/internal/server/model"

// goverter:converter
// goverter:output:file ./converter_gen.go
// goverter:skipCopySameType
//
//go:generate go run github.com/jmattheis/goverter/cmd/goverter gen -build-tags '' .
type Converter interface {
	// goverter:map HeaderProtectionKey HeaderProtectionKeySet | HeaderProtectionConfigured
	// goverter:map TimingsJSON Timings | AmneziaTimings
	ToAmneziaSettings(model.AmneziaWGNodeConfig) AmneziaSettings
	// goverter:ignoreMissing
	ToAccount(model.Account) Account
	ToAccounts([]model.Account) []Account
	// goverter:ignoreMissing
	ToNode(model.Node) Node
	ToNodes([]model.Node) []Node
	// goverter:ignoreMissing
	ToConnection(model.Connection) Connection
	// goverter:ignoreMissing
	ToIssuedConfig(model.IssuedConfig) IssuedConfig
	ToIssuedConfigs([]model.IssuedConfig) []IssuedConfig
	// goverter:ignoreMissing
	ToConfigProfile(model.ConfigProfile) ConfigProfile
	ToConfigProfiles([]model.ConfigProfile) []ConfigProfile
	// goverter:ignoreMissing
	ToAccountPortalToken(model.AccountPortalToken) AccountPortalToken
	ToAccountPortalTokens([]model.AccountPortalToken) []AccountPortalToken
	// goverter:ignoreMissing
	ToConnectionSubscriptionToken(model.ConnectionSubscriptionToken) ConnectionSubscriptionToken
	ToConnectionSubscriptionTokens([]model.ConnectionSubscriptionToken) []ConnectionSubscriptionToken
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
