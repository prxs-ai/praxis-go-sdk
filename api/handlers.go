package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/praxis/praxis-go-sdk/internal/a2a"
	internalcrypto "github.com/praxis/praxis-go-sdk/internal/crypto"
	"github.com/praxis/praxis-go-sdk/internal/did"
	didweb "github.com/praxis/praxis-go-sdk/internal/did/web"
	didwebvh "github.com/praxis/praxis-go-sdk/internal/did/webvh"
	"github.com/praxis/praxis-go-sdk/internal/explorer/store"
)

func RegisterRoutes(r *gin.Engine, st *store.Postgres) {
	r.GET("/agents", func(c *gin.Context) {
		params := store.SearchParams{
			Q:          c.Query("q"),
			Network:    c.Query("network"),
			Capability: c.Query("capability"),
			Skill:      c.Query("skill"),
			Tag:        c.Query("tag"),
			TrustModel: c.Query("trustModel"),
			Cursor:     c.Query("cursor"),
		}
		limitStr := c.Query("limit")
		if limitStr != "" {
			if v, err := strconv.Atoi(limitStr); err == nil {
				params.Limit = v
			}
		}
		items, next, err := st.SearchAgents(c, params)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "nextCursor": next})
	})

	r.GET("/agents/:chainId/:agentId", func(c *gin.Context) {
		ai, err := st.GetAgent(c, c.Param("chainId"), c.Param("agentId"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusOK, ai)
	})

	// Admin: refresh an agent by fetching card and upserting with provided agentId
	r.POST("/admin/refresh", func(c *gin.Context) {
		var req struct {
			ChainID      string `json:"chainId"`
			Domain       string `json:"domain"`
			AgentID      int64  `json:"agentId"`
			RegistryAddr string `json:"registryAddr"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.ChainID == "" || req.Domain == "" || req.AgentID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "chainId, domain, agentId required"})
			return
		}
		url := req.Domain
		if !(len(url) > 7 && (url[:7] == "http://" || (len(url) > 8 && url[:8] == "https://"))) {
			url = fmt.Sprintf("http://%s/.well-known/agent-card.json", req.Domain)
		}
		resp, err := http.Get(url)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		defer resp.Body.Close()
		var card map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}

		cardBytes, err := json.Marshal(card)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		var agentCard a2a.AgentCard
		if err := json.Unmarshal(cardBytes, &agentCard); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("failed to parse agent card: %v", err)})
			return
		}

		didID := agentCard.DID
		kid := ""
		var signatureTime *time.Time
		sigValid := false

		if len(agentCard.Signatures) > 0 {
			sig := agentCard.Signatures[0]
			if protected, err := base64.RawURLEncoding.DecodeString(sig.Protected); err == nil {
				var header map[string]any
				if err := json.Unmarshal(protected, &header); err == nil {
					if v, ok := header["kid"].(string); ok {
						kid = v
					}
					if tsStr, ok := header["ts"].(string); ok {
						if t, err := time.Parse(time.RFC3339, tsStr); err == nil {
							t = t.UTC()
							signatureTime = &t
						}
					}
				}
			}
		}

		if didID != "" && len(agentCard.Signatures) > 0 {
			allowInsecure := strings.HasPrefix(strings.ToLower(url), "http://")
			webResolver := &didweb.Resolver{AllowInsecure: allowInsecure}
			webvhResolver := &didwebvh.Resolver{WebResolver: webResolver}
			resolver := did.NewMultiResolver(
				did.WithWebResolver(webResolver),
				did.WithWebVHResolver(webvhResolver),
			)

			ctxVerify, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
			if err := internalcrypto.VerifyAgentCard(ctxVerify, &agentCard, resolver); err != nil {
				fmt.Printf("[explorer] failed to verify agent card from %s: %v\n", req.Domain, err)
			} else {
				sigValid = true
			}
			cancel()
		}

		if err := st.UpsertAgentFromCard(c, req.ChainID, req.RegistryAddr, req.AgentID, req.Domain, card, didID, kid, signatureTime, sigValid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// best-effort: remove placeholder row with agent_id=0
		_ = st.DeleteAgent(c, req.ChainID, 0)
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
}
