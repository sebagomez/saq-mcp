# saq-mcp

An MCP (Model Context Protocol) server for the **SAQ** (Société des alcools du
Québec) — lets AI agents search products, check prices, and find per-store
inventory, the way you'd do it by hand on saq.com.

## Tools

| Tool | What it does |
|---|---|
| `search_products` | Search the SAQ catalog by name/keyword. Returns name, SKU, price (CAD), stock status. |
| `product_details` | Full details for one product by SKU. |
| `stores_with_stock` | Stores carrying a product, with bottles in stock per store. Optional `latitude`/`longitude`/`radius_km` to find the closest ones. |

Example agent queries this enables:

- "Find me a bottle of Coureur des Bois near Longueuil"
- "Which SAQ near me has this wine in stock, and how many bottles?"
- "How much is X at the SAQ right now?"

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
