package adminauth

type AuthError string

func (e AuthError) Error() string { return string(e) }

const ErrInvalidCredentials = AuthError("invalid credentials")
