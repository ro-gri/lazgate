// Package admin exposes the admin HTTP API and embedded admin UI.
//
// It should stay thin: decode requests, enforce admin auth/permissions, call
// account/connection services, prepare admin views, and record audit events.
package admin
