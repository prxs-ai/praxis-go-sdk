# 📚 **COMPLETE GUIDE FOR COMPREHENSIVE TESTING OF PRAXIS P2P AGENT SYSTEM**

## 🚀 **1. SYSTEM STARTUP**

### **Step 1.1: Start MCP Filesystem Server**

```bash
# In the first terminal - start MCP server
cd /Users/drobotukhin/Desktop/prxs_go_client/praxis-go-sdk
go run mcp-filesystem-server.go ./shared ./configs &

# Check server functionality
curl -X POST http://localhost:3000/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}'
```

### **Step 1.2: Start Docker containers with agents**

```bash
# In the second terminal - start agents
docker-compose -f docker-compose-test.yml up -d

# Check container status
docker ps
docker logs praxis-agent-1 --tail 20
docker logs praxis-agent-2 --tail 20
```

### **Step 1.3: System health check**

```bash
# Agent status
curl http://localhost:8000/health
curl http://localhost:8001/health

# Check P2P connection
curl http://localhost:8000/p2p/info | jq
curl http://localhost:8000/p2p/cards | jq
```

## 🎯 **2. DAGGER ENGINE DEMONSTRATION**

### **Test data preparation**

```bash
# Create test files for analysis
echo "This is a test document with multiple words.
It contains text for analysis.
Python Dagger Engine will analyze this file.
Line count: 4, Word count should be around 20." > ./shared/demo_data.txt

echo "import pandas as pd
import numpy as np

data = pd.DataFrame({
    'name': ['Alice', 'Bob', 'Charlie'],
    'age': [25, 30, 35],
    'score': [85.5, 92.3, 88.7]
})

print(data.describe())" > ./shared/python_code.txt

echo "Lorem ipsum dolor sit amet, consectetur adipiscing elit.
Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.
Numbers: 123, 456, 789
Special chars: @#$%^&*()
Multiple lines for testing." > ./shared/complex_text.txt
```

### **🔥 DEMO 1: Simple file analysis via LLM**

```bash
curl -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{
    "dsl": "analyze file demo_data.txt using python_analyzer"
  }'
```

**Expected result:**

```json
{
  "status": "success",
  "message": "Python analyzer executed successfully via Dagger",
  "input_file": "demo_data.txt",
  "analysis": {
    "word_count": 20,
    "line_count": 4,
    "has_numbers": true
  }
}
```

### **🔥 DEMO 2: Python code analysis**

```bash
curl -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{
    "dsl": "use python_analyzer to analyze python_code.txt file and show statistics"
  }'
```

### **🔥 DEMO 3: Complex analysis with natural language**

```bash
curl -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{
    "dsl": "I need to analyze complex_text.txt file through python analyzer and get complete information about the content"
  }'
```

## 🌐 **3. P2P AND MCP TESTING**

### **Test 3.1: P2P peers list**

```bash
curl -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{
    "dsl": "show list of all connected P2P agents"
  }'
```

### **Test 3.2: Available tools check**

```bash
# All agent tools
curl http://localhost:8000/mcp/tools | jq '.tools[] | {name, description}'

# Tool count
curl http://localhost:8000/mcp/tools | jq '.count'
```

### **Test 3.3: Dynamically discovered external tools**

```bash
curl http://localhost:8000/mcp/tools | \
  jq '.tools[] | select(.name | contains("external")) | {name, description}'
```

## 🎭 **4. ADVANCED DEMONSTRATION SCENARIOS**

### **Scenario A: Multi-step workflow**

```bash
# Create test workflow
curl -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{
    "dsl": "first show list of P2P peers, then analyze demo_data.txt file"
  }'
```

### **Scenario B: LLM context understanding check**

```bash
curl -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{
    "dsl": "I have a file demo_data.txt, I want to know how many words it contains"
  }'
```

### **Scenario C: Caching test**

```bash
# First request
time curl -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{"dsl": "analyze demo_data.txt file through python"}'

# Check cache
curl http://localhost:8000/cache/stats

# Repeat request (should be faster)
time curl -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{"dsl": "analyze demo_data.txt file through python"}'
```

## 📊 **5. MONITORING AND DEBUGGING**

### **Real-time log viewing**

```bash
# Agent 1 logs
docker logs -f praxis-agent-1

# Agent 2 logs
docker logs -f praxis-agent-2

# Logs filtered by Dagger
docker logs praxis-agent-1 2>&1 | grep -i "dagger\|python\|analyzer"

# LLM interaction logs
docker logs praxis-agent-1 2>&1 | grep -i "llm\|orchestrator\|workflow"
```

### **Dagger Docker containers check**

```bash
# List all containers (including Dagger)
docker ps -a

# Containers created by Dagger
docker ps -a | grep python

# Clean up old Dagger containers
docker container prune -f
```

## 🎬 **6. PRESENTATION DEMONSTRATION SCENARIO**

### **Step 1: Show architecture**

```bash
echo "=== PRAXIS P2P AGENT SYSTEM ==="
echo "Components:"
echo "- 2 P2P Agents (Docker containers)"
echo "- MCP Filesystem Server (7 tools)"
echo "- Dagger Engine (Python analyzer)"
echo "- LLM Orchestrator (OpenAI GPT-4)"
```

### **Step 2: Natural language demonstration**

```bash
echo "=== NATURAL LANGUAGE UNDERSTANDING ==="
curl -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{
    "dsl": "hello, can you analyze demo_data.txt file and tell me how many words it has?"
  }' | jq
```

### **Step 3: Dagger in action**

```bash
echo "=== DAGGER ENGINE WITH PYTHON ==="
echo "Dagger launches isolated Python container..."
curl -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{
    "dsl": "run python_analyzer for complex_text.txt file"
  }' | jq

# Show running containers
docker ps | grep python
```

### **Step 4: P2P interaction**

```bash
echo "=== P2P NETWORK ==="
curl http://localhost:8000/p2p/cards | jq '.cards | to_entries[] | .value | {name, version, tools: (.tools | length)}'
```

### **Step 5: Dynamic MCP discovery**

```bash
echo "=== DYNAMIC MCP DISCOVERY ==="
echo "No hardcoded tools - everything discovered dynamically!"
curl http://localhost:8000/mcp/tools | jq '{
  total: .count,
  local_tools: [.tools[] | select(.name | contains("external") | not) | .name],
  external_tools: [.tools[] | select(.name | contains("external")) | .name]
}'
```

## ⚡ **7. QUICK TEST OF EVERYTHING**

Create file `test_all.sh`:

```bash
#!/bin/bash
# save as test_all.sh

echo "🚀 Starting comprehensive system test..."

# 1. Health check
echo -e "\n✅ Health Check:"
curl -s http://localhost:8000/health | jq

# 2. P2P Status
echo -e "\n🌐 P2P Network:"
curl -s -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{"dsl": "list P2P peers"}' | jq '.result.results[0].result.content[0].text'

# 3. Dagger Test
echo -e "\n🐳 Dagger Engine Test:"
curl -s -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{"dsl": "analyze demo_data.txt through python_analyzer"}' | jq

# 4. Tool Count
echo -e "\n🔧 Available Tools:"
curl -s http://localhost:8000/mcp/tools | jq '.count'

echo -e "\n✨ All tests completed!"
```

Run: `chmod +x test_all.sh && ./test_all.sh`

## 🐳 **8. HOW DAGGER WORKS IN THE SYSTEM**

**Dagger uses the "Docker-outside-of-Docker" (DooD) principle:**

### **DooD Architecture:**

```yaml
# In docker-compose-test.yml
volumes:
  - /var/run/docker.sock:/var/run/docker.sock  # Docker socket for Dagger
```

### **Working principle:**

1. **Praxis Agent** runs inside Docker container
2. When calling Dagger, it accesses **host** Docker daemon through socket
3. Dagger starts a **new container** on the same host (sibling containers)
4. Containers exchange data through shared volumes

### **Example python_analyzer execution:**

```go
// In configs/agent.yaml:
engineSpec:
  image: "python:3.11-slim"
  command: ["python", "/shared/analyzer.py"]
  mounts:
    ./shared: /shared
```

**Execution process:**

- Agent receives `python_analyzer` command
- Dagger creates `python:3.11-slim` container
- Mounts `./shared:/shared`
- Executes `python /shared/analyzer.py --input_file test_data.txt`
- Returns JSON result

### **DooD Benefits:**

- ✅ Dagger containers have full Docker API access
- ✅ No Docker-in-Docker (dind) issues
- ✅ Better performance
- ✅ Simple configuration

## 💡 **9. IMPORTANT POINTS FOR DEMO**

1. **Dagger works through Docker Socket**: Containers run on host, not inside agent container
2. **LLM understands context**: You can use conversational English
3. **No fallbacks**: All tools are discovered dynamically
4. **P2P automatic**: Agents find each other through mDNS
5. **Shared volumes**: `/shared` directory is accessible to all containers

## 🎯 **10. COMMAND FOR PERFECT DAGGER DEMO**

```bash
# Most effective Dagger demonstration:
curl -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{
    "dsl": "run analysis of demo_data.txt file through python analyzer in isolated container and show me complete statistics including word count, line count and presence of numbers"
  }' | jq
```

This command will show:

- ✅ LLM understands natural English
- ✅ Correctly selects `python_analyzer` tool
- ✅ Dagger launches Python container
- ✅ Analyzes file and returns JSON
- ✅ Complete workflow from request to result

## 🚨 **11. TROUBLESHOOTING**

### **Problem: MCP server not responding**

```bash
# Check
curl http://localhost:3000/mcp

# Solution
pkill -f mcp-filesystem-server
go run mcp-filesystem-server.go ./shared ./configs &
```

### **Problem: Containers won't start**

```bash
# Solution
docker-compose -f docker-compose-test.yml down
docker system prune -f
docker-compose -f docker-compose-test.yml up -d --build
```

### **Problem: LLM timeout**

```bash
# Check API key
echo $OPENAI_API_KEY

# Solution - export key
export OPENAI_API_KEY="your-api-key-here"
```

### **Problem: Dagger not working**

```bash
# Check Docker socket
ls -la /var/run/docker.sock

# Check permissions
docker run hello-world
```

## 📈 **12. METRICS AND STATISTICS**

### **Getting system metrics**

```bash
# Cache statistics
curl http://localhost:8000/cache/stats | jq

# Tool count
curl http://localhost:8000/mcp/tools | jq '.count'

# P2P connections
curl http://localhost:8000/p2p/cards | jq '. | length'
```

### **Performance monitoring**

```bash
# Command execution time
time curl -X POST http://localhost:8000/execute \
  -H "Content-Type: application/json" \
  -d '{"dsl": "analyze demo_data.txt"}'

# Resource usage
docker stats praxis-agent-1 praxis-agent-2
```

## ✅ **13. COMPLETE TESTING CHECKLIST**

- [ ] MCP server started and responding
- [ ] Docker containers healthy
- [ ] P2P agents can see each other
- [ ] 13 tools registered
- [ ] Dagger executes python_analyzer
- [ ] LLM understands English commands
- [ ] No fallback tools used
- [ ] Cache working correctly
- [ ] WebSocket/SSE endpoints accessible
- [ ] Logs without critical errors
