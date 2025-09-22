-- 002_registry_addr.sql — add registry address column and helpful indexes
ALTER TABLE agents
  ADD COLUMN IF NOT EXISTS registry_addr TEXT NOT NULL DEFAULT '';

-- Helpful indexes
CREATE INDEX IF NOT EXISTS idx_agents_registry_addr ON agents (registry_addr);

-- Enforce uniqueness for real on-chain pairs (registry_addr, agent_id)
-- Keep placeholders (agent_id = 0) outside of uniqueness to avoid conflicts during seeding.
CREATE UNIQUE INDEX IF NOT EXISTS uniq_agents_registry_agent
ON agents (registry_addr, agent_id)
WHERE agent_id <> 0;
