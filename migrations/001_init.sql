-- 001_init.sql — schema for praxis-explorer
CREATE TABLE IF NOT EXISTS agents (
  chain_id        TEXT NOT NULL,
  agent_id        BIGINT NOT NULL,
  domain          TEXT NOT NULL,
  address_caip10  TEXT NOT NULL,
  card_json       JSONB NOT NULL,
  trust_models    TEXT[] DEFAULT '{}',
  skills          JSONB,
  capabilities    JSONB,
  score_avg       NUMERIC,
  validations_cnt INTEGER DEFAULT 0,
  feedbacks_cnt   INTEGER DEFAULT 0,
  last_seen_at    TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (chain_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_agents_domain ON agents (domain);
CREATE INDEX IF NOT EXISTS idx_agents_address ON agents (address_caip10);
CREATE INDEX IF NOT EXISTS idx_agents_trust_models ON agents USING GIN (trust_models);
CREATE INDEX IF NOT EXISTS idx_agents_skills ON agents USING GIN (skills);
CREATE INDEX IF NOT EXISTS idx_agents_capabilities ON agents USING GIN (capabilities);

