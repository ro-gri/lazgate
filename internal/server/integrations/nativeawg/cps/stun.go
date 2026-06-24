package cps

import (
	"crypto/rand"

	"github.com/pion/stun/v3"
)

func generateSTUN() (Signature, error) {
	var id [stun.TransactionIDSize]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Signature{}, err
	}
	n, err := randomInt(4)
	if err != nil {
		return Signature{}, err
	}
	padding := make([]byte, 12+n*4)
	if _, err := rand.Read(padding); err != nil {
		return Signature{}, err
	}
	message, err := stun.Build(stun.NewTransactionIDSetter(id), stun.BindingRequest,
		stun.RawAttribute{Type: stun.AttrPadding, Value: padding})
	if err != nil {
		return Signature{}, err
	}
	// RFC 5780 PADDING is arbitrary data; no FINGERPRINT or MESSAGE-INTEGRITY is
	// emitted, since either would be invalidated by dynamic CPS substitutions.
	tag, err := randomInt(3)
	if err != nil {
		return Signature{}, err
	}
	// Keep eight fresh static padding bytes to make the CPS template itself new;
	// all 96 transaction-ID bits remain random on every runtime expansion.
	return encode(message.Raw, dynamicField{8, 12, "r"}, dynamicField{32, len(padding) - 8, []string{"r", "rc", "rd"}[tag]})
}
