# Kubetail MCP Server

The Kubetail MCP server provides an interface for AI tools (like Claude Desktop) to access Kubernetes logs through natural language queries.

### Advanced Filtering

The MCP server supports many filtering options:

- **Basic Kubernetes filters**: namespace, pod, container
- **Infrastructure filters**: region, zone, architecture, OS, node
- **Content filtering**: grep patterns
- **Time-based filtering**: since, until, tail
- **Output control**: format, follow


## Architecture Overview

The MCP server is a **locally running component** of the Kubetail CLI, not a service that runs in the Kubernetes cluster. This design was chosen deliberately for several reasons:

1. **Security**: Running locally means your credentials stay on your machine
2. **Simplicity**: No need to deploy and manage another service
3. **Integration**: Works directly with your configured kubeconfig and contexts

### Component Relationships

```
┌───────────────────────────────────────┐
│ User's Local Machine                  │
│                                       │
│  ┌─────────────┐     ┌─────────────┐  │
│  │ AI Assistant│     │ Kubetail CLI│  │
│  │ (Claude)    │◄────┤ MCP Server  │  │
│  └─────────────┘     └─────┬───────┘  │
│                            │          │
└────────────────────────────┼──────────┘
                             │
                             ▼
┌───────────────────────────────────────┐
│ Kubernetes Cluster                    │
│  ┌─────────────────┐ ┌──────────────┐ │
│  │ Kubetail        │ │ Kubetail     │ │
│  │ Dashboard       │ │ Cluster API  │ │
│  └─────────────────┘ └───────┬──────┘ │
│                              │        │
│  ┌──────────────┐  ┌─────────▼──────┐ │
│  │ Your         │  │ Kubetail       │ │
│  │ Applications │  │ Cluster Agents │ │
│  └──────────────┘  └────────────────┘ │
└───────────────────────────────────────┘
```

## How It Works

The MCP server acts as a bridge between AI tools and your Kubernetes clusters by:

1. Starting a local Model Context Protocol (MCP) server
2. Exposing a `kubernetes_logs_search` tool to AI assistants
3. Using your kubeconfig to authenticate with your clusters
4. Automatically detecting if Kubetail's in-cluster components are available
5. Connecting through the most efficient path available:
   - First tries to use Kubetail Cluster API (proxied through Kubernetes API)
   - Falls back to standard Kubernetes API logs if Kubetail is not deployed

### Connection Flow

When the MCP server needs to access logs, it attempts these methods in order:

1. **Proxy through Kubernetes API to Cluster API**
   - Uses the Kubernetes API server to proxy to the Kubetail Cluster API
   - Works when Kubetail's in-cluster components are deployed
   - Provides enhanced features like server-side filtering
   
2. **Direct Kubernetes API**
   - Always works as long as your cluster is accessible
   - More limited in features (e.g., no server-side filtering)
   - Higher bandwidth usage for large log volumes


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






