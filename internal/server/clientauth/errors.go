package clientauth

import "laz/internal/server/apperrors"

type ValidationError = apperrors.ValidationError

var ErrInvalidCredentials = ValidationError("invalid credentials")
var ErrLocked = ValidationError("client auth is temporarily locked")
var ErrForbidden = ValidationError("forbidden")
var ErrClientLimitReached = ValidationError("client limit reached")
