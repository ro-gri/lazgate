package agentprotocol

import "errors"

var (
	ErrMissingStoredConnectionSource  = errors.New("missing stored credential source")
	ErrMissingStoredConnectionBuilder = errors.New("missing stored credential change builder")
)
