package view

import "laz/internal/model"

//go:generate go run github.com/jmattheis/goverter/cmd/goverter gen -build-tags '' .

// goverter:converter
// goverter:output:file ./converter_gen.go
// goverter:skipCopySameType
type Converter interface {
	// goverter:ignoreMissing
	ToAccount(model.Account) Account
	// goverter:ignoreMissing
	ToClient(model.Client) Client
	// goverter:ignoreMissing
	ToNode(model.Node) Node
	// goverter:ignoreMissing
	ToConnection(model.Connection) Connection
	// goverter:ignoreMissing
	ToConnectionSummary(model.ConnectionSummary) ConnectionSummary
	// goverter:ignoreMissing
	ToIssuedConfig(model.IssuedConfig) IssuedConfig
	// goverter:ignoreMissing
	ToConfigProfile(model.ConfigProfile) ConfigProfile
	// goverter:ignoreMissing
	ToSession(model.ClientSession) Session
}
