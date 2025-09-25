package main

import (
    "context"
    "encoding/json"
    "flag"
    "fmt"
    "bytes"
    "log"
    "math/big"
    "net/http"
    "os"
    "strings"

    "github.com/ethereum/go-ethereum/accounts/abi/bind"
    "github.com/ethereum/go-ethereum/accounts/abi"
    "github.com/ethereum/go-ethereum/common"
    "github.com/ethereum/go-ethereum/crypto"
    "github.com/ethereum/go-ethereum/ethclient"
    "github.com/ethereum/go-ethereum"
    erc "github.com/praxis/praxis-go-sdk/internal/erc8004"
)

func usage() {
    fmt.Println("erc8004-tool commands:")
    fmt.Println("  new-agent   --rpc <url> --identity <addr> --privkey <hex> --domain <domain>")
    fmt.Println("  update-agent --rpc <url> --identity <addr> --privkey <hex> --agent-id <id> [--domain <domain>] [--address <0x...>]")
    fmt.Println("  sign-registration --privkey <hex> --chain-id <id> --agent-id <id> --address <0x...> --domain <domain>")
    fmt.Println("  post-registration --agent-url <http://host:port> --chain-id <id> --agent-id <id> --address <0x...> --signature <0x...>")
    fmt.Println("  resolve-domain --rpc <url> --identity <addr> --domain <domain>")
    fmt.Println("  resolve-address --rpc <url> --identity <addr> --address <0x...>")
}

func main() {
    if len(os.Args) < 2 { usage(); return }
    cmd := os.Args[1]
    // no-op

    switch cmd {
    case "new-agent":
        fs := flag.NewFlagSet("new-agent", flag.ExitOnError)
        rpc := fs.String("rpc", os.Getenv("SEPOLIA_RPC"), "RPC URL")
        idAddr := fs.String("identity", "", "IdentityRegistry address")
        pkhex := fs.String("privkey", "", "EOA private key (hex)")
        domain := fs.String("domain", "", "agent domain")
        fs.Parse(os.Args[2:])
        if *rpc == "" || *idAddr == "" || *pkhex == "" || *domain == "" { log.Fatal("missing args") }
        client, err := dialEth(*rpc)
        if err != nil { log.Fatal(err) }
        priv, err := crypto.HexToECDSA(strings.TrimPrefix(*pkhex, "0x"))
        if err != nil { log.Fatal(err) }
        auth, err := bind.NewKeyedTransactorWithChainID(priv, big.NewInt(11155111))
        if err != nil { log.Fatal(err) }
        ident, err := erc.NewIdentity(common.HexToAddress(*idAddr), client)
        if err != nil { log.Fatal(err) }
        _, err = ident.NewAgent(auth, *domain, auth.From)
        if err != nil { log.Fatal(err) }
        fmt.Println("tx sent: newAgent; wait for receipt then resolve agentId via getAgentCount or events")
    case "update-agent":
        fs := flag.NewFlagSet("update-agent", flag.ExitOnError)
        rpc := fs.String("rpc", os.Getenv("SEPOLIA_RPC"), "RPC URL")
        idAddr := fs.String("identity", "", "IdentityRegistry address")
        pkhex := fs.String("privkey", "", "EOA private key (hex)")
        agentID := fs.Int64("agent-id", 0, "agent id")
        newDomain := fs.String("domain", "", "new domain (optional)")
        newAddress := fs.String("address", "", "new address (optional 0x..)")
        fs.Parse(os.Args[2:])
        if *rpc == "" || *idAddr == "" || *pkhex == "" || *agentID == 0 { log.Fatal("missing args") }
        client, err := dialEth(*rpc); if err != nil { log.Fatal(err) }
        priv, err := crypto.HexToECDSA(strings.TrimPrefix(*pkhex, "0x")); if err != nil { log.Fatal(err) }
        auth, err := bind.NewKeyedTransactorWithChainID(priv, big.NewInt(11155111)); if err != nil { log.Fatal(err) }
        ident, err := erc.NewIdentity(common.HexToAddress(*idAddr), client); if err != nil { log.Fatal(err) }
        dom := *newDomain
        addr := common.Address{}
        if *newAddress != "" { addr = common.HexToAddress(*newAddress) }
        _, err = ident.UpdateAgent(auth, big.NewInt(*agentID), dom, addr)
        if err != nil { log.Fatal(err) }
        fmt.Println("tx sent: updateAgent")
    case "sign-registration":
        fs := flag.NewFlagSet("sign-registration", flag.ExitOnError)
        pkhex := fs.String("privkey", "", "EOA private key (hex)")
        chainID := fs.Uint64("chain-id", 11155111, "chain id")
        agentID := fs.Uint64("agent-id", 0, "agent id")
        addr := fs.String("address", "", "agent EOA 0x...")
        domain := fs.String("domain", "", "agent domain")
        fs.Parse(os.Args[2:])
        if *pkhex == "" || *agentID == 0 || *addr == "" || *domain == "" { log.Fatal("missing args") }
        priv, err := crypto.HexToECDSA(strings.TrimPrefix(*pkhex, "0x")); if err != nil { log.Fatal(err) }
        sig, err := erc.SignRegistrationEIP191(priv, *chainID, *agentID, *addr, *domain)
        if err != nil { log.Fatal(err) }
        // output JSON snippet for card
        out := map[string]any{
            "agentId": *agentID,
            "agentAddress": fmt.Sprintf("eip155:%d:%s", *chainID, strings.ToLower(*addr)),
            "signature": sig,
        }
        b, _ := json.MarshalIndent(out, "", "  ")
        fmt.Println(string(b))
    case "post-registration":
        fs := flag.NewFlagSet("post-registration", flag.ExitOnError)
        agentURL := fs.String("agent-url", "http://localhost:8001", "agent base url")
        chainID := fs.Uint64("chain-id", 11155111, "chain id")
        agentID := fs.Uint64("agent-id", 0, "agent id")
        addr := fs.String("address", "", "agent EOA 0x...")
        sig := fs.String("signature", "", "0x signature")
        registry := fs.String("registry", "", "IdentityRegistry address (optional)")
        fs.Parse(os.Args[2:])
        if *agentID == 0 || *addr == "" || *sig == "" { log.Fatal("missing args") }
        body := map[string]any{"chainId":*chainID, "agentId":*agentID, "agentAddress":*addr, "signature":*sig}
        if *registry != "" { body["registryAddr"] = *registry }
        b, _ := json.Marshal(body)
        resp, err := http.Post(strings.TrimRight(*agentURL, "/")+"/admin/erc8004/register", "application/json", bytes.NewBuffer(b))
        if err != nil { log.Fatal(err) }
        defer resp.Body.Close()
        fmt.Println("status:", resp.Status)
    case "resolve-domain":
        fs := flag.NewFlagSet("resolve-domain", flag.ExitOnError)
        rpc := fs.String("rpc", os.Getenv("SEPOLIA_RPC"), "RPC URL")
        idAddr := fs.String("identity", "", "IdentityRegistry address")
        domain := fs.String("domain", "", "agent domain")
        fs.Parse(os.Args[2:])
        if *rpc == "" || *idAddr == "" || *domain == "" { log.Fatal("missing args") }
        ec, err := dialEth(*rpc); if err != nil { log.Fatal(err) }
        ident, err := erc.NewIdentity(common.HexToAddress(*idAddr), ec); if err != nil { log.Fatal(err) }
        ai, err := ident.ResolveByDomain(context.Background(), &bind.CallOpts{}, *domain)
        if err != nil {
            // Fallback: scan recent logs
            li, ferr := findByDomainLogs(context.Background(), ec, common.HexToAddress(*idAddr), *domain)
            if ferr != nil { log.Fatal(err) }
            ai.AgentId = li.AgentId; ai.AgentDomain = li.AgentDomain; ai.AgentAddress = li.AgentAddress
        }
        out := map[string]any{"agentId": ai.AgentId.String(), "domain": ai.AgentDomain, "address": ai.AgentAddress.Hex()}
        b, _ := json.MarshalIndent(out, "", "  "); fmt.Println(string(b))
    case "resolve-address":
        fs := flag.NewFlagSet("resolve-address", flag.ExitOnError)
        rpc := fs.String("rpc", os.Getenv("SEPOLIA_RPC"), "RPC URL")
        idAddr := fs.String("identity", "", "IdentityRegistry address")
        addr := fs.String("address", "", "0x...")
        fs.Parse(os.Args[2:])
        if *rpc == "" || *idAddr == "" || *addr == "" { log.Fatal("missing args") }
        client, err := dialEth(*rpc); if err != nil { log.Fatal(err) }
        ident, err := erc.NewIdentity(common.HexToAddress(*idAddr), client); if err != nil { log.Fatal(err) }
        ai, err := ident.ResolveByAddress(context.Background(), &bind.CallOpts{}, common.HexToAddress(*addr)); if err != nil { log.Fatal(err) }
        out := map[string]any{"agentId": ai.AgentId.String(), "domain": ai.AgentDomain, "address": ai.AgentAddress.Hex()}
        b, _ := json.MarshalIndent(out, "", "  "); fmt.Println(string(b))
    default:
        usage()
    }
}

func dialEth(rpc string) (*ethclient.Client, error) {
    client, err := ethclient.Dial(rpc)
    if err != nil { return nil, err }
    return client, nil
}

// findByDomainLogs scans Identity events and returns the last seen AgentInfo for a domain
func findByDomainLogs(ctx context.Context, ec *ethclient.Client, idAddr common.Address, domain string) (erc.AgentInfo, error) {
    // query all logs for this contract; for performance you can limit range
    q := ethereum.FilterQuery{Addresses: []common.Address{idAddr}}
    logs, err := ec.FilterLogs(ctx, q)
    if err != nil { return erc.AgentInfo{}, err }
    parsedABI, _ := abiFromIdentity()
    var last erc.AgentInfo
    for _, lg := range logs {
        if lg.Topics[0] == parsedABI.Events["AgentRegistered"].ID || lg.Topics[0] == parsedABI.Events["AgentUpdated"].ID {
            // indexed: agentId
            if len(lg.Topics) < 2 { continue }
            id := new(big.Int).SetBytes(lg.Topics[1].Bytes())
            // data: (agentDomain, agentAddress)
            var event struct{ AgentDomain string; AgentAddress common.Address }
            if err := parsedABI.UnpackIntoInterface(&event, parsedABI.Events["AgentRegistered"].Name, lg.Data); err != nil {
                // try AgentUpdated
                _ = parsedABI.UnpackIntoInterface(&event, parsedABI.Events["AgentUpdated"].Name, lg.Data)
            }
            if strings.EqualFold(event.AgentDomain, domain) {
                last.AgentId = id
                last.AgentDomain = event.AgentDomain
                last.AgentAddress = event.AgentAddress
            }
        }
    }
    if last.AgentId == nil { return erc.AgentInfo{}, fmt.Errorf("not found in logs") }
    return last, nil
}

func abiFromIdentity() (abi.ABI, error) {
    return abi.JSON(strings.NewReader(erc.IdentityABI()))
}
