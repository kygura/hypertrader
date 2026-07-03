// approve-agent: one-shot CLI that authorizes an agent (API) wallet to trade on
// the master account's behalf via Hyperliquid's approveAgent user-signed action.
// The master key is read from HL_MASTER_KEY or a hidden prompt, used for exactly
// one signature, and never written anywhere. The agent key it prints is what the
// daemon runs with (HL_AGENT_KEY) — it can sign trades but cannot withdraw.
package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/term"

	"github.com/hyperagent/hyperagent/internal/hlclient"
	"github.com/hyperagent/hyperagent/internal/signing"
)

func runApproveAgent(args []string) error {
	fs := flag.NewFlagSet("approve-agent", flag.ExitOnError)
	name := fs.String("name", "", "agent display name shown in the HL UI (optional)")
	testnet := fs.Bool("testnet", false, "approve on Hyperliquid testnet")
	agentKeyFlag := fs.String("agent-key", "", "existing agent private key to (re)approve; omit to generate a fresh one")
	if err := fs.Parse(args); err != nil {
		return err
	}

	masterHex, err := readMasterKey()
	if err != nil {
		return err
	}
	master, err := signing.NewSigner(masterHex)
	if err != nil {
		return fmt.Errorf("master key: %w", err)
	}

	// Agent wallet: reuse the supplied key or generate a fresh one.
	agentHex := strings.TrimSpace(*agentKeyFlag)
	if agentHex == "" {
		priv, err := crypto.GenerateKey()
		if err != nil {
			return fmt.Errorf("generate agent key: %w", err)
		}
		agentHex = "0x" + hex.EncodeToString(crypto.FromECDSA(priv))
	}
	agent, err := signing.NewSigner(agentHex)
	if err != nil {
		return fmt.Errorf("agent key: %w", err)
	}

	mainnet := !*testnet
	nonce := uint64(time.Now().UnixMilli())

	sig, err := master.SignApproveAgent(agent.Address(), *name, nonce, mainnet)
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}
	action := signing.OmitEmptyAgentName(
		signing.ApproveAgentAction(agent.Address(), *name, nonce, mainnet), *name)

	apiURL := hlclient.MainnetAPI
	if *testnet {
		apiURL = hlclient.TestnetAPI
	}
	if err := postExchange(apiURL, action, nonce, sig); err != nil {
		return err
	}

	net := "mainnet"
	if *testnet {
		net = "testnet"
	}
	fmt.Printf(`
agent wallet approved on %s
  master account : %s
  agent address  : %s

Add to .env (the daemon signs orders with this key; it CANNOT withdraw):

  HL_AGENT_KEY=%s

Store it now — it is not saved anywhere else. Approval expires per HL policy;
re-run this command with -agent-key to renew the same wallet.
`, net, master.Address().Hex(), agent.Address().Hex(), agentHex)
	return nil
}

// readMasterKey pulls the master private key from HL_MASTER_KEY or prompts with
// echo disabled. Never logged, never persisted.
func readMasterKey() (string, error) {
	if k := strings.TrimSpace(os.Getenv("HL_MASTER_KEY")); k != "" {
		return k, nil
	}
	fmt.Fprint(os.Stderr, "master account private key (input hidden): ")
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("read key: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("read key: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// postExchange sends a signed action envelope to /exchange and surfaces HL's
// error string on rejection (HL returns 200 with {"status":"err",...}).
func postExchange(apiURL string, action any, nonce uint64, sig signing.Signature) error {
	envelope := map[string]any{
		"action":    action,
		"nonce":     nonce,
		"signature": sig,
	}
	buf, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	resp, err := http.Post(apiURL+"/exchange", "application/json", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("exchange status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Status   string `json:"status"`
		Response any    `json:"response"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("exchange: unparseable response: %s", string(body))
	}
	if out.Status != "ok" {
		return fmt.Errorf("exchange rejected: %s", string(body))
	}
	return nil
}
