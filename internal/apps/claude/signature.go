package claude

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"

	assets "github.com/PMExtra/RedApp/installers"
)

const SigningFingerprint = "31ddde24ddfab679f42d7bd2baa929ff1a7ecace"

// Signature failures callers or tests may distinguish. All of them reject the
// manifest; none is retryable against the same bytes.
var (
	ErrSignatureLimits      = errors.New("manifest or signature exceeds limits")
	ErrUntrustedKey         = errors.New("untrusted release signing key")
	ErrUnsupportedSignature = errors.New("unsupported manifest signature algorithm or time")
	ErrSignatureMismatch    = errors.New("manifest signature verification failed")
)

// verifier checks a detached manifest signature. Production protocols always
// use Verify; package tests substitute a verifier pinned to a test-only key.
type verifier func(raw, signature []byte) error

// Verify accepts only the reviewed upstream RSA/SHA512 binary signature format.
// The embedded key is never replaced by a key supplied alongside network metadata.
func Verify(raw, signature []byte) error {
	key, err := assets.PublicAsset("anthropic/claude-code", "claude-code.asc")
	if err != nil {
		return errors.New("pinned release signing key is unavailable")
	}
	return verifyPinnedKey(raw, signature, key, SigningFingerprint)
}

func verifyPinnedKey(raw, signature, key []byte, fingerprint string) error {
	if len(raw) == 0 || len(raw) > 1<<20 || len(signature) == 0 || len(signature) > 16<<10 {
		return ErrSignatureLimits
	}
	keys, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(key))
	if err != nil || len(keys) != 1 || hex.EncodeToString(keys[0].PrimaryKey.Fingerprint) != fingerprint {
		return ErrUntrustedKey
	}
	pub, ok := keys[0].PrimaryKey.PublicKey.(*rsa.PublicKey)
	if !ok || pub.N.BitLen() != 4096 {
		return ErrUntrustedKey
	}
	block, err := armor.Decode(bytes.NewReader(signature))
	if err != nil || block.Type != openpgp.SignatureType {
		return ErrSignatureMismatch
	}
	b, err := io.ReadAll(io.LimitReader(block.Body, 16<<10))
	if err != nil {
		return ErrSignatureMismatch
	}
	reader := bytes.NewReader(b)
	p, err := packet.Read(reader)
	if err != nil {
		return ErrSignatureMismatch
	}
	sig, ok := p.(*packet.Signature)
	if !ok || sig.SigType != packet.SigTypeBinary || sig.PubKeyAlgo != packet.PubKeyAlgoRSA || sig.Hash != crypto.SHA512 || sig.CreationTime.After(time.Now().Add(5*time.Minute)) {
		return ErrUnsupportedSignature
	}
	if _, err = packet.Read(reader); err != io.EOF {
		return ErrSignatureMismatch
	}
	if sig.SigLifetimeSecs != nil && *sig.SigLifetimeSecs != 0 && sig.CreationTime.Add(time.Duration(*sig.SigLifetimeSecs)*time.Second).Before(time.Now()) {
		return ErrUnsupportedSignature
	}
	if _, err = openpgp.CheckDetachedSignature(keys, bytes.NewReader(raw), bytes.NewReader(b), nil); err != nil {
		return ErrSignatureMismatch
	}
	return nil
}
