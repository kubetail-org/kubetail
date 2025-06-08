# Kubetail MCP Server Documentation

## Overview
The Kubetail MCP (Model Context Protocol) server exposes advanced Kubernetes log search and streaming capabilities to AI tools and external clients. It enables natural language log queries, advanced filtering, and real-time log streaming across clusters, regions, and architectures. The MCP server is designed for seamless integration with AI assistants (e.g., Claude Desktop) and automation tools.

## Use Case
- **AI-driven log analysis:** Allow AI tools to search, summarize, and analyze Kubernetes logs using natural language queries.
- **Automated troubleshooting:** Integrate with bots or scripts to fetch logs for incidents, alerts, or CI/CD pipelines.
- **Unified log access:** Provide a single endpoint for querying logs across multiple clusters, namespaces, and infrastructure layers.

## Features
- **Natural Language Log Search:** Accepts queries like "Show me error logs from nginx pods in production since yesterday."
- **Advanced Filtering:** Filter logs by namespace, pod, container, region, zone, architecture, OS, node, grep pattern, and time range.
- **Server-Side Processing:** When possible, filters logs at the source (via cluster agents) for efficiency and scalability.
- **Multi-Cluster & Multi-Region:** Aggregate logs from multiple clusters and cloud regions.
- **Real-Time Streaming:** Supports tailing and following logs as they arrive.
- **Flexible Output:** Returns results as structured JSON, summaries, or raw log lines.
- **AI Integration:** Implements the Model Context Protocol for easy AI tool consumption.

## Architecture
The MCP server uses a three-tier log fetcher selection strategy:

1. **Direct Agent Access (gRPC):** Fastest, most feature-rich. Attempts to connect directly to the cluster agent via gRPC.
2. **Cluster API Proxy (GraphQL):** If direct access fails, uses the Kubernetes service proxy to reach the cluster agent via GraphQL.
3. **Kubernetes API (KubeLogFetcher):** As a last resort, fetches logs directly from the Kubernetes API.


## Example Usage

### Start the MCP Server
```sh
./bin/kubetail mcp-server
```

### Example Log Search (via AI tool or API)
- "Find API timeout logs in the production namespace since 1 hour ago."
- "Show me error logs from nginx pods in us-west-2 region."


## Setup Instructions

### Build the Project
```sh
make build
```

### Example CLI Usage

You can invoke the MCP server directly using a JSON-RPC request piped to the CLI. For example:

```sh
echo '{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": {"name": "kubernetes_logs_search", "arguments": {"query": "test", "namespace": "kube-system"}}}' | ./bin/kubetail mcp-server 2>&1 | grep -E "(debug|error|info)" | jq
```


### Run the MCP Server
```sh
./bin/kubetail mcp-server
```

## Connecting to Desktop AI Tools (e.g., Claude Desktop)

To use the Kubetail MCP server with a desktop AI tool like Claude Desktop, add the following to your Claude Desktop configuration file (usually `claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "kubetail": {
      "command": "kubetail",
      "args": ["mcp-server"]
    }
  }
}
```

For detailed instructions on configuring Claude Desktop for Mac or Windows, see:
- [Claude Desktop Configuration Guide](https://modelcontextprotocol.io/quickstart/user)

After saving the config, restart Claude Desktop. You can now use natural language queries.



