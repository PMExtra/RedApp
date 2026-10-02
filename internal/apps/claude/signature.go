package claude

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	_ "crypto/sha512"
	"encoding/hex"
	"errors"
	assets "github.com/PMExtra/RedApp/installers/claude-code"
	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
	"golang.org/x/crypto/openpgp/packet"
	"io"
	"time"
)

const SigningFingerprint = "31ddde24ddfab679f42d7bd2baa929ff1a7ecace"

// Only the reviewed upstream RSA/SHA512 binary signature format is accepted.
// The embedded key is never replaced by a key supplied alongside network metadata.
func Verify(raw, signature []byte) error { return verifyWithKey(raw, signature, assets.PublicKey()) }

func verifyWithKey(raw, signature, key []byte) error {
	if len(raw) == 0 || len(raw) > 1<<20 || len(signature) == 0 || len(signature) > 16<<10 {
		return errors.New("Manifest or signature exceeds limits")
	}
	keys, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(key))
	if err != nil || len(keys) != 1 || hex.EncodeToString(keys[0].PrimaryKey.Fingerprint[:]) != SigningFingerprint {
		return errors.New("Untrusted release signing key")
	}
	pub, ok := keys[0].PrimaryKey.PublicKey.(*rsa.PublicKey)
	if !ok || pub.N.BitLen() != 4096 {
		return errors.New("Unexpected signing key algorithm")
	}
	block, err := armor.Decode(bytes.NewReader(signature))
	if err != nil || block.Type != openpgp.SignatureType {
		return errors.New("Invalid armored manifest signature")
	}
	b, err := io.ReadAll(io.LimitReader(block.Body, 16<<10))
	if err != nil {
		return err
	}
	reader := bytes.NewReader(b)
	p, err := packet.Read(reader)
	if err != nil {
		return errors.New("Invalid manifest signature packet")
	}
	sig, ok := p.(*packet.Signature)
	if !ok || sig.SigType != packet.SigTypeBinary || sig.PubKeyAlgo != packet.PubKeyAlgoRSA || sig.Hash != crypto.SHA512 || sig.CreationTime.After(time.Now().Add(5*time.Minute)) {
		return errors.New("Unsupported manifest signature algorithm or time")
	}
	if _, err = packet.Read(reader); err != io.EOF {
		return errors.New("Unexpected extra signature packets")
	}
	if sig.SigLifetimeSecs != nil && sig.CreationTime.Add(time.Duration(*sig.SigLifetimeSecs)*time.Second).Before(time.Now()) {
		return errors.New("Expired manifest signature")
	}
	if _, err = openpgp.CheckDetachedSignature(keys, bytes.NewReader(raw), bytes.NewReader(b)); err != nil {
		return errors.New("Manifest signature verification failed")
	}
	return nil
}
