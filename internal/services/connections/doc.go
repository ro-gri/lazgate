// Package connections owns account-associated VPN connection orchestration.
//
// It translates account/client/node intent into local Connection and IssuedConfig
// records, and delegates provider-specific network work to remote.Provider
// implementations from integrations.
package connections
