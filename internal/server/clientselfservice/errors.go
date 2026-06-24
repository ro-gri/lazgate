package clientselfservice

import "laz/internal/server/apperrors"

type ValidationError = apperrors.ValidationError

var ErrForbidden = ValidationError("forbidden")
var ErrConnectionLimitReached = ValidationError("connection limit reached")
