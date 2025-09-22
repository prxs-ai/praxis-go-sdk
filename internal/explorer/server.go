package explorer

import (
    "context"
    "os"
    "time"

    "github.com/gin-contrib/cors"
    "github.com/gin-gonic/gin"
    api "github.com/praxis/praxis-go-sdk/internal/explorer/api"
    "github.com/praxis/praxis-go-sdk/internal/explorer/indexer"
    "github.com/praxis/praxis-go-sdk/internal/explorer/store"
)

type Server struct {
    store   *store.Postgres
    indexer *indexer.Indexer
    http    *gin.Engine
}

func NewServerFromEnv() (*Server, error) {
    psql, err := store.NewPostgres(os.Getenv("DATABASE_URL"))
    if err != nil { return nil, err }
    ix, err := indexer.New(psql, os.Getenv("ERC8004_CONFIG")) // path to configs/erc8004.yaml
    if err != nil { return nil, err }
    r := gin.Default()
    // CORS: allow browser UI at localhost:3000 (and any origin for simplicity in dev)
    r.Use(cors.New(cors.Config{
        AllowOrigins:     []string{"*"},
        AllowMethods:     []string{"GET", "POST", "OPTIONS"},
        AllowHeaders:     []string{"Origin", "Content-Type", "Accept"},
        ExposeHeaders:    []string{"Content-Length"},
        AllowCredentials: false,
        MaxAge:           12 * time.Hour,
    }))
    s := &Server{store: psql, indexer: ix, http: r}
    api.RegisterRoutes(r, s.store)
    return s, nil
}

func (s *Server) RunIndexer() { go s.indexer.Start(context.Background()) }
func (s *Server) RunHTTP(addr string) error { return s.http.Run(addr) }
