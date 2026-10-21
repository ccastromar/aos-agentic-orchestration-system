"""A real MCP server (official Python SDK, Streamable HTTP with sessions and SSE responses).

    python mcp_server.py [port]      # default 18110, endpoint /mcp

Tools: lookup_customer(customer_id: str, limit: int) and broken() (always fails, so the
client must see isError).
"""
import sys

from mcp.server.mcpserver import MCPServer

server = MCPServer("aos-interop-crm")


@server.tool()
def lookup_customer(customer_id: str, limit: int) -> dict:
    """Look up a CRM customer. `limit` must arrive as an integer."""
    if not isinstance(limit, int):
        raise TypeError(f"limit must be an integer, got {type(limit).__name__}")
    return {"customerId": customer_id, "name": "Interop Customer", "orders": list(range(limit))}


@server.tool()
def broken() -> str:
    """Always fails, like a backend that is down."""
    raise RuntimeError("CRM backend unavailable")


if __name__ == "__main__":
    import anyio

    port = int(sys.argv[1]) if len(sys.argv) > 1 else 18110
    anyio.run(lambda: server.run_streamable_http_async(host="127.0.0.1", port=port))
