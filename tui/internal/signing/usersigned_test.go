package signing

import (
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

// TestUserSignedVectorUsdSend reproduces the reference Python SDK's
// test_sign_usd_transfer_action byte-exact. UsdSend shares the
// HyperliquidSignTransaction envelope with ApproveAgent, so matching this
// vector proves the whole user-signed pipeline (domain, chainId 0x66eee,
// digest, recoverable signature) against Hyperliquid's reference.
func TestUserSignedVectorUsdSend(t *testing.T) {
	s, err := NewSigner(testKey)
	if err != nil {
		t.Fatal(err)
	}
	td := userSignedTypedData(
		"HyperliquidTransaction:UsdSend",
		[]apitypes.Type{
			{Name: "hyperliquidChain", Type: "string"},
			{Name: "destination", Type: "string"},
			{Name: "amount", Type: "string"},
			{Name: "time", Type: "uint64"},
		},
		apitypes.TypedDataMessage{
			"hyperliquidChain": "Testnet",
			"destination":      "0x5e9ee1089755c3435139848e47e6635505d5a13a",
			"amount":           "1",
			"time":             math.NewHexOrDecimal256(1687816341423),
		},
	)
	sig, err := s.signTypedData(td)
	if err != nil {
		t.Fatal(err)
	}
	wantR := "0x637b37dd731507cdd24f46532ca8ba6eec616952c56218baeff04144e4a77073"
	wantS := "0x11a6a24900e6e314136d2592e2f8d502cd89b7c15b198e1bee043c9589f9fad7"
	if sig.R != wantR {
		t.Fatalf("r mismatch:\n got %s\nwant %s", sig.R, wantR)
	}
	if sig.S != wantS {
		t.Fatalf("s mismatch:\n got %s\nwant %s", sig.S, wantS)
	}
	if sig.V != 27 {
		t.Fatalf("v mismatch: got %d want 27", sig.V)
	}
}

func TestSignApproveAgentAndRecover(t *testing.T) {
	s, _ := NewSigner(testKey)
	agent := common.HexToAddress("0x5e9ee1089755c3435139848e47e6635505d5a13a")
	const nonce = uint64(1687816341423)

	sig, err := s.SignApproveAgent(agent, "hyperagent", nonce, false)
	if err != nil {
		t.Fatal(err)
	}

	// Rebuild the digest and recover — must match the master signer.
	td := userSignedTypedData(
		"HyperliquidTransaction:ApproveAgent",
		[]apitypes.Type{
			{Name: "hyperliquidChain", Type: "string"},
			{Name: "agentAddress", Type: "address"},
			{Name: "agentName", Type: "string"},
			{Name: "nonce", Type: "uint64"},
		},
		apitypes.TypedDataMessage{
			"hyperliquidChain": "Testnet",
			"agentAddress":     agent.Hex(),
			"agentName":        "hyperagent",
			"nonce":            math.NewHexOrDecimal256(int64(nonce)),
		},
	)
	domainSep, _ := td.HashStruct("EIP712Domain", td.Domain.Map())
	msgHash, _ := td.HashStruct(td.PrimaryType, td.Message)
	digest := crypto.Keccak256(append([]byte{0x19, 0x01}, append(domainSep, msgHash...)...))

	r, _ := hex.DecodeString(sig.R[2:])
	ss, _ := hex.DecodeString(sig.S[2:])
	sigBytes := make([]byte, 65)
	copy(sigBytes[:32], r)
	copy(sigBytes[32:64], ss)
	sigBytes[64] = sig.V - 27
	pub, err := crypto.SigToPub(digest, sigBytes)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if crypto.PubkeyToAddress(*pub) != s.Address() {
		t.Fatal("recovered address does not match master signer")
	}
}

func TestApproveAgentActionShape(t *testing.T) {
	agent := common.HexToAddress("0x5e9ee1089755c3435139848e47e6635505d5a13a")
	action := ApproveAgentAction(agent, "", 1700000000000, true)
	if action.values["hyperliquidChain"] != "Mainnet" {
		t.Fatal("mainnet flag must set hyperliquidChain=Mainnet")
	}
	if action.values["signatureChainId"] != SignatureChainID {
		t.Fatal("signatureChainId must be pinned to 0x66eee")
	}
	stripped := OmitEmptyAgentName(action, "")
	if _, ok := stripped.values["agentName"]; ok {
		t.Fatal("empty agentName must be omitted from posted action")
	}
	if stripped.Len() != action.Len()-1 {
		t.Fatal("only agentName should be removed")
	}
	kept := OmitEmptyAgentName(action, "named")
	if _, ok := kept.values["agentName"]; !ok {
		t.Fatal("non-empty agentName must be kept")
	}
}
