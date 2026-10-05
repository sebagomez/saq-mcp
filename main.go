// Command saq-mcp is an MCP server exposing SAQ product search, pricing,
// and per-store inventory as tools, over stdio.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/sebagomez/saq-mcp/saq"
)

func mustJSON(v any) string {
	out, _ := json.MarshalIndent(v, "", "  ")
	return string(out)
}

func main() {
	client := saq.NewClient()
	s := server.NewMCPServer("saq-mcp", "0.1.0", server.WithToolCapabilities(true))

	searchTool := mcp.NewTool("search_products",
		mcp.WithDescription("Search the SAQ (Société des alcools du Québec) product catalog by keyword. Returns matching products with name, SKU, price in CAD, and stock status."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search keywords, e.g. a product name like 'coureur des bois'")),
		mcp.WithNumber("limit", mcp.Description("Maximum results to return (default 10, max 50)")),
	)
	s.AddTool(searchTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		products, err := client.SearchProducts(ctx, query, req.GetInt("limit", 10))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(mustJSON(products)), nil
	})

	detailsTool := mcp.NewTool("product_details",
		mcp.WithDescription("Get full details (name, price in CAD, stock status, product page URL) for one SAQ product by its SKU / SAQ code."),
		mcp.WithString("sku", mcp.Required(), mcp.Description("The SAQ product code (SKU), e.g. '11091921'")),
	)
	s.AddTool(detailsTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sku, err := req.RequireString("sku")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		p, err := client.ProductDetails(ctx, sku)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(mustJSON(p)), nil
	})

	storesTool := mcp.NewTool("stores_with_stock",
		mcp.WithDescription("List SAQ stores that carry a product, with bottles in stock per store. Optionally filter to stores near a location, sorted by distance."),
		mcp.WithString("sku", mcp.Required(), mcp.Description("The SAQ product code (SKU), e.g. '11091921'")),
		mcp.WithNumber("latitude", mcp.Description("Reference latitude for distance filtering/sorting")),
		mcp.WithNumber("longitude", mcp.Description("Reference longitude for distance filtering/sorting")),
		mcp.WithNumber("radius_km", mcp.Description("Only include stores within this many km (requires latitude/longitude; default 25)")),
		mcp.WithNumber("limit", mcp.Description("Maximum stores to return (default 50, max 200)")),
	)
	s.AddTool(storesTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sku, err := req.RequireString("sku")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		stores, err := client.StoresWithStock(ctx, sku,
			req.GetInt("limit", 50),
			req.GetFloat("latitude", 0),
			req.GetFloat("longitude", 0),
			req.GetFloat("radius_km", 25),
		)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(mustJSON(stores)), nil
	})

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "saq-mcp: server error: %v\n", err)
		os.Exit(1)
	}
}
