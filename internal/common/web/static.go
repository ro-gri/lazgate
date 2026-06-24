package web

import "embed"

// AssetsFS contains web UI assets grouped by application boundary.
//
//go:embed admin/* client/* common/*
var AssetsFS embed.FS
