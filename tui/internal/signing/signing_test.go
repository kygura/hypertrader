package signing

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

// A well-known test private key (NOT for real funds).
const testKey = "0x0123456789012345678901234567890123456789012345678901234567890123"

func TestSignerAddressDeterministic(t *testing.T) {
	s, err := NewSigner(testKey)
	if err != nil {
		t.Fatal(err)
	}
	s2, _ := NewSigner(testKey[2:]) // without 0x prefix
	if s.Address() != s2.Address() {
		t.Fatal("address should be identical with/without 0x prefix")
	}
}

func TestMsgpackDeterministicOrder(t *testing.T) {
	m := NewOrderedMap().Set("type", "order").Set("a", 5).Set("b", true)
	a, err := msgpackEncode(m)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := msgpackEncode(m)
	if !bytes.Equal(a, b) {
		t.Fatal("msgpack encoding not deterministic")
	}
	// Map header for 3 entries is 0x83.
	if a[0] != 0x83 {
		t.Fatalf("want map header 0x83, got 0x%02x", a[0])
	}
}

func TestActionHashStable(t *testing.T) {
	action := NewOrderedMap().Set("type", "order").Set("grouping", "na")
	h1, err := ActionHash(action, 1700000000000, nil)
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := ActionHash(action, 1700000000000, nil)
	if !bytes.Equal(h1, h2) {
		t.Fatal("action hash not stable")
	}
	if len(h1) != 32 {
		t.Fatalf("want 32-byte keccak hash, got %d", len(h1))
	}
	// Different nonce → different hash.
	h3, _ := ActionHash(action, 1700000000001, nil)
	if bytes.Equal(h1, h3) {
		t.Fatal("nonce should affect hash")
	}
}

func TestSignAndRecover(t *testing.T) {
	s, _ := NewSigner(testKey)
	action := NewOrderedMap().Set("type", "order")
	sig, err := s.SignL1Action(action, 1700000000000, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if sig.V != 27 && sig.V != 28 {
		t.Fatalf("V must be 27 or 28, got %d", sig.V)
	}
	// Recompute the digest the signer used and recover the pubkey to confirm the
	// signature is valid and addresses match.
	r, _ := hex.DecodeString(sig.R[2:])
	ss, _ := hex.DecodeString(sig.S[2:])
	if len(r) != 32 || len(ss) != 32 {
		t.Fatalf("r/s must be 32 bytes, got %d/%d", len(r), len(ss))
	}

	// Rebuild digest (mirror of SignL1Action).
	hash, _ := ActionHash(action, 1700000000000, nil)
	td := agentTypedData("a", hash)
	domainSep, _ := td.HashStruct("EIP712Domain", td.Domain.Map())
	msgHash, _ := td.HashStruct(td.PrimaryType, td.Message)
	raw := append([]byte{0x19, 0x01}, append(domainSep, msgHash...)...)
	digest := crypto.Keccak256(raw)

	sigBytes := make([]byte, 65)
	copy(sigBytes[:32], r)
	copy(sigBytes[32:64], ss)
	sigBytes[64] = sig.V - 27
	pub, err := crypto.SigToPub(digest, sigBytes)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if crypto.PubkeyToAddress(*pub) != s.Address() {
		t.Fatal("recovered address does not match signer")
	}
}
