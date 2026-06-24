// Package accounts owns account, client, policy, and enrollment workflows.
//
// It coordinates account-level decisions and may ask the connections layer to
// provision connection, but it must not talk to VPN server integrations directly.
package accounts
