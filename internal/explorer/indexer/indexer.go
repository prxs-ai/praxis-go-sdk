package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	erc "github.com/praxis/praxis-go-sdk/internal/erc8004"
	"github.com/praxis/praxis-go-sdk/internal/explorer/store"
)

type Chain struct {
	Name       string
	RPC        string
	Identity   string
	Reputation string
	Validation string
}

type Indexer struct {
	store *store.Postgres
	nets  []Chain
	seeds []string // optional list of seed domains to crawl if logs are unavailable
	// runtime
	clients map[string]*ethclient.Client
	idents  map[string]common.Address
	idABI   abi.ABI
}

func New(st *store.Postgres, cfgPath string) (*Indexer, error) {
	// Load networks from YAML config if provided
	nets, _ := loadConfig(cfgPath)
	seeds := readSeedsFromEnv()
	parsed, _ := abi.JSON(strings.NewReader(erc.IdentityABI()))
	return &Indexer{store: st, nets: nets, seeds: seeds, clients: map[string]*ethclient.Client{}, idents: map[string]common.Address{}, idABI: parsed}, nil
}

func (ix *Indexer) Start(ctx context.Context) {
	// MVP: crawl seeds periodically; on production parse cfgPath and subscribe to logs
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	ix.crawlSeeds(ctx)
	ix.upgradeZeroIDs(ctx)
	ix.startOnchainWatchers(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			ix.crawlSeeds(ctx)
			ix.upgradeZeroIDs(ctx)
		}
	}
}

func (ix *Indexer) crawlSeeds(ctx context.Context) {
	for _, domain := range ix.seeds {
		url := fmt.Sprintf("http://%s/.well-known/agent-card.json", domain)
		resp, err := http.Get(url)
		if err != nil {
			continue
		}
		dec := json.NewDecoder(resp.Body)
		var card map[string]any
		if err := dec.Decode(&card); err == nil {
			_ = ix.store.UpsertAgentFromCard(ctx, "sepolia", "", 0, domain, card) // registry unknown in seed mode
		}
		resp.Body.Close()
	}
}

func readSeedsFromEnv() []string {
	v := os.Getenv("EXPLORER_SEEDS")
	if v == "" {
		return []string{}
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// --- On-chain watchers ---
func (ix *Indexer) startOnchainWatchers(ctx context.Context) {
	for _, n := range ix.nets {
		if n.RPC == "" || n.Identity == "" {
			continue
		}
		client, err := ethclient.Dial(os.ExpandEnv(n.RPC))
		if err != nil {
			continue
		}
		ix.clients[n.Name] = client
		ix.idents[n.Name] = common.HexToAddress(n.Identity)
		go ix.watchIdentity(ctx, n.Name, client, ix.idents[n.Name])
		go ix.backfillAgents(ctx, n.Name, client, ix.idents[n.Name])
	}
}

func (ix *Indexer) backfillAgents(ctx context.Context, chain string, client *ethclient.Client, idAddr common.Address) {
	ident, err := erc.NewIdentity(idAddr, client)
	if err != nil {
		return
	}
	count, err := ident.GetAgentCount(ctx, &bind.CallOpts{Context: ctx})
	if err != nil {
		return
	}
	total := count.Int64()
	for i := int64(1); i <= total; i++ {
		ai, err := ident.GetAgent(ctx, &bind.CallOpts{Context: ctx}, big.NewInt(i))
		if err != nil {
			continue
		}
		domain := ai.AgentDomain
		ix.fetchAndStoreCard(ctx, chain, idAddr.Hex(), ai.AgentId.Int64(), domain)
	}
}

func (ix *Indexer) watchIdentity(ctx context.Context, chain string, client *ethclient.Client, idAddr common.Address) {
	q := ethereum.FilterQuery{Addresses: []common.Address{idAddr}}
	logs := make(chan types.Log)
	sub, err := client.SubscribeFilterLogs(ctx, q, logs)
	if err != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-sub.Err():
			_ = err
			time.Sleep(3 * time.Second)
			go ix.watchIdentity(ctx, chain, client, idAddr)
			return
		case lg := <-logs:
			ix.handleIdentityLog(ctx, chain, lg)
		}
	}
}

func (ix *Indexer) handleIdentityLog(ctx context.Context, chain string, lg types.Log) {
	// Try AgentRegistered
	evReg := ix.idABI.Events["AgentRegistered"]
	evUpd := ix.idABI.Events["AgentUpdated"]
	switch lg.Topics[0] {
	case evReg.ID:
		// agentId is indexed
		if len(lg.Topics) < 2 {
			return
		}
		id := new(big.Int).SetBytes(lg.Topics[1].Bytes())
		// non-indexed data: agentDomain (string), agentAddress (address)
		var data struct {
			AgentDomain  string
			AgentAddress common.Address
		}
		if err := ix.idABI.UnpackIntoInterface(&data, "AgentRegistered", lg.Data); err != nil {
			return
		}
		reg := ix.idents[chain].Hex()
		ix.fetchAndStoreCard(ctx, chain, reg, id.Int64(), data.AgentDomain)
	case evUpd.ID:
		if len(lg.Topics) < 2 {
			return
		}
		id := new(big.Int).SetBytes(lg.Topics[1].Bytes())
		var data struct {
			AgentDomain  string
			AgentAddress common.Address
		}
		if err := ix.idABI.UnpackIntoInterface(&data, "AgentUpdated", lg.Data); err != nil {
			return
		}
		reg := ix.idents[chain].Hex()
		ix.fetchAndStoreCard(ctx, chain, reg, id.Int64(), data.AgentDomain)
	}
}

func (ix *Indexer) fetchAndStoreCard(ctx context.Context, chain string, registryAddr string, agentID int64, domain string) {
	d := strings.TrimSpace(domain)
	if d == "" {
		return
	}
	// basic heuristic: add http:// if missing scheme
	url := d
	if !strings.HasPrefix(d, "http://") && !strings.HasPrefix(d, "https://") {
		url = fmt.Sprintf("http://%s/.well-known/agent-card.json", d)
	} else if !strings.Contains(d, "/.well-known/agent-card.json") {
		url = strings.TrimRight(d, "/") + "/.well-known/agent-card.json"
	}
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var card map[string]any
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&card); err != nil {
		return
	}
	_ = ix.store.UpsertAgentFromCard(ctx, chain, registryAddr, agentID, d, card)
}

// upgradeZeroIDs resolves agentId on-chain for domains saved with placeholder agent_id=0
func (ix *Indexer) upgradeZeroIDs(ctx context.Context) {
	for _, n := range ix.nets {
		client := ix.clients[n.Name]
		if client == nil {
			// try dial if not present
			c, err := ethclient.Dial(os.ExpandEnv(n.RPC))
			if err != nil {
				continue
			}
			ix.clients[n.Name] = c
			client = c
		}
		idAddr, ok := ix.idents[n.Name]
		if !ok || (idAddr == common.Address{}) {
			ix.idents[n.Name] = common.HexToAddress(n.Identity)
			idAddr = ix.idents[n.Name]
		}
		domains, err := ix.store.ListZeroIDAgents(ctx, n.Name, 200)
		if err != nil {
			continue
		}
		if len(domains) == 0 {
			continue
		}
		ident, err := erc.NewIdentity(idAddr, client)
		if err != nil {
			continue
		}
		for _, d := range domains {
			// resolve by domain
			ai, err := ident.ResolveByDomain(ctx, &bind.CallOpts{Context: ctx}, d)
			if err != nil || ai.AgentId == nil || ai.AgentId.Int64() == 0 {
				continue
			}
			ix.fetchAndStoreCard(ctx, n.Name, idAddr.Hex(), ai.AgentId.Int64(), d)
			// optional: delete placeholder row
			_ = ix.store.DeleteAgent(ctx, n.Name, 0)
		}
	}
}
