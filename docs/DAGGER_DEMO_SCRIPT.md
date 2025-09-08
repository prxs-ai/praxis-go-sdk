# Dagger Engine Demo Script (2 Minutes)

## 🎬 Introduction (15 seconds)

"Hi everyone! Today I'll demonstrate how Dagger powers our Praxis P2P Agent System to execute isolated, containerized workloads dynamically.

Dagger is a programmable CI/CD engine that runs pipelines inside containers, giving us complete isolation and reproducibility for our Python analysis tasks."

## 🏗️ Architecture Overview (20 seconds)

"Our system consists of:
- Two P2P agents running in Docker containers
- An MCP filesystem server providing dynamic tool discovery
- Dagger Engine orchestrating Python analyzers in isolated containers
- An LLM that understands natural language and routes requests

The key here is Dagger's 'Docker-outside-of-Docker' approach - our agents access the host Docker daemon through a socket, allowing them to spin up sibling containers for analysis tasks."

## 🚀 Live Demo Setup (15 seconds)

"Let me show you the system in action. First, let's verify everything is running:

```bash
# Check agent health
curl http://localhost:8000/health

# Verify P2P network - agents discovered each other
curl http://localhost:8000/p2p/cards | jq
```

We have two agents connected via P2P, with 13 dynamically discovered tools available."

## 💫 Dagger in Action (45 seconds)

"Now, let's see Dagger execute a Python analysis. I'll send a natural language request:

```bash
curl -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{
    "dsl": "analyze the demo_data.txt file using python analyzer and show me complete statistics"
  }'
```

Watch what happens:
1. The LLM understands my request and identifies the python_analyzer tool
2. Dagger spins up a fresh Python 3.11 container
3. Mounts the shared volume with our data
4. Executes the analysis script in complete isolation
5. Returns structured JSON results

Let's check the container that Dagger created:

```bash
docker ps | grep python
```

See? Dagger created a Python container on-demand, ran our analysis, and cleaned up. Complete isolation, no dependencies conflicts!"

## 🔄 Advanced Workflow (20 seconds)

"Dagger really shines with complex workflows. Let me run a multi-step analysis:

```bash
curl -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{
    "dsl": "first list all P2P peers, then analyze complex_text.txt through python analyzer"
  }'
```

The system chains operations seamlessly - P2P discovery followed by containerized Python analysis, all orchestrated through Dagger."

## 🎯 Key Benefits (15 seconds)

"What makes this powerful:
- **Zero configuration** - Tools discovered dynamically at runtime
- **Complete isolation** - Each analysis runs in its own container
- **Language agnostic** - Dagger can run Python, Node, Go, anything
- **Reproducible** - Same container, same results, every time
- **Scalable** - Spin up hundreds of containers in parallel"

## 🏁 Conclusion (10 seconds)

"That's Dagger in our P2P agent system - turning natural language into containerized, isolated executions. No manual Docker commands, no dependency hell, just pure programmable infrastructure.

Questions? Check out dagger.io for more. Thanks for watching!"

---

## 📝 Quick Reference Commands

For the demo presenter - key commands to copy/paste:

```bash
# Initial setup check
curl http://localhost:8000/health | jq

# Show P2P network
curl http://localhost:8000/p2p/cards | jq '.cards | length'

# Main Dagger demo
curl -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{"dsl": "analyze demo_data.txt using python_analyzer"}' | jq

# Show containers
docker ps | grep python

# Multi-step workflow
curl -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{"dsl": "list P2P peers then analyze complex_text.txt"}' | jq
```

## 🎙️ Speaking Notes

- **Pace**: Keep it energetic but clear - 2 minutes goes fast
- **Focus**: Emphasize isolation, dynamic discovery, and natural language
- **Visual**: Show terminal with commands executing live
- **Highlight**: The moment when Dagger spins up the Python container
- **End strong**: "No configuration, just code"