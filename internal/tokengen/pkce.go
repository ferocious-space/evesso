package tokengen

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

type pkce struct {
	state               string
	codeVerifier        string
	codeChallange       string
	codeChallangeMethod string
}

func makePKCE(CharacterName string) *pkce {
	sha := sha256.New()

	verifier := make([]byte, 32)
	if n, err := rand.Read(verifier); err != nil || n != 32 {
		return nil
	}

	nonce := verifier[:12]

	encodedVerifier := base64.RawURLEncoding.EncodeToString(verifier)
	shaEncodedVerifier := sha.Sum([]byte(encodedVerifier))
	challange := base64.RawURLEncoding.EncodeToString(shaEncodedVerifier)

	as, err := aes.NewCipher(verifier)
	if err != nil {
		return nil
	}

	aesgcm, err := cipher.NewGCM(as)
	if err != nil {
		return nil
	}

	binState := aesgcm.Seal(nil, nonce, []byte(CharacterName), nil)

	return &pkce{
		state:               base64.RawURLEncoding.EncodeToString(binState),
		codeVerifier:        encodedVerifier,
		codeChallange:       challange,
		codeChallangeMethod: "S256",
	}
}
