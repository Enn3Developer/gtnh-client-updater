package selfupdate

import (
	"crypto/ed25519"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Releases are authenticated by an ed25519 signature over checksums.txt, published as
// checksums.txt.sig. The release workflow signs with the RELEASE_SIGNING_KEY secret
// (internal/cmd/sign); binaries only trust the public key embedded below. Rotating the
// key means old binaries can no longer self-update and must be replaced by hand once.

//go:embed signing_key.pub
var embeddedPublicKey string

// publicKey is the trusted release key; tests replace it.
var publicKey = func() ed25519.PublicKey {
	k, err := ParsePublicKey(embeddedPublicKey)
	if err != nil {
		return nil // a build without a key refuses every self-update (see verifySums)
	}
	return k
}()

// sigContext domain-separates these signatures from anything else the key could sign.
const sigContext = "gtnh-update checksums.txt v1\n"

// SignChecksums signs checksums.txt content. The result is what checksums.txt.sig holds.
func SignChecksums(priv ed25519.PrivateKey, sums []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, signedMessage(sums))) + "\n"
}

// VerifyChecksums checks a checksums.txt.sig against checksums.txt content.
func VerifyChecksums(pub ed25519.PublicKey, sums, sig []byte) error {
	if len(pub) != ed25519.PublicKeySize {
		return errors.New("this build has no release signing key, so it can't verify updates")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || !ed25519.Verify(pub, signedMessage(sums), raw) {
		return errors.New("the update's signature is not valid -- it was not published by the gtnh-update maintainers")
	}
	return nil
}

func signedMessage(sums []byte) []byte {
	return append([]byte(sigContext), sums...)
}

// EncodePublicKey / ParsePublicKey use one line of standard base64.
func EncodePublicKey(pub ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(pub) + "\n"
}

func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("not an ed25519 public key")
	}
	return ed25519.PublicKey(raw), nil
}

// EncodePrivateKey / ParsePrivateKey store the 32-byte seed as standard base64.
func EncodePrivateKey(priv ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(priv.Seed()) + "\n"
}

func ParsePrivateKey(s string) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(raw) != ed25519.SeedSize {
		return nil, fmt.Errorf("not an ed25519 private key seed")
	}
	return ed25519.NewKeyFromSeed(raw), nil
}
