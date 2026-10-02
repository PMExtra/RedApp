package claude

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	assets "github.com/PMExtra/RedApp/installers"
	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
	"golang.org/x/crypto/openpgp/packet"
	"strings"
	"testing"
)

func TestRejectUnsupportedSignatureHashAndExtraPackets(t *testing.T) {
	raw, sig := fixture(t)
	block, err := armor.Decode(bytes.NewReader(sig))
	if err != nil {
		t.Fatal(err)
	}
	p, err := packet.Read(block.Body)
	if err != nil {
		t.Fatal(err)
	}
	signature := p.(*packet.Signature)
	encode := func(extra bool) []byte {
		var b bytes.Buffer
		w, err := armor.Encode(&b, openpgp.SignatureType, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = signature.Serialize(w); err != nil {
			t.Fatal(err)
		}
		if extra {
			if err = signature.Serialize(w); err != nil {
				t.Fatal(err)
			}
		}
		if err = w.Close(); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	if Verify(raw, encode(true)) == nil {
		t.Fatal("extra packets accepted")
	}
	signature.Hash = crypto.SHA256
	signature.HashSuffix[3] = 8 // OpenPGP SHA256 identifier in the serialized v4 header.
	if err = Verify(raw, encode(false)); err == nil || !strings.Contains(err.Error(), "Unsupported manifest signature algorithm") {
		t.Fatal("hash algorithm gate missing", err)
	}
}

func TestRejectDifferentValidPublicKey(t *testing.T) {
	// Change the valid RSA key's creation timestamp, yielding a different fingerprint.
	keyBytes, err := assets.PublicAsset("anthropic/claude-code", "claude-code.asc")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(keyBytes))
	if err != nil {
		t.Fatal(err)
	}
	key := packet.NewRSAPublicKey(keys[0].PrimaryKey.CreationTime.AddDate(0, 0, 1), keys[0].PrimaryKey.PublicKey.(*rsa.PublicKey))
	var b bytes.Buffer
	w, err := armor.Encode(&b, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = key.Serialize(w); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	raw, sig := fixture(t)
	if err = verifyWithKey(raw, sig, b.Bytes()); err == nil {
		t.Fatal("different public key accepted")
	}
}
