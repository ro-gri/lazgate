// Package store is the persistence boundary for the application.
//
// It exposes the current storage port and concrete SQLite and Postgres
// implementations. Domain packages should depend on this boundary only through
// narrow store interfaces; concrete persistence details stay here.
package store
