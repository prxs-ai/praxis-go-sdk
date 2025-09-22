package main

import (
    "log"
    "os"

    explorer "github.com/praxis/praxis-go-sdk/internal/explorer"
)

func main() {
    srv, err := explorer.NewServerFromEnv()
    if err != nil {
        log.Fatalf("explorer init error: %v", err)
    }
    go srv.RunIndexer()
    port := os.Getenv("EXPLORER_PORT")
    if port == "" { port = "8080" }
    if err := srv.RunHTTP(":" + port); err != nil {
        log.Fatalf("explorer http error: %v", err)
    }
}

