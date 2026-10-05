# AGENTS.md

Context for AI agents working on this repository.

## What this is

`saq-mcp` is a Go MCP (Model Context Protocol) server exposing SAQ
(Société des alcools du Québec) product search, pricing, and per-store
inventory as tools, over stdio. Built with
[mark3labs/mcp-go](https://github.com/mark3labs/mcp-go).

## Layout

```
main.go        MCP server wiring: tool definitions + stdio transport
saq/client.go  HTTP client for the two SAQ endpoints (GraphQL search,
               store-locator inventory), incl. product-page scraping for
               the Magento entity ID
saq/cart.go    Authenticated cart tools (generateCustomerToken, customer
               cart CRUD); credentials via SAQ_EMAIL/SAQ_PASSWORD env only
saq/types.go   Product, Store, Inventory, Cart types
```

## Conventions

- Keep it dependency-light: stdlib + mcp-go only.
- All SAQ calls go through `saq/client.go`. Never build SAQ URLs in `main.go`.
- Be polite to SAQ's servers: sequential requests, small page sizes, no
  concurrency against their endpoints.
- Tools return concise JSON. The agent reading it is the UI — no pretty
  printing needed.
- French product/store names are data, not UI: pass them through verbatim.
- Cart tools need the user's SAQ.com account because only a *customer* cart
  persists into the browser session for checkout (guest carts can't be handed
  off). Credentials come from env vars only — never log them, never persist
  them, only send them to `generateCustomerToken`.

## Endpoints (undocumented, reverse-engineered — verify if broken)

1. `POST https://www.saq.com/graphql`
   `{ products(search: "...", pageSize: N) { items { name sku stock_status
   price_range { minimum_price { regular_price { value currency } } } } } }`
2. `GET https://www.saq.com/fr/store/locator/ajaxlist/context/product/id/{id}?loaded={offset}`
   Header `X-Requested-With: XMLHttpRequest` required. The `{id}` is the
   Magento entity ID from the product page's `data-product-id` attribute
   (page URL is `https://www.saq.com/fr/{sku}`).
3. `POST https://www.saq.com/graphql` — cart mutations (require a customer
   token from `generateCustomerToken(email, password) { token }` sent as
   `Authorization: Bearer`):
   `customerCart { id items { ... } }`, `addProductsToCart`, `removeItemFromCart`.
   SKU = the SAQ product code (e.g. `11091921`).
