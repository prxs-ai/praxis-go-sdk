package erc8004

import (
	"context"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
)

type AgentInfo struct {
	AgentId      *big.Int
	AgentDomain  string
	AgentAddress common.Address
}

// internal tuple type used for ABI decoding of AgentInfo
type agentInfoTuple struct {
	AgentId      *big.Int
	AgentDomain  string
	AgentAddress common.Address
}

// ABI for IdentityRegistry with events (from reference implementation)
const identityABI = `[
  {"anonymous":false,"inputs":[{"indexed":true,"internalType":"uint256","name":"agentId","type":"uint256"},{"indexed":false,"internalType":"string","name":"agentDomain","type":"string"},{"indexed":false,"internalType":"address","name":"agentAddress","type":"address"}],"name":"AgentRegistered","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"internalType":"uint256","name":"agentId","type":"uint256"},{"indexed":false,"internalType":"string","name":"agentDomain","type":"string"},{"indexed":false,"internalType":"address","name":"agentAddress","type":"address"}],"name":"AgentUpdated","type":"event"},
  {"inputs":[{"internalType":"string","name":"agentDomain","type":"string"},{"internalType":"address","name":"agentAddress","type":"address"}],"name":"newAgent","outputs":[{"internalType":"uint256","name":"agentId","type":"uint256"}],"stateMutability":"nonpayable","type":"function"},
  {"inputs":[{"internalType":"uint256","name":"agentId","type":"uint256"},{"internalType":"string","name":"newAgentDomain","type":"string"},{"internalType":"address","name":"newAgentAddress","type":"address"}],"name":"updateAgent","outputs":[{"internalType":"bool","name":"success","type":"bool"}],"stateMutability":"nonpayable","type":"function"},
  {"inputs":[{"internalType":"uint256","name":"agentId","type":"uint256"}],"name":"getAgent","outputs":[{"components":[{"internalType":"uint256","name":"agentId","type":"uint256"},{"internalType":"string","name":"agentDomain","type":"string"},{"internalType":"address","name":"agentAddress","type":"address"}],"internalType":"struct IIdentityRegistry.AgentInfo","name":"agentInfo","type":"tuple"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"internalType":"string","name":"agentDomain","type":"string"}],"name":"resolveByDomain","outputs":[{"components":[{"internalType":"uint256","name":"agentId","type":"uint256"},{"internalType":"string","name":"agentDomain","type":"string"},{"internalType":"address","name":"agentAddress","type":"address"}],"internalType":"struct IIdentityRegistry.AgentInfo","name":"agentInfo","type":"tuple"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"internalType":"address","name":"agentAddress","type":"address"}],"name":"resolveByAddress","outputs":[{"components":[{"internalType":"uint256","name":"agentId","type":"uint256"},{"internalType":"string","name":"agentDomain","type":"string"},{"internalType":"address","name":"agentAddress","type":"address"}],"internalType":"struct IIdentityRegistry.AgentInfo","name":"agentInfo","type":"tuple"}],"stateMutability":"view","type":"function"},
  {"inputs":[],"name":"getAgentCount","outputs":[{"internalType":"uint256","name":"count","type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"internalType":"uint256","name":"agentId","type":"uint256"}],"name":"agentExists","outputs":[{"internalType":"bool","name":"exists","type":"bool"}],"stateMutability":"view","type":"function"}
]`

type Identity struct {
	addr     common.Address
	backend  bind.ContractBackend
	contract *bind.BoundContract
	abi      abi.ABI
}

func NewIdentity(addr common.Address, backend bind.ContractBackend) (*Identity, error) {
	parsed, err := abi.JSON(strings.NewReader(identityABI))
	if err != nil {
		return nil, err
	}
	c := bind.NewBoundContract(addr, parsed, backend, backend, backend)
	return &Identity{addr: addr, backend: backend, contract: c, abi: parsed}, nil
}

// IdentityABI returns the ABI JSON used by this package (helper for indexer)
func IdentityABI() string { return identityABI }

func (i *Identity) NewAgent(auth *bind.TransactOpts, domain string, addr common.Address) (*big.Int, error) {
	_, err := i.contract.Transact(auth, "newAgent", domain, addr)
	if err != nil {
		return nil, err
	}
	// agentId is emitted in receipt; for brevity return nil here; use receipt decode in production
	return nil, nil
}

func (i *Identity) UpdateAgent(auth *bind.TransactOpts, id *big.Int, domain string, addr common.Address) (bool, error) {
	_, err := i.contract.Transact(auth, "updateAgent", id, domain, addr)
	return err == nil, err
}

func (i *Identity) ResolveByDomain(ctx context.Context, call *bind.CallOpts, domain string) (AgentInfo, error) {
	if call == nil {
		call = &bind.CallOpts{}
	}
	var res agentInfoTuple
	out := []interface{}{&res}
	if err := i.contract.Call(call, &out, "resolveByDomain", domain); err != nil {
		return AgentInfo{}, err
	}
	return AgentInfo{AgentId: res.AgentId, AgentDomain: res.AgentDomain, AgentAddress: res.AgentAddress}, nil
}

func (i *Identity) ResolveByAddress(ctx context.Context, call *bind.CallOpts, addr common.Address) (AgentInfo, error) {
	if call == nil {
		call = &bind.CallOpts{}
	}
	var res agentInfoTuple
	out := []interface{}{&res}
	if err := i.contract.Call(call, &out, "resolveByAddress", addr); err != nil {
		return AgentInfo{}, err
	}
	return AgentInfo{AgentId: res.AgentId, AgentDomain: res.AgentDomain, AgentAddress: res.AgentAddress}, nil
}

func (i *Identity) GetAgent(ctx context.Context, call *bind.CallOpts, id *big.Int) (AgentInfo, error) {
	if call == nil {
		call = &bind.CallOpts{}
	}
	var res agentInfoTuple
	out := []interface{}{&res}
	if err := i.contract.Call(call, &out, "getAgent", id); err != nil {
		return AgentInfo{}, err
	}
	return AgentInfo{AgentId: res.AgentId, AgentDomain: res.AgentDomain, AgentAddress: res.AgentAddress}, nil
}

func (i *Identity) GetAgentCount(ctx context.Context, call *bind.CallOpts) (*big.Int, error) {
	if call == nil {
		call = &bind.CallOpts{}
	}
	var cnt *big.Int
	out := []interface{}{&cnt}
	if err := i.contract.Call(call, &out, "getAgentCount"); err != nil {
		return nil, err
	}
	return cnt, nil
}
