// User-signed actions — the second Hyperliquid signing scheme. Unlike L1
// actions (msgpack action-hash wrapped in the phantom Agent envelope), these
// sign the action fields directly under the "HyperliquidSignTransaction"
// EIP-712 domain, and must be signed by the MASTER account key. approveAgent is
// the one we need: it authorizes an agent (API) wallet to trade on the master
// account's behalf. The master key is used exactly once, in the approval CLI,
// and never persists in the daemon.
package signing

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

// SignatureChainID is the EVM chain id Hyperliquid expects in user-signed
// actions. The reference SDK pins 0x66eee (Arbitrum Sepolia) for mainnet and
// testnet alike; the hyperliquidChain string is what selects the network.
const SignatureChainID = "0x66eee"

const signatureChainIDInt = 0x66eee

// HyperliquidChain returns the chain discriminator for user-signed actions.
func HyperliquidChain(mainnet bool) string {
	if mainnet {
		return "Mainnet"
	}
	return "Testnet"
}

// ApproveAgentAction builds the approveAgent action object in the exact field
// shape the exchange endpoint expects. Matches the reference SDK: when
// agentName is empty the field is signed as "" but omitted from the posted
// action (see OmitEmptyAgentName).
func ApproveAgentAction(agent common.Address, agentName string, nonce uint64, mainnet bool) *OrderedMap {
	m := NewOrderedMap().
		Set("type", "approveAgent").
		Set("hyperliquidChain", HyperliquidChain(mainnet)).
		Set("signatureChainId", SignatureChainID).
		Set("agentAddress", agent.Hex()).
		Set("agentName", agentName).
		Set("nonce", nonce)
	return m
}

// OmitEmptyAgentName strips the agentName key from a posted approveAgent
// action, mirroring the reference SDK's `if name is None: del action["agentName"]`.
// Call AFTER signing (the signature covers agentName="").
func OmitEmptyAgentName(action *OrderedMap, agentName string) *OrderedMap {
	if agentName != "" {
		return action
	}
	out := NewOrderedMap()
	for _, k := range action.keys {
		if k == "agentName" {
			continue
		}
		out.Set(k, action.values[k])
	}
	return out
}

// SignApproveAgent signs an approveAgent user-signed action with s (the MASTER
// account key). nonce must equal the envelope nonce (millisecond timestamp).
func (s *Signer) SignApproveAgent(agent common.Address, agentName string, nonce uint64, mainnet bool) (Signature, error) {
	td := userSignedTypedData(
		"HyperliquidTransaction:ApproveAgent",
		[]apitypes.Type{
			{Name: "hyperliquidChain", Type: "string"},
			{Name: "agentAddress", Type: "address"},
			{Name: "agentName", Type: "string"},
			{Name: "nonce", Type: "uint64"},
		},
		apitypes.TypedDataMessage{
			"hyperliquidChain": HyperliquidChain(mainnet),
			"agentAddress":     agent.Hex(),
			"agentName":        agentName,
			"nonce":            math.NewHexOrDecimal256(int64(nonce)),
		},
	)
	return s.signTypedData(td)
}

// userSignedTypedData builds the HyperliquidSignTransaction EIP-712 envelope
// shared by every user-signed action type.
func userSignedTypedData(primaryType string, fields []apitypes.Type, msg apitypes.TypedDataMessage) apitypes.TypedData {
	return apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			primaryType: fields,
		},
		PrimaryType: primaryType,
		Domain: apitypes.TypedDataDomain{
			Name:              "HyperliquidSignTransaction",
			Version:           "1",
			ChainId:           math.NewHexOrDecimal256(signatureChainIDInt),
			VerifyingContract: "0x0000000000000000000000000000000000000000",
		},
		Message: msg,
	}
}
