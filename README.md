# saq-mcp

An MCP (Model Context Protocol) server for the **SAQ** (Société des alcools du
Québec) — lets AI agents search products, check prices, find per-store
inventory, and build a cart in your SAQ.com account that you check out
yourself, the way you'd do it by hand on saq.com.

## Tools

| Tool | What it does |
|---|---|
| `search_products` | Search the SAQ catalog by name/keyword. Returns name, SKU, price (CAD), stock status. |
| `product_details` | Full details for one product by SKU. |
| `stores_with_stock` | Stores carrying a product, with bottles in stock per store. Optional `latitude`/`longitude`/`radius_km` to find the closest ones. |
| `get_cart` | Show your SAQ.com cart: items, quantities, total. Needs `SAQ_EMAIL`/`SAQ_PASSWORD`. |
| `add_to_cart` | Add bottles to your SAQ.com cart by SKU. Returns the updated cart. Needs `SAQ_EMAIL`/`SAQ_PASSWORD`. |
| `remove_from_cart` | Remove one line from your cart by item UID. Needs `SAQ_EMAIL`/`SAQ_PASSWORD`. |
| `clear_cart` | Empty your cart. Needs `SAQ_EMAIL`/`SAQ_PASSWORD`. |

Example agent queries this enables:

- "Find me a bottle of Coureur des Bois near Longueuil"
- "Which SAQ near me has this wine in stock, and how many bottles?"
- "How much is X at the SAQ right now?"
- "Put together a cart of three Bordeaux under $30 for pickup" *(you check out and pay at saq.com)*

## Cart — "shop for me, I'll pay"

The cart tools build a real cart in **your SAQ.com account**: the agent
searches, picks the bottles, and fills the cart — you open saq.com in your
browser (logged in), check out, and pay. Payment details never touch the
agent.

This needs your SAQ.com credentials, because only a *customer* cart persists
into the browser session (guest carts can't be handed off — SAQ has no
restore-by-ID UI):

```json
{
  "mcpServers": {
    "saq": {
      "command": "/path/to/saq-mcp",
      "env": {
        "SAQ_EMAIL": "you@example.com",
        "SAQ_PASSWORD": "your-saq-password"
      }
    }
  }
}
```

The server exchanges them for a short-lived customer token via
`generateCustomerToken`, cached in memory and refreshed automatically. The
password is never logged or written anywhere.

## How it works

SAQ publishes no official public API. This server uses two reverse-engineered
storefront endpoints (both work without authentication):

1. **Product search** — the site's open Magento GraphQL endpoint:
   `POST https://www.saq.com/graphql`
2. **Per-store inventory** — the store-locator JSON endpoint:
   `GET https://www.saq.com/fr/store/locator/ajaxlist/context/product/id/{magento_id}?loaded={offset}`
   with header `X-Requested-With: XMLHttpRequest`. Paginated (10/page), each
   store includes a `qty` field plus address, hours, and coordinates. The
   Magento product ID is scraped from the product page's `data-product-id`.

Because these are undocumented storefront endpoints, they can change — and the
server keeps request rates polite.

## Build

```bash
go build -o saq-mcp .
```

## Testing

No model subscription needed — you can call the tools directly.

**MCP Inspector** (official, free, no account):

```bash
npx @modelcontextprotocol/inspector
```

Opens a UI at http://localhost:6274. Set transport to `stdio`, command to
`/path/to/saq-mcp`, click Connect, then Tools → List Tools to invoke any tool
with your own arguments. Cart tools need `SAQ_EMAIL`/`SAQ_PASSWORD` set in the
Inspector's environment section.

**Raw JSON-RPC** over stdin (zero dependencies):

```bash
printf '%s\n' \
'{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}' \
'{"jsonrpc":"2.0","method":"notifications/initialized"}' \
'{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
| ./saq-mcp
```

For a real agent driving the tools without a paid subscription,
[Goose](https://block.github.io/goose/) (open-source, MCP-native) with a free
model tier works, as does Ollama + Open WebUI fully local.

## Use with Claude Desktop / any MCP client (stdio)

```json
{
  "mcpServers": {
    "saq": {
      "command": "/path/to/saq-mcp"
    }
  }
}
```

## Disclaimer

Unofficial project. Not affiliated with the SAQ. Product data belongs to the SAQ.
