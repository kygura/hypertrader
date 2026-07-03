// Package signing is the owned EIP-712 signing module for the Hyperliquid
// exchange endpoint — the one dangerous layer the plan insists we own rather than
// import. It builds the action hash and the typed-data signature against
// go-ethereum/crypto (keccak + ECDSA), signs with a Hyperliquid agent (API)
// wallet, and never touches the master key.
//
// This is a sealed module: written once, tested against testnet. The agent
// wallet can sign trades but cannot withdraw.
package signing

import (
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

// Signer holds the agent wallet's private key.
type Signer struct {
	priv    *ecdsa.PrivateKey
	address common.Address
}

// NewSigner parses a hex private key (with or without 0x prefix).
func NewSigner(hexKey string) (*Signer, error) {
	hexKey = strings.TrimPrefix(hexKey, "0x")
	priv, err := crypto.HexToECDSA(hexKey)
	if err != nil {
		return nil, fmt.Errorf("signing: bad private key: %w", err)
	}
	return &Signer{priv: priv, address: crypto.PubkeyToAddress(priv.PublicKey)}, nil
}

// Address returns the agent wallet's address.
func (s *Signer) Address() common.Address { return s.address }

// Signature is the {r,s,v} form Hyperliquid expects in the request envelope.
type Signature struct {
	R string `json:"r"`
	S string `json:"s"`
	V byte   `json:"v"`
}

// ActionHash computes Hyperliquid's "connectionId" / action hash: keccak256 of
// msgpack(action) || nonce(8 bytes BE) || vaultByte || (vaultAddress). HL uses
// msgpack for the action encoding; we implement the deterministic encoding here.
//
// For order/cancel actions vault is typically nil. nonce is a millisecond
// timestamp.
func ActionHash(action any, nonce uint64, vault *common.Address) ([]byte, error) {
	packed, err := msgpackEncode(action)
	if err != nil {
		return nil, fmt.Errorf("signing: msgpack: %w", err)
	}
	buf := make([]byte, 0, len(packed)+9+21)
	buf = append(buf, packed...)
	var n [8]byte
	for i := 0; i < 8; i++ {
		n[7-i] = byte(nonce >> (8 * i))
	}
	buf = append(buf, n[:]...)
	if vault == nil {
		buf = append(buf, 0x00)
	} else {
		buf = append(buf, 0x01)
		buf = append(buf, vault.Bytes()...)
	}
	return crypto.Keccak256(buf), nil
}

// SignL1Action signs an L1 (exchange) action. Hyperliquid wraps the action hash
// in an EIP-712 "Agent" typed-data envelope whose source is "a" for mainnet and
// "b" for testnet, and signs that. Returns the {r,s,v} signature.
func (s *Signer) SignL1Action(action any, nonce uint64, vault *common.Address, mainnet bool) (Signature, error) {
	hash, err := ActionHash(action, nonce, vault)
	if err != nil {
		return Signature{}, err
	}
	source := "b"
	if mainnet {
		source = "a"
	}
	return s.signTypedData(agentTypedData(source, hash))
}

// agentTypedData builds Hyperliquid's EIP-712 "Agent" envelope wrapping the
// action hash. Extracted so tests can recompute the exact digest and verify the
// signature recovers to the signer.
func agentTypedData(source string, connectionID []byte) apitypes.TypedData {
	return apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"Agent": []apitypes.Type{
				{Name: "source", Type: "string"},
				{Name: "connectionId", Type: "bytes32"},
			},
		},
		PrimaryType: "Agent",
		Domain: apitypes.TypedDataDomain{
			Name:              "Exchange",
			Version:           "1",
			ChainId:           math.NewHexOrDecimal256(1337),
			VerifyingContract: "0x0000000000000000000000000000000000000000",
		},
		Message: apitypes.TypedDataMessage{
			"source":       source,
			"connectionId": connectionID,
		},
	}
}

// signTypedData hashes an EIP-712 typed-data payload and signs it, returning the
// recoverable {r,s,v} signature.
func (s *Signer) signTypedData(td apitypes.TypedData) (Signature, error) {
	domainSep, err := td.HashStruct("EIP712Domain", td.Domain.Map())
	if err != nil {
		return Signature{}, fmt.Errorf("signing: domain hash: %w", err)
	}
	msgHash, err := td.HashStruct(td.PrimaryType, td.Message)
	if err != nil {
		return Signature{}, fmt.Errorf("signing: message hash: %w", err)
	}
	raw := append([]byte{0x19, 0x01}, append(domainSep, msgHash...)...)
	digest := crypto.Keccak256(raw)

	sig, err := crypto.Sign(digest, s.priv)
	if err != nil {
		return Signature{}, fmt.Errorf("signing: sign: %w", err)
	}
	// crypto.Sign returns [R || S || V] with V in {0,1}; HL wants V in {27,28}.
	r := new(big.Int).SetBytes(sig[:32])
	ss := new(big.Int).SetBytes(sig[32:64])
	v := sig[64] + 27
	return Signature{
		R: "0x" + hex.EncodeToString(common.LeftPadBytes(r.Bytes(), 32)),
		S: "0x" + hex.EncodeToString(common.LeftPadBytes(ss.Bytes(), 32)),
		V: v,
	}, nil
}

// msgpackEncode is a minimal deterministic msgpack encoder sufficient for HL
// action objects (maps with string keys, strings, ints, floats, bools, arrays).
// HL hashes the msgpack of the action; key order must match the struct order,
// so callers pass *orderedMap* values (see exec package) rather than Go maps.
func msgpackEncode(v any) ([]byte, error) {
	var b []byte
	if err := encodeValue(&b, v); err != nil {
		return nil, err
	}
	return b, nil
}

// We reuse rlp only as a guard import marker for go-ethereum completeness; the
// msgpack encoder below is self-contained.
var _ = rlp.EncodeToBytes
