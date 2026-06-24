package view

import "laz/internal/server/model"

// goverter:converter
// goverter:output:file ./converter_gen.go
// goverter:skipCopySameType
//
//go:generate go run github.com/jmattheis/goverter/cmd/goverter gen -build-tags '' .
type Converter interface {
	// goverter:ignoreMissing
	ToAccount(model.Account) Account
	// goverter:ignoreMissing
	ToNode(model.Node) Node
	// goverter:ignoreMissing
	ToConnection(model.Connection) Connection
	// goverter:ignoreMissing
	ToIssuedConfig(model.IssuedConfig) IssuedConfig
	// goverter:ignoreMissing
	ToConfigProfile(model.ConfigProfile) ConfigProfile
	// goverter:ignoreMissing
	ToSession(model.SessionView) Session
}
