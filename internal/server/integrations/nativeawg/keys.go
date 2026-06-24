package nativeawg

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/curve25519"
)

func GenerateKeyPair() (privateKey string, publicKey string, err error) {
	var private [32]byte
	if _, err := rand.Read(private[:]); err != nil {
		return "", "", fmt.Errorf("generate native awg private key: %w", err)
	}
	clampWireGuardPrivateKey(private[:])
	public, err := curve25519.X25519(private[:], curve25519.Basepoint)
	if err != nil {
		return "", "", fmt.Errorf("derive native awg public key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(private[:]), base64.StdEncoding.EncodeToString(public), nil
}

func PublicKeyFromPrivate(privateKey string) (string, error) {
	private, err := base64.StdEncoding.DecodeString(privateKey)
	if err != nil {
		return "", fmt.Errorf("decode native awg private key: %w", err)
	}
	if len(private) != 32 {
		return "", fmt.Errorf("native awg private key must be 32 bytes")
	}
	clampWireGuardPrivateKey(private)
	public, err := curve25519.X25519(private, curve25519.Basepoint)
	if err != nil {
		return "", fmt.Errorf("derive native awg public key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(public), nil
}

func GeneratePresharedKey() (string, error) {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return "", fmt.Errorf("generate native awg preshared key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key[:]), nil
}

func clampWireGuardPrivateKey(private []byte) {
	private[0] &= 248
	private[31] &= 127
	private[31] |= 64
}
