package main

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/praxis/praxis-go-sdk/internal/crypto"
	"github.com/praxis/praxis-go-sdk/internal/did"
	didweb "github.com/praxis/praxis-go-sdk/internal/did/web"
	didwebvh "github.com/praxis/praxis-go-sdk/internal/did/webvh"
)

func main() {
	didFlag := flag.String("did", "", "DID to resolve (did:web or did:webvh)")
	allowInsecure := flag.Bool("allow-insecure", false, "Allow HTTP for did:web (development)")
	timeout := flag.Duration("timeout", 10*time.Second, "Resolution timeout")
	flag.Parse()

	if *didFlag == "" {
		fmt.Fprintln(os.Stderr, "--did flag is required")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	webResolver := &didweb.Resolver{AllowInsecure: *allowInsecure}
	resolver := did.NewMultiResolver(
		did.WithWebResolver(webResolver),
		did.WithWebVHResolver(&didwebvh.Resolver{WebResolver: webResolver}),
	)

	doc, err := resolver.Resolve(ctx, *didFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve error: %v\n", err)
		os.Exit(2)
	}

	pretty, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal error: %v\n", err)
		os.Exit(3)
	}

	fmt.Println("Resolved DID document:")
	fmt.Println(string(pretty))

	method, _, err := did.BaseIdentifier(*didFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse identifier error: %v\n", err)
		os.Exit(4)
	}

	if method == "webvh" {
		if err := printWebVHHash(ctx, *didFlag, webResolver); err != nil {
			fmt.Fprintf(os.Stderr, "webvh hash check: %v\n", err)
			os.Exit(5)
		}
	}
}

func printWebVHHash(ctx context.Context, identifier string, webResolver *didweb.Resolver) error {
	_, specific, err := did.BaseIdentifier(identifier)
	if err != nil {
		return err
	}
	parts := strings.Split(specific, ":")
	if len(parts) < 2 {
		return fmt.Errorf("invalid did:webvh: %s", identifier)
	}
	baseSpecific := strings.Join(parts[:len(parts)-1], ":")
	baseDID := "did:web:" + baseSpecific

	doc, raw, err := webResolver.ResolveRaw(ctx, baseDID)
	if err != nil {
		return fmt.Errorf("resolve underlying did:web: %w", err)
	}

	canonical, err := crypto.CanonicalizeRawJSON(raw)
	if err != nil {
		return fmt.Errorf("canonicalize did:web document: %w", err)
	}

	digest := sha256.Sum256(canonical)
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:])

	fmt.Println("\nWebVH verification details:")
	fmt.Printf("  Base DID: %s\n", baseDID)
	fmt.Printf("  Canonical SHA-256 (base32, lower-case): %s\n", strings.ToLower(encoded))
	fmt.Printf("  Canonical document bytes: %d\n", len(canonical))
	fmt.Printf("  Verification methods: %d\n", len(doc.VerificationMethod))
	return nil
}
