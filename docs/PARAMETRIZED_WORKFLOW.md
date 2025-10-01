# Parameterized Workflows

This section explains how parameters and secrets flow through Praxis—from the UI to the orchestrator, into the DSL analyzer, and finally down to tool execution (e.g., Dagger / containers). It also covers logging, security, and debugging.

## TL;DR

- Params = non-sensitive user inputs (e.g., user, message, channel).

- Secrets = sensitive credentials (e.g., api_key, tokens).

- Planning (LLM/DSL plan building) happens on CHAT_MESSAGE without params/secrets.

- Execution happens on EXECUTE_WORKFLOW with params/secrets.

- Secrets never go to the LLM and are masked in logs.

- At runtime, params & secrets are injected into the tool call and mapped to environment variables for Dagger-based tools.


## Lifecycle: From Chat to Execution

1) `CHAT_MESSAGE` → Plan (No Params/Secrets)

- The frontend sends the user’s natural language as a `CHAT_MESSAGE`.

- The backend uses the LLM planner / DSL analyzer to build a workflow plan (nodes/edges).

- No params/secrets are needed or used at this stage (keeps secrets out of the LLM).

#### Example WebSocket payload:

```json
{
  "type": "CHAT_MESSAGE",
  "payload": {
    "sender": "user",
    "content": "please post my web3 thoughts to telegram"
  }
}
```

2) `EXECUTE_WORKFLOW` → Run (Params/Secrets Included)

- After the plan is built (or user confirms), the frontend sends `EXECUTE_WORKFLOW` with params and secrets.

- Backend injects them into the execution context and runs the workflow.

- Params/secrets are attached to each node/tool call as appropriate.

#### Example WebSocket payload:

```json
{
  "type": "EXECUTE_WORKFLOW",
  "payload": {
    "workflowId": "workflow_1759173798717541170",
    "workflow": { "nodes": [...], "edges": [...], "complexity": "simple" },
    "params": { "user": "Alice", "message": "Hello!" },
    "secrets": { "api_key": "SECRET123" },
    "metadata": {
      "name": "Interactive Workflow",
      "description": "Workflow created and executed from UI",
      "version": "1.0.0",
      "createdAt": "2025-09-29T19:23:15.243Z"
    }
  }
}
```

### Tool Execution: Dagger Engine

Dagger maps combined payload (args + params + secrets) to container environment variables so your scripts can read them.

```json
{
  "image": "python:3.11-slim",
  "command": ["python", "/app/telegram_poster.py"],
  "mounts": { "./tools/telegram": "/app" },
  "env": { "LANG": "C.UTF-8" },
  "env_passthrough": ["HTTPS_PROXY"]
}
```

#### Engine behavior:

- Applies fixed env values.

- Applies each args/params/secrets key/value via WithEnvVariable(k, v).

- Logs non-sensitive metadata


## FAQ

Q: Should we send params/secrets with CHAT_MESSAGE?
A: No. Only with EXECUTE_WORKFLOW. Keep planning clean.

Q: How do tools receive parameters?
A: As environment variables inside the container (e.g., $user, $message, $api_key).

Q: How are secrets protected?
A: Not sent to LLM, masked in logs, and sanitized in results.
