# Building Dagger Tools for the Praxis Agent

This guide explains how to add a tool that runs inside a [Dagger](https://dagger.io/) container to the Praxis Go agent. Follow the steps below to create the script, update the agent configuration, and run the tool with the Dagger execution engine.

## Prerequisites
- Docker 20.10+ is running on the host. The Dagger engine spins up Docker containers behind the scenes.
- The [Dagger CLI](https://docs.dagger.io/install) is installed and accessible on the `PATH`. The Praxis agent calls `dagger.Connect`, which automatically provisions the engine through the CLI.
- You have access to the repository root at `praxis-go-sdk/`.

## Repository paths used by Dagger
- The agent config (`configs/agent.yaml`) declares `shared_dir: "./shared"`. Everything you mount into your Dagger container must live under `praxis-go-sdk/shared`.
- `shared/` is mounted into Dagger containers according to the `engineSpec.mounts` mapping. In the sample configs the directory is mounted to `/shared` inside the container. Place your tool scripts, helper modules, data files, and generated artifacts in this folder so they are available to the container.

```
praxis-go-sdk/
├── configs/
│   └── agent.yaml          # add your tool definition here
└── shared/
    └── <your_tool_files>   # code and assets consumed by Dagger
```

## Step 1 – Add the tool implementation under `shared/`
1. Create your script or binary inside `shared/`. You can use any language supported by the container image you plan to run (Python, Node.js, shell, etc.).
2. Inside the script, read tool parameters from environment variables. The agent automatically exposes each parameter you pass when invoking the tool (for example, the `username` parameter becomes `$username` inside the container).
3. Write any outputs you want to persist back into `shared/`. The Dagger engine exports the mounted directories back to the host when the command finishes.

**Example (`shared/twitter_scraper.py`):**
```python
import os
import sys
from apify_client import ApifyClient

username = os.getenv("username")
limit = int(os.getenv("tweets_count", "50"))

if not username:
    sys.exit("Missing required username parameter")

client = ApifyClient(os.getenv("APIFY_API_TOKEN"))
# ... call Apify and print or write results ...
```

## Step 2 – Describe the tool in `configs/agent.yaml`
Add an entry under the `agent.tools` array with `engine: "dagger"` and complete the `engineSpec`. Below is a template you can copy.

```yaml
agent:
  tools:
    - name: "my_tool"
      description: "Short description of what the tool does"
      engine: "dagger"
      params:
        - name: "input_param"
          type: "string"
          description: "Explain how to use this parameter"
          required: true
      engineSpec:
        image: "python:3.11-slim"
        command: ["python", "/shared/my_tool.py"]
        mounts:
          ./shared: /shared
        env:                        # optional fixed env vars passed to the container
          LOG_LEVEL: "info"
        env_passthrough:            # optional list of host env vars to forward
          - "APIFY_API_TOKEN"
```

### `engineSpec` fields
- `image` (required): Container image to run. Use public images (e.g. `python:3.11-slim`) or your own registry.
- `command` (required): Command array executed inside the container. If you need shell features, wrap with `sh -c`, e.g. `["sh", "-c", "pip install -r /shared/requirements.txt && python /shared/task.py"]`.
- `mounts` (required): Mapping of host paths to container paths. Use `./shared: /shared` so your scripts and outputs are available inside the container. Additional mounts are allowed if needed.
- `env` (optional): A map of constant environment variables injected into the container.
- `env_passthrough` (optional): A list of environment variable names that should be copied from the host (for secrets such as API keys).

The agent converts tool parameters into environment variables for the container (matching the parameter names). No manual interpolation is required in `command`.

## Step 3 – Reload the agent
After editing the configuration:
1. Restart or redeploy the Praxis agent so it reads the updated `configs/agent.yaml`.
2. On startup, the agent detects tools with `engine: "dagger"`, initializes a shared Dagger engine instance, and registers the tool with the MCP runtime.

## Step 4 – Invoke the tool
Call the tool through your preferred interface (Praxis Explorer UI, MCP client, or API). Provide arguments that match the `params` list. Example payload:

```json
{
  "tool_name": "twitter_scraper",
  "arguments": {
    "username": "elonmusk",
    "tweets_count": 20
  }
}
```

Dagger runs the configured container, captures `stdout` as the tool response, and syncs any mutated files under `shared/` back to the host.

## Troubleshooting
- **"failed to connect to dagger"** – Ensure Docker is running and the Dagger CLI can create engines (`dagger version`).
- **Missing scripts or assets** – Confirm files live inside `shared/` and your mount path is `./shared: /shared`.
- **Command not found** – Install dependencies inside the command (e.g. `pip install -r /shared/requirements.txt`) before invoking the script.
- **Environment variable not available** – Verify the parameter name matches the variable your script reads, or add the variable to `env`/`env_passthrough`.

Following the steps above you can quickly package containerized tooling for Praxis. Keep scripts under `shared/`, describe the tool in `configs/agent.yaml` with `engine: "dagger"`, and let the agent orchestrate execution using the Dagger engine.
