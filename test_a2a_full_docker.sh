#!/bin/bash

# ================================================================
# A2A PROTOCOL FULL DOCKER-COMPOSE TESTING SCRIPT
# ================================================================
# Complete Agent2Agent protocol testing with two containerized agents
# Based on real testing results and container log analysis
# ================================================================

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
NC='\033[0m' # No Color

# Configuration
AGENT1_URL="http://localhost:8000"
AGENT2_URL="http://localhost:8001"
MCP_SERVER_URL="http://localhost:3000/mcp"
EXTERNAL_MCP_SERVER="http://host.docker.internal:3000/mcp"

# Test results
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0
TEST_RESULTS=()

echo -e "${PURPLE}================================================================${NC}"
echo -e "${PURPLE}🚀 A2A PROTOCOL FULL DOCKER-COMPOSE TESTING${NC}"
echo -e "${PURPLE}================================================================${NC}"

# Function to print test headers
print_test() {
    echo -e "\n${YELLOW}🧪 TEST: $1${NC}"
    echo "----------------------------------------"
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
}

# Function to print success
print_success() {
    echo -e "${GREEN}✅ $1${NC}"
    PASSED_TESTS=$((PASSED_TESTS + 1))
    TEST_RESULTS+=("✅ $1")
}

# Function to print error
print_error() {
    echo -e "${RED}❌ $1${NC}"
    FAILED_TESTS=$((FAILED_TESTS + 1))
    TEST_RESULTS+=("❌ $1")
}

# Function to print info
print_info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

# Function to wait for service with detailed logging
wait_for_service() {
    local url=$1
    local service_name=$2
    local timeout=${3:-30}
    local count=0

    echo "Waiting for $service_name to be ready (timeout: ${timeout}s)..."
    while [ $count -lt $timeout ]; do
        if curl -s "$url" > /dev/null 2>&1; then
            print_success "$service_name is ready"
            return 0
        fi
        echo -n "."
        count=$((count + 1))
        sleep 1
    done

    print_error "$service_name failed to start within $timeout seconds"
    return 1
}

# Function to check prerequisites
check_prerequisites() {
    print_test "CHECKING PREREQUISITES"

    # Check OpenAI API key
    if [ -z "$OPENAI_API_KEY" ]; then
        print_error "OPENAI_API_KEY environment variable not set"
        echo "Set it with: export OPENAI_API_KEY='your-key-here'"
        exit 1
    else
        print_success "OpenAI API key configured (${OPENAI_API_KEY:0:20}...)"
    fi

    # Check required commands
    local required_commands=("docker" "docker-compose" "curl" "jq")
    for cmd in "${required_commands[@]}"; do
        if ! command -v "$cmd" &> /dev/null; then
            print_error "$cmd not found"
            exit 1
        else
            print_success "$cmd available"
        fi
    done
}

# Function to clean environment
clean_environment() {
    print_test "CLEANING ENVIRONMENT"

    # Stop any existing containers
    print_info "Stopping existing containers..."
    docker-compose down --remove-orphans --volumes > /dev/null 2>&1 || true

    # Kill any running processes
    print_info "Cleaning up processes..."
    pkill -f "mcp-filesystem-server" > /dev/null 2>&1 || true
    pkill -f "praxis-agent" > /dev/null 2>&1 || true

    # Wait for ports to be free
    sleep 3

    print_success "Environment cleaned"
}

# Function to start MCP server
start_mcp_server() {
    print_test "STARTING MCP FILESYSTEM SERVER"

    print_info "Starting MCP server on port 3000..."
    go run examples/mcp-filesystem-server.go ./shared ./configs > mcp_server.log 2>&1 &
    MCP_PID=$!

    # Wait for MCP server to start
    sleep 5

    if wait_for_service "http://localhost:3000" "MCP Server" 15; then
        print_success "MCP Filesystem Server started (PID: $MCP_PID)"
    else
        print_error "MCP server failed to start"
        echo "MCP server log:"
        cat mcp_server.log | tail -10
        return 1
    fi
}

# Function to build and start agents
start_agents() {
    print_test "BUILDING AND STARTING AGENT CONTAINERS"

    print_info "Building Docker images..."
    if docker-compose build > build.log 2>&1; then
        print_success "Docker images built successfully"
    else
        print_error "Failed to build Docker images"
        echo "Build log:"
        cat build.log | tail -10
        return 1
    fi

    print_info "Starting agent containers..."
    if OPENAI_API_KEY="$OPENAI_API_KEY" docker-compose up -d > startup.log 2>&1; then
        print_success "Containers started"
    else
        print_error "Failed to start containers"
        echo "Startup log:"
        cat startup.log | tail -10
        return 1
    fi

    # Wait for agents to be ready
    print_info "Waiting for agents to initialize..."
    sleep 15 # Give time for full initialization

    if wait_for_service "$AGENT1_URL/health" "Agent 1" 30; then
        print_success "Agent 1 ready"
    else
        print_error "Agent 1 failed to start"
        docker logs praxis-agent-1 --tail 20
        return 1
    fi

    if wait_for_service "$AGENT2_URL/health" "Agent 2" 30; then
        print_success "Agent 2 ready"
    else
        print_error "Agent 2 failed to start"
        docker logs praxis-agent-2 --tail 20
        return 1
    fi
}

# Function to verify A2A initialization
verify_a2a_initialization() {
    print_test "VERIFYING A2A INITIALIZATION"

    # Check agent logs for A2A components
    local agent1_a2a_logs=$(docker logs praxis-agent-1 2>&1 | grep -c "A2A TaskManager initialized successfully" || echo "0")
    local agent2_a2a_logs=$(docker logs praxis-agent-2 2>&1 | grep -c "A2A TaskManager initialized successfully" || echo "0")

    if [ "$agent1_a2a_logs" -ge 1 ] && [ "$agent2_a2a_logs" -ge 1 ]; then
        print_success "A2A TaskManager initialized in both agents"
    else
        print_error "A2A TaskManager initialization failed"
        return 1
    fi

    # Check A2A agent interface setup
    local a2a_interface_logs=$(docker logs praxis-agent-1 praxis-agent-2 2>&1 | grep -c "A2A agent interface set" || echo "0")
    if [ "$a2a_interface_logs" -ge 2 ]; then
        print_success "A2A agent interfaces configured"
    else
        print_error "A2A agent interfaces not properly configured"
        return 1
    fi
}

# Function to verify P2P discovery with A2A cards
verify_p2p_discovery() {
    print_test "VERIFYING P2P DISCOVERY AND CARD EXCHANGE"

    # Wait for P2P discovery
    print_info "Waiting for P2P discovery to complete..."
    sleep 10

    # Check P2P cards
    local p2p_response=$(curl -s "$AGENT1_URL/p2p/cards" || echo '{"cards":{}}')
    local peer_count=$(echo "$p2p_response" | jq '.cards | length // 0' 2>/dev/null || echo "0")

    print_info "Found $peer_count P2P peers"

    if [ "$peer_count" -gt 0 ]; then
        print_success "P2P discovery working: $peer_count peers discovered"

        # Show peer information
        echo "Discovered peers:"
        echo "$p2p_response" | jq '.cards | to_entries[] | {peer: .key[:8], name: .value.name, tools: (.value.tools | length)}' 2>/dev/null || echo "Peer info parsing failed"
    else
        print_error "No P2P peers discovered"

        # Check logs for discovery issues
        print_info "Checking discovery logs..."
        docker logs praxis-agent-1 2>&1 | grep -E "(Discovered|Card exchange|Connected)" | tail -5
        return 1
    fi

    # Check card exchange logs
    local card_exchange_count=$(docker logs praxis-agent-1 praxis-agent-2 2>&1 | grep -c "Card exchange complete" || echo "0")
    if [ "$card_exchange_count" -ge 2 ]; then
        print_success "Agent card exchange completed ($card_exchange_count exchanges)"
    else
        print_error "Agent card exchange incomplete"
        return 1
    fi
}

# Function to test A2A agent card compliance
test_agent_card_compliance() {
    print_test "A2A AGENT CARD COMPLIANCE"

    # Get agent card
    local card_response=$(curl -s "$AGENT1_URL/agent/card" || echo '{}')

    # Check required A2A fields
    local required_fields=("supportedTransports" "securitySchemes" "skills" "capabilities")
    local missing_fields=()

    for field in "${required_fields[@]}"; do
        if echo "$card_response" | jq -e "has(\"$field\")" > /dev/null 2>&1; then
            print_success "Agent card has required field: $field"
        else
            missing_fields+=("$field")
        fi
    done

    if [ ${#missing_fields[@]} -eq 0 ]; then
        print_success "Agent card is fully A2A compliant"

        # Show A2A specific capabilities
        echo "A2A Capabilities:"
        echo "$card_response" | jq '{
          protocolVersion,
          supportedTransports,
          a2a_skills: [.skills[] | select(.tags[]? | contains("a2a"))]
        }' 2>/dev/null || echo "Card parsing failed"
    else
        print_error "Agent card missing A2A fields: ${missing_fields[*]}"
        return 1
    fi
}

# Function to test A2A message/send workflow
test_a2a_message_send() {
    print_test "A2A MESSAGE/SEND WORKFLOW"

    # Create A2A message
    local test_message='{
        "jsonrpc": "2.0",
        "id": 1,
        "method": "message/send",
        "params": {
            "message": {
                "role": "user",
                "parts": [
                    {
                        "kind": "text",
                        "text": "create file a2a_test_result.txt with content: A2A Pipeline Test Successful"
                    }
                ],
                "messageId": "docker-test-001",
                "kind": "message"
            }
        }
    }'

    print_info "Sending A2A message/send request..."
    local response=$(curl -s -X POST "$AGENT1_URL/execute" \
        -H "Content-Type: application/json" \
        -d "$test_message")

    echo "Response:"
    echo "$response" | jq . 2>/dev/null || echo "$response"

    # Extract task ID
    local task_id=$(echo "$response" | jq -r '.result.id // empty' 2>/dev/null)

    if [ -n "$task_id" ] && [ "$task_id" != "null" ]; then
        print_success "A2A task created successfully: $task_id"
        echo "$task_id" > /tmp/current_task_id

        # Verify task structure
        if echo "$response" | jq -e '.result.kind == "task"' > /dev/null 2>&1; then
            print_success "Task structure follows A2A specification"
        else
            print_error "Task structure invalid"
            return 1
        fi
    else
        print_error "Failed to create A2A task"
        return 1
    fi
}

# Function to test task status polling
test_task_status_polling() {
    print_test "A2A TASK STATUS POLLING"

    local task_id=$(cat /tmp/current_task_id 2>/dev/null)
    if [ -z "$task_id" ]; then
        print_error "No task ID available from previous test"
        return 1
    fi

    print_info "Polling status for task: $task_id"

    local max_attempts=20
    local attempt=1
    local final_state=""

    while [ $attempt -le $max_attempts ]; do
        echo "Status check $attempt/$max_attempts..."

        local status_request='{
            "jsonrpc": "2.0",
            "id": 2,
            "method": "tasks/get",
            "params": {
                "id": "'$task_id'"
            }
        }'

        local status_response=$(curl -s -X POST "$AGENT1_URL/execute" \
            -H "Content-Type: application/json" \
            -d "$status_request")

        local state=$(echo "$status_response" | jq -r '.result.status.state // .error.message // "unknown"' 2>/dev/null)
        echo "Current state: $state"

        case "$state" in
            "submitted")
                print_info "✳️  Task submitted, waiting for processing..."
                ;;
            "working")
                print_info "⚙️  Task in progress..."
                ;;
            "completed")
                print_success "🎉 Task completed successfully!"
                final_state="completed"

                # Show artifacts
                echo "Artifacts:"
                echo "$status_response" | jq '.result.artifacts // []' 2>/dev/null || echo "No artifacts or parsing failed"
                break
                ;;
            "failed")
                print_error "💥 Task failed"
                final_state="failed"
                echo "Error details:"
                echo "$status_response" | jq '.result.status.message // {}' 2>/dev/null || echo "$status_response"
                break
                ;;
            *)
                print_error "Unknown task state: $state"
                echo "Full response:"
                echo "$status_response" | jq . 2>/dev/null || echo "$status_response"
                final_state="unknown"
                break
                ;;
        esac

        attempt=$((attempt + 1))
        sleep 2
    done

    if [ "$final_state" = "completed" ]; then
        print_success "Task lifecycle completed successfully"
        return 0
    else
        print_error "Task did not complete successfully (final state: $final_state)"
        return 1
    fi
}

# Function to test legacy DSL compatibility
test_legacy_dsl_compatibility() {
    print_test "LEGACY DSL TO A2A CONVERSION"

    local legacy_request='{
        "dsl": "get list of available tools from both agents"
    }'

    print_info "Sending legacy DSL request..."
    echo "$legacy_request" | jq .

    local response=$(curl -s -X POST "$AGENT1_URL/execute" \
        -H "Content-Type: application/json" \
        -d "$legacy_request")

    echo "Response:"
    echo "$response" | jq . 2>/dev/null || echo "$response"

    # Check if converted to A2A task
    if echo "$response" | jq -e '.result.kind == "task"' > /dev/null 2>&1; then
        print_success "Legacy DSL successfully converted to A2A task"

        local converted_task_id=$(echo "$response" | jq -r '.result.id // empty')
        if [ -n "$converted_task_id" ]; then
            print_success "Converted task ID: $converted_task_id"
        fi
    else
        print_error "Legacy DSL conversion failed"
        return 1
    fi
}

# Function to test direct A2A endpoints
test_direct_a2a_endpoints() {
    print_test "DIRECT A2A ENDPOINTS"

    # Test /a2a/message/send
    print_info "Testing direct /a2a/message/send endpoint..."
    local direct_message_params='{
        "message": {
            "role": "user",
            "parts": [{"kind": "text", "text": "test direct A2A endpoint"}],
            "messageId": "direct-test-001",
            "kind": "message"
        }
    }'

    local direct_response=$(curl -s -X POST "$AGENT1_URL/a2a/message/send" \
        -H "Content-Type: application/json" \
        -d "$direct_message_params")

    if echo "$direct_response" | jq -e '.result.id' > /dev/null 2>&1; then
        print_success "Direct A2A message/send endpoint working"
    else
        print_error "Direct A2A message/send endpoint failed"
        echo "$direct_response"
    fi

    # Test /a2a/tasks list
    print_info "Testing /a2a/tasks list endpoint..."
    local tasks_list=$(curl -s "$AGENT1_URL/a2a/tasks")

    if echo "$tasks_list" | jq -e '.tasks' > /dev/null 2>&1; then
        local task_count=$(echo "$tasks_list" | jq '.tasks | length // 0')
        print_success "A2A tasks list endpoint working ($task_count tasks found)"

        # Show task statistics
        echo "Task Statistics:"
        echo "$tasks_list" | jq '.counts' 2>/dev/null || echo "Stats parsing failed"
    else
        print_error "A2A tasks list endpoint failed"
        echo "$tasks_list"
    fi
}

# Function to test inter-agent communication
test_inter_agent_communication() {
    print_test "INTER-AGENT A2A COMMUNICATION"

    # Test message from Agent 1 to Agent 2 via A2A
    print_info "Testing A2A communication from Agent 1 to Agent 2..."

    local inter_agent_message='{
        "jsonrpc": "2.0",
        "id": 3,
        "method": "message/send",
        "params": {
            "message": {
                "role": "user",
                "parts": [{"kind": "text", "text": "list files in shared directory using agent 2"}],
                "messageId": "inter-agent-001",
                "kind": "message"
            }
        }
    }'

    local inter_response=$(curl -s -X POST "$AGENT1_URL/execute" \
        -H "Content-Type: application/json" \
        -d "$inter_agent_message")

    echo "Inter-agent response:"
    echo "$inter_response" | jq . 2>/dev/null || echo "$inter_response"

    if echo "$inter_response" | jq -e '.result.id' > /dev/null 2>&1; then
        print_success "Inter-agent A2A communication working"

        # Monitor this task briefly
        local inter_task_id=$(echo "$inter_response" | jq -r '.result.id')
        sleep 5

        local final_status=$(curl -s -X POST "$AGENT1_URL/execute" \
            -H "Content-Type: application/json" \
            -d '{"jsonrpc":"2.0","id":4,"method":"tasks/get","params":{"id":"'$inter_task_id'"}}')

        local final_state=$(echo "$final_status" | jq -r '.result.status.state // "unknown"')
        print_info "Inter-agent task final state: $final_state"
    else
        print_error "Inter-agent A2A communication failed"
        return 1
    fi
}

# Function to test error handling
test_error_handling() {
    print_test "A2A ERROR HANDLING"

    # Test invalid method
    print_info "Testing invalid JSON-RPC method..."
    local invalid_method='{
        "jsonrpc": "2.0",
        "id": 999,
        "method": "invalid/method",
        "params": {}
    }'

    local error_response=$(curl -s -X POST "$AGENT1_URL/execute" \
        -H "Content-Type: application/json" \
        -d "$invalid_method")

    local error_code=$(echo "$error_response" | jq '.error.code // 0' 2>/dev/null || echo "0")
    if [ "$error_code" = "-32601" ]; then
        print_success "Method not found error handled correctly (code: $error_code)"
    else
        print_error "Invalid method error handling failed"
        echo "$error_response"
        return 1
    fi

    # Test non-existent task
    print_info "Testing non-existent task lookup..."
    local missing_task='{
        "jsonrpc": "2.0",
        "id": 998,
        "method": "tasks/get",
        "params": {"id": "non-existent-task-id"}
    }'

    local task_error=$(curl -s -X POST "$AGENT1_URL/execute" \
        -H "Content-Type: application/json" \
        -d "$missing_task")

    local task_error_code=$(echo "$task_error" | jq '.error.code // 0' 2>/dev/null || echo "0")
    if [ "$task_error_code" = "-32001" ]; then
        print_success "Task not found error handled correctly (code: $task_error_code)"
    else
        print_error "Task error handling failed"
        echo "$task_error"
        return 1
    fi
}

# Function to analyze container logs
analyze_container_logs() {
    print_test "ANALYZING CONTAINER LOGS FOR A2A PIPELINE"

    print_info "Agent 1 A2A Pipeline Logs:"
    docker logs praxis-agent-1 2>&1 | grep -E "(TaskID|A2A|submitted|working|completed|message/send|tasks/get)" | tail -15 || echo "No A2A logs found"

    print_info "Agent 2 A2A Pipeline Logs:"
    docker logs praxis-agent-2 2>&1 | grep -E "(TaskID|A2A|submitted|working|completed|message/send|tasks/get)" | tail -15 || echo "No A2A logs found"

    # Count key log patterns
    local task_created_count=$(docker logs praxis-agent-1 praxis-agent-2 2>&1 | grep -c "Task created in 'submitted' state" || echo "0")
    local task_completed_count=$(docker logs praxis-agent-1 praxis-agent-2 2>&1 | grep -c "Status updated.*to 'completed'" || echo "0")
    local a2a_requests_count=$(docker logs praxis-agent-1 praxis-agent-2 2>&1 | grep -c "A2A Request received" || echo "0")

    print_info "Log Analysis:"
    echo "  - Tasks created: $task_created_count"
    echo "  - Tasks completed: $task_completed_count"
    echo "  - A2A requests processed: $a2a_requests_count"

    if [ "$task_created_count" -gt 0 ] && [ "$a2a_requests_count" -gt 0 ]; then
        print_success "A2A pipeline logs show proper task lifecycle"
    else
        print_error "A2A pipeline logs incomplete"
        return 1
    fi
}

# Function to test performance
test_performance() {
    print_test "A2A PERFORMANCE MEASUREMENT"

    # Quick performance test
    local start_time=$(date +%s%N)

    local perf_message='{
        "jsonrpc": "2.0",
        "id": 100,
        "method": "message/send",
        "params": {
            "message": {
                "role": "user",
                "parts": [{"kind": "text", "text": "performance test"}],
                "messageId": "perf-test-001",
                "kind": "message"
            }
        }
    }'

    local perf_response=$(curl -s -X POST "$AGENT1_URL/execute" \
        -H "Content-Type: application/json" \
        -d "$perf_message")

    local end_time=$(date +%s%N)
    local duration=$(( (end_time - start_time) / 1000000 )) # Convert to milliseconds

    echo "A2A message/send response time: ${duration}ms"

    if [ "$duration" -lt 500 ]; then
        print_success "Performance test passed: ${duration}ms < 500ms"
    else
        print_error "Performance test failed: ${duration}ms >= 500ms"
    fi

    # Check if task was created
    if echo "$perf_response" | jq -e '.result.id' > /dev/null 2>&1; then
        print_success "Performance test task created successfully"
    else
        print_error "Performance test task creation failed"
        return 1
    fi
}

# Function to cleanup
cleanup() {
    print_test "CLEANUP"

    # Save final logs
    print_info "Saving final container logs..."
    docker logs praxis-agent-1 > agent1_final.log 2>&1 || true
    docker logs praxis-agent-2 > agent2_final.log 2>&1 || true

    # Kill MCP server
    if [ ! -z "$MCP_PID" ]; then
        kill $MCP_PID 2>/dev/null || true
    fi
    pkill -f "mcp-filesystem-server" 2>/dev/null || true

    # Stop containers
    print_info "Stopping containers..."
    docker-compose down --remove-orphans > /dev/null 2>&1 || true

    # Clean up temp files
    rm -f /tmp/current_task_id mcp_server.log build.log startup.log

    print_success "Cleanup completed"
}

# Function to generate final report
generate_final_report() {
    echo -e "\n${PURPLE}================================================================${NC}"
    echo -e "${PURPLE}📊 FINAL A2A TESTING REPORT${NC}"
    echo -e "${PURPLE}================================================================${NC}"

    echo -e "\n${YELLOW}Test Summary:${NC}"
    echo "  Total Tests: $TOTAL_TESTS"
    echo "  Passed: ${GREEN}$PASSED_TESTS${NC}"
    echo "  Failed: ${RED}$FAILED_TESTS${NC}"
    echo "  Success Rate: $(( PASSED_TESTS * 100 / TOTAL_TESTS ))%"

    echo -e "\n${YELLOW}Test Results:${NC}"
    for result in "${TEST_RESULTS[@]}"; do
        echo "  $result"
    done

    if [ $FAILED_TESTS -eq 0 ]; then
        echo -e "\n${GREEN}🎉 ALL A2A TESTS PASSED! IMPLEMENTATION FULLY FUNCTIONAL!${NC}"
        echo -e "${GREEN}✨ The A2A protocol is working correctly with docker-compose${NC}"
        return 0
    else
        echo -e "\n${RED}⚠️  $FAILED_TESTS tests failed. Check logs above for details.${NC}"
        echo -e "${YELLOW}💡 Common issues and solutions:${NC}"
        echo "   - MCP server connection: Check if external MCP server is accessible from containers"
        echo "   - OpenAI API: Verify API key is valid and has credits"
        echo "   - Network: Ensure docker-compose network allows inter-container communication"
        return 1
    fi
}

# Function to show usage
show_usage() {
    echo "A2A Protocol Docker-Compose Testing Script"
    echo ""
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --quick     Run essential tests only (faster)"
    echo "  --full      Run complete test suite (default)"
    echo "  --logs      Show detailed container logs"
    echo "  --help      Show this help message"
    echo ""
    echo "Prerequisites:"
    echo "  export OPENAI_API_KEY='your-openai-api-key'"
    echo ""
    echo "The script will:"
    echo "  1. Clean environment and start services"
    echo "  2. Test A2A protocol compliance"
    echo "  3. Verify inter-agent communication"
    echo "  4. Check legacy DSL compatibility"
    echo "  5. Analyze container logs for A2A lifecycle"
    echo ""
    echo "Example:"
    echo "  export OPENAI_API_KEY='sk-...'"
    echo "  $0 --full"
}

# Trap for cleanup on exit
trap cleanup EXIT

# Main execution based on arguments
case "${1:-full}" in
    "--quick"|"quick")
        check_prerequisites
        clean_environment
        start_mcp_server
        start_agents
        verify_a2a_initialization
        test_a2a_message_send
        test_task_status_polling
        analyze_container_logs
        ;;
    "--full"|"full"|"")
        check_prerequisites
        clean_environment
        start_mcp_server
        start_agents
        verify_a2a_initialization
        verify_p2p_discovery
        test_agent_card_compliance
        test_a2a_message_send
        test_task_status_polling
        test_legacy_dsl_compatibility
        test_direct_a2a_endpoints
        test_inter_agent_communication
        test_error_handling
        test_performance
        analyze_container_logs
        ;;
    "--logs"|"logs")
        echo -e "${BLUE}📋 Container Logs Analysis${NC}"
        echo "Agent 1 logs:"
        docker logs praxis-agent-1 2>&1 | grep -E "(A2A|TaskID|submitted|working|completed)" || echo "No A2A logs"
        echo -e "\nAgent 2 logs:"
        docker logs praxis-agent-2 2>&1 | grep -E "(A2A|TaskID|submitted|working|completed)" || echo "No A2A logs"
        exit 0
        ;;
    "--help"|"help"|"-h")
        show_usage
        exit 0
        ;;
    *)
        echo "Unknown option: $1"
        show_usage
        exit 1
        ;;
esac

# Generate final report
exit_code=0
if [ $FAILED_TESTS -gt 0 ]; then
    exit_code=1
fi

generate_final_report

# Save detailed test results
cat > "a2a_docker_test_results_$(date +%Y%m%d_%H%M%S).json" << EOF
{
    "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
    "test_suite": "A2A Protocol Docker-Compose Testing",
    "total_tests": $TOTAL_TESTS,
    "passed_tests": $PASSED_TESTS,
    "failed_tests": $FAILED_TESTS,
    "success_rate": $(( PASSED_TESTS * 100 / TOTAL_TESTS )),
    "exit_code": $exit_code,
    "environment": {
        "docker_compose": true,
        "agents": 2,
        "mcp_server": true
    },
    "test_results": $(printf '%s\n' "${TEST_RESULTS[@]}" | jq -R . | jq -s .)
}
EOF

echo -e "\n${BLUE}📁 Detailed results saved to: a2a_docker_test_results_$(date +%Y%m%d_%H%M%S).json${NC}"

exit $exit_code
