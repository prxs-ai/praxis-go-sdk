package erc8004

import (
	"crypto/ecdsa"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// BuildRegistrationMessage constructs a human-readable message for EIP-191 signing.
// You can swap this to an EIP-712 typed message later if needed.
func BuildRegistrationMessage(chainID uint64, agentID uint64, agentAddress string, agentDomain string) string {
	return fmt.Sprintf("Praxis Agent Registration\nchainId=%d\nagentId=%d\naddress=%s\ndomain=%s", chainID, agentID, strings.ToLower(agentAddress), agentDomain)
}

// SignRegistrationEIP191 signs the registration message using EIP-191 (personal_sign semantics).
// Returns 0x-prefixed signature.
func SignRegistrationEIP191(privKey *ecdsa.PrivateKey, chainID uint64, agentID uint64, agentAddress string, agentDomain string) (string, error) {
	msg := BuildRegistrationMessage(chainID, agentID, agentAddress, agentDomain)
	hash := accounts.TextHash([]byte(msg))
	sig, err := crypto.Sign(hash, privKey)
	if err != nil {
		return "", err
	}
	// Adjust V to 27/28 form if needed; most consumers accept 0/1 in last byte already
	return hexutil.Encode(sig), nil
}
