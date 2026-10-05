package saq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	graphqlURL   = "https://www.saq.com/graphql"
	productURL   = "https://www.saq.com/fr/%s" // %s = SKU
	inventoryURL = "https://www.saq.com/fr/store/locator/ajaxlist/context/product/id/%s?loaded=%d"

	userAgent      = "saq-mcp/0.1.0 (+https://github.com/sebagomez/saq-mcp)"
	requestTimeout = 20 * time.Second
	pageSize       = 10 // stores per inventory page
)

var productIDRe = regexp.MustCompile(`data-product-id="(\d+)"`)

// Client talks to SAQ's (undocumented) storefront endpoints.
type Client struct {
	http *http.Client
}

// NewClient returns a client with a polite timeout and user agent.
func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: requestTimeout}}
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("saq: unexpected status %d", resp.StatusCode)
	}
	return body, nil
}

// gql runs a GraphQL query against saq.com.
func (c *Client) gql(ctx context.Context, query string) ([]byte, error) {
	payload, _ := json.Marshal(map[string]string{"query": query})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphqlURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

type gqlProductItem struct {
	Name        string `json:"name"`
	SKU         string `json:"sku"`
	StockStatus string `json:"stock_status"`
	PriceRange  struct {
		MinimumPrice struct {
			RegularPrice struct {
				Value    float64 `json:"value"`
				Currency string  `json:"currency"`
			} `json:"regular_price"`
		} `json:"minimum_price"`
	} `json:"price_range"`
}

func toProduct(it gqlProductItem) Product {
	return Product{
		Name:        it.Name,
		SKU:         it.SKU,
		Price:       it.PriceRange.MinimumPrice.RegularPrice.Value,
		Currency:    it.PriceRange.MinimumPrice.RegularPrice.Currency,
		StockStatus: it.StockStatus,
		URL:         fmt.Sprintf(productURL, it.SKU),
	}
}

// SearchProducts searches the SAQ catalog by keyword.
func (c *Client) SearchProducts(ctx context.Context, query string, limit int) ([]Product, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	q := fmt.Sprintf(`{ products(search: %s, pageSize: %d) { items { name sku stock_status price_range { minimum_price { regular_price { value currency } } } } } }`,
		strconv.Quote(query), limit)
	body, err := c.gql(ctx, q)
	if err != nil {
		return nil, err
	}
	var res struct {
		Data struct {
			Products struct {
				Items []gqlProductItem `json:"items"`
			} `json:"products"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	out := make([]Product, 0, len(res.Data.Products.Items))
	for _, it := range res.Data.Products.Items {
		out = append(out, toProduct(it))
	}
	return out, nil
}

// ProductDetails returns full details for one product by SKU.
func (c *Client) ProductDetails(ctx context.Context, sku string) (Product, error) {
	q := fmt.Sprintf(`{ products(filter: {sku: {eq: %s}}) { items { name sku stock_status price_range { minimum_price { regular_price { value currency } } } } } }`,
		strconv.Quote(strings.TrimSpace(sku)))
	body, err := c.gql(ctx, q)
	if err != nil {
		return Product{}, err
	}
	var res struct {
		Data struct {
			Products struct {
				Items []gqlProductItem `json:"items"`
			} `json:"products"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return Product{}, err
	}
	if len(res.Data.Products.Items) == 0 {
		return Product{}, fmt.Errorf("saq: no product found for sku %q", sku)
	}
	return toProduct(res.Data.Products.Items[0]), nil
}

// entityID resolves a SKU to the Magento entity ID used by the inventory endpoint.
func (c *Client) entityID(ctx context.Context, sku string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(productURL, sku), nil)
	if err != nil {
		return "", err
	}
	body, err := c.do(req)
	if err != nil {
		return "", err
	}
	m := productIDRe.FindSubmatch(body)
	if m == nil {
		return "", fmt.Errorf("saq: could not find product id on page for sku %q", sku)
	}
	return string(m[1]), nil
}

type storeRaw struct {
	Name      string `json:"name"`
	Address1  string `json:"address1"`
	Address2  string `json:"address2"`
	City      string `json:"city"`
	Postcode  string `json:"postcode"`
	Telephone string `json:"telephone"`
	Latitude  string `json:"latitude"`
	Longitude string `json:"longitude"`
	Qty       any    `json:"qty"`
}

func toFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	}
	return 0
}

// StoresWithStock returns stores carrying the product (SKU), paged through the
// inventory endpoint until maxStores is reached. When lat/lng are provided,
// results are filtered to radiusKm and sorted by distance.
func (c *Client) StoresWithStock(ctx context.Context, sku string, maxStores int, lat, lng, radiusKm float64) ([]Store, error) {
	if maxStores <= 0 || maxStores > 200 {
		maxStores = 50
	}
	id, err := c.entityID(ctx, sku)
	if err != nil {
		return nil, err
	}
	locate := lat != 0 || lng != 0
	// The endpoint's page order isn't by distance, so when filtering by
	// location we walk more pages to give nearby stores a chance to appear.
	// Sequential and polite: ~1 request per page.
	maxPages := 3
	if locate {
		maxPages = 15
	}
	var out []Store
	for page := 0; page < maxPages; page++ {
		loaded := page * pageSize
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(inventoryURL, id, loaded), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("Accept", "application/json")
		body, err := c.do(req)
		if err != nil {
			return nil, err
		}
		var res struct {
			List       []storeRaw `json:"list"`
			IsLastPage bool       `json:"is_last_page"`
		}
		if err := json.Unmarshal(body, &res); err != nil {
			return nil, err
		}
		for _, s := range res.List {
			addr := strings.TrimSpace(s.Address1)
			if s.Address2 != "" {
				addr += ", " + strings.TrimSpace(s.Address2)
			}
			st := Store{
				Name:       s.Name,
				Address:    addr,
				City:       s.City,
				Postcode:   s.Postcode,
				Phone:      s.Telephone,
				Latitude:   toFloat(s.Latitude),
				Longitude:  toFloat(s.Longitude),
				QtyBottles: toInt(s.Qty),
			}
			if locate {
				st.DistanceKm = math.Round(haversine(lat, lng, st.Latitude, st.Longitude)*10) / 10
				if radiusKm > 0 && st.DistanceKm > radiusKm {
					continue
				}
			}
			out = append(out, st)
			if len(out) >= maxStores {
				break
			}
		}
		if res.IsLastPage || len(res.List) == 0 || len(out) >= maxStores {
			break
		}
	}
	if locate {
		// insertion sort by distance (lists are small)
		for i := 1; i < len(out); i++ {
			for j := i; j > 0 && out[j].DistanceKm < out[j-1].DistanceKm; j-- {
				out[j], out[j-1] = out[j-1], out[j]
			}
		}
	}
	return out, nil
}

// haversine returns the great-circle distance in km.
func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * R * math.Asin(math.Sqrt(a))
}
