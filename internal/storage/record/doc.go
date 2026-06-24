// Package record defines the shapes persisted by storage implementations.
//
// Records intentionally mirror domain models today, but they are distinct types.
// Storage code must map between record and domain shapes at repository
// boundaries, which leaves room for the database schema to diverge later.
package record
