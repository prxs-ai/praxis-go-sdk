package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct{ db *pgxpool.Pool }

func NewPostgres(url string) (*Postgres, error) {
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		return nil, err
	}
	return &Postgres{db: pool}, nil
}

type AgentRow struct {
	ChainID        string           `json:"chainId"`
	AgentID        int64            `json:"agentId"`
	RegistryAddr   string           `json:"registryAddr,omitempty"`
	Domain         string           `json:"domain"`
	AddressCAIP    string           `json:"addressCaip10"`
	CardJSON       map[string]any   `json:"card"`
	TrustModels    []string         `json:"trustModels"`
	Skills         []map[string]any `json:"skills"`
	Capabilities   map[string]any   `json:"capabilities"`
	ScoreAvg       *float64         `json:"scoreAvg"`
	ValidationsCnt int              `json:"validationsCnt"`
	FeedbacksCnt   int              `json:"feedbacksCnt"`
	LastSeenAt     time.Time        `json:"lastSeenAt"`
}

type SearchParams struct {
	Q          string
	Network    string
	Capability string
	Skill      string
	Tag        string
	TrustModel string
	Limit      int
	Cursor     string
}

func (s *Postgres) UpsertAgentFromCard(ctx context.Context, chainID string, registryAddr string, agentID int64, domain string, card map[string]any) error {
	// Extract fields for indices
	address := ""
	if regs, ok := card["registrations"].([]any); ok && len(regs) > 0 {
		if first, ok := regs[0].(map[string]any); ok {
			if v, ok := first["agentAddress"].(string); ok {
				address = v
			}
			if v2, ok2 := first["addressCaip10"].(string); ok2 && address == "" {
				address = v2
			} // backward compat
		}
	}
	trustModels := []string{}
	if arr, ok := card["trustModels"].([]any); ok {
		for _, x := range arr {
			if s, ok := x.(string); ok {
				trustModels = append(trustModels, strings.ToLower(s))
			}
		}
	}
	var skills []map[string]any
	if arr, ok := card["skills"].([]any); ok {
		for _, it := range arr {
			if m, ok := it.(map[string]any); ok {
				skills = append(skills, m)
			}
		}
	}
	caps := map[string]any{}
	if m, ok := card["capabilities"].(map[string]any); ok {
		caps = m
	}

	b, _ := json.Marshal(card)
	_, err := s.db.Exec(ctx, `
        INSERT INTO agents (chain_id, registry_addr, agent_id, domain, address_caip10, card_json, trust_models, skills, capabilities, last_seen_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9, now())
        ON CONFLICT (chain_id, agent_id)
        DO UPDATE SET registry_addr=EXCLUDED.registry_addr, domain=EXCLUDED.domain, address_caip10=EXCLUDED.address_caip10, card_json=EXCLUDED.card_json, trust_models=EXCLUDED.trust_models, skills=EXCLUDED.skills, capabilities=EXCLUDED.capabilities, last_seen_at=now()
    `, chainID, registryAddr, agentID, domain, address, b, trustModels, skills, caps)
	return err
}

func (s *Postgres) SearchAgents(ctx context.Context, p SearchParams) ([]AgentRow, string, error) {
	limit := 50
	if p.Limit > 0 && p.Limit <= 200 {
		limit = p.Limit
	}
	where := []string{}
	args := []any{}

	// q: search by domain or name or skill name
	if q := strings.TrimSpace(p.Q); q != "" {
		args = append(args, "%"+q+"%")
		idx := len(args)
		where = append(where, fmt.Sprintf("(domain ILIKE $%d OR card_json->>'name' ILIKE $%d OR EXISTS (SELECT 1 FROM jsonb_array_elements(skills) s WHERE s->>'name' ILIKE $%d))", idx, idx, idx))
	}
	// trustModel: case-insensitive
	if tm := strings.TrimSpace(p.TrustModel); tm != "" {
		args = append(args, strings.ToLower(tm))
		idx := len(args)
		where = append(where, fmt.Sprintf("$%d = ANY(trust_models)", idx))
	}
	// skill: by id or name
	if sk := strings.TrimSpace(p.Skill); sk != "" {
		args = append(args, "%"+sk+"%")
		idx := len(args)
		where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM jsonb_array_elements(skills) s WHERE s->>'id' ILIKE $%d OR s->>'name' ILIKE $%d)", idx, idx))
	}
	// tag: inside skills[].tags
	if tg := strings.TrimSpace(p.Tag); tg != "" {
		args = append(args, strings.ToLower(tg))
		idx := len(args)
		where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM jsonb_array_elements(skills) s, jsonb_array_elements_text(COALESCE(s->'tags','[]'::jsonb)) t WHERE lower(t)= $%d)", idx))
	}
	// capability: presence in card_json.capabilities keys or true value
	if cap := strings.TrimSpace(p.Capability); cap != "" {
		// presence of key OR key set to true
		args = append(args, cap)
		idx := len(args)
		where = append(where, fmt.Sprintf("((card_json->'capabilities' ? $%d) OR ((card_json->'capabilities'->>$%d)::boolean IS TRUE))", idx, idx))
	}
	// network: chain_id equals (optional)
	if net := strings.TrimSpace(p.Network); net != "" {
		args = append(args, net)
		idx := len(args)
		where = append(where, fmt.Sprintf("chain_id = $%d", idx))
	}

	sql := `
        SELECT chain_id, agent_id, registry_addr, domain, address_caip10, card_json, trust_models, skills, capabilities, score_avg, validations_cnt, feedbacks_cnt, last_seen_at
        FROM agents`
	if len(where) > 0 {
		sql += " WHERE " + strings.Join(where, " AND ")
	}
	sql += " ORDER BY last_seen_at DESC LIMIT $" + fmt.Sprint(len(args)+1)
	args = append(args, limit)

	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []AgentRow
	for rows.Next() {
		var r AgentRow
		var cardBytes []byte
		var skillsBytes []byte
		var capsBytes []byte
		err := rows.Scan(&r.ChainID, &r.AgentID, &r.RegistryAddr, &r.Domain, &r.AddressCAIP, &cardBytes, &r.TrustModels, &skillsBytes, &capsBytes, &r.ScoreAvg, &r.ValidationsCnt, &r.FeedbacksCnt, &r.LastSeenAt)
		if err != nil {
			return nil, "", err
		}
		_ = json.Unmarshal(cardBytes, &r.CardJSON)
		if len(skillsBytes) > 0 {
			_ = json.Unmarshal(skillsBytes, &r.Skills)
		}
		if len(capsBytes) > 0 {
			_ = json.Unmarshal(capsBytes, &r.Capabilities)
		}
		out = append(out, r)
	}
	return out, "", nil
}

func (s *Postgres) GetAgent(ctx context.Context, chainID, agentID string) (AgentRow, error) {
	row := s.db.QueryRow(ctx, `
        SELECT chain_id, agent_id, registry_addr, domain, address_caip10, card_json, trust_models, skills, capabilities, score_avg, validations_cnt, feedbacks_cnt, last_seen_at
        FROM agents WHERE chain_id=$1 AND agent_id=$2
    `, chainID, agentID)
	var r AgentRow
	var cardBytes []byte
	var skillsBytes []byte
	var capsBytes []byte
	err := row.Scan(&r.ChainID, &r.AgentID, &r.RegistryAddr, &r.Domain, &r.AddressCAIP, &cardBytes, &r.TrustModels, &skillsBytes, &capsBytes, &r.ScoreAvg, &r.ValidationsCnt, &r.FeedbacksCnt, &r.LastSeenAt)
	if err != nil {
		return AgentRow{}, err
	}
	_ = json.Unmarshal(cardBytes, &r.CardJSON)
	if len(skillsBytes) > 0 {
		_ = json.Unmarshal(skillsBytes, &r.Skills)
	}
	if len(capsBytes) > 0 {
		_ = json.Unmarshal(capsBytes, &r.Capabilities)
	}
	return r, nil
}

// ListZeroIDAgents returns domains that were indexed with placeholder agent_id=0 for a given chain.
func (s *Postgres) ListZeroIDAgents(ctx context.Context, chainID string, limit int) ([]string, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.Query(ctx, `SELECT domain FROM agents WHERE chain_id=$1 AND agent_id=0 LIMIT $2`, chainID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// DeleteAgent removes a specific (chain_id, agent_id) row.
func (s *Postgres) DeleteAgent(ctx context.Context, chainID string, agentID int64) error {
	_, err := s.db.Exec(ctx, `DELETE FROM agents WHERE chain_id=$1 AND agent_id=$2`, chainID, agentID)
	return err
}
