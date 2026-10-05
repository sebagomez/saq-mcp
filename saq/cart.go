package saq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Cart support: "shop for me, I'll pay".
//
// The agent builds the cart in the user's SAQ.com account; the user opens
// saq.com in their browser (logged in) to check out and pay. Payment details
// never touch the agent.
//
// Only a *customer* cart persists into the browser session, so cart tools
// require SAQ.com account credentials via SAQ_EMAIL / SAQ_PASSWORD. Guest
// carts are intentionally not used: they can't be handed off (SAQ has no
// restore-by-ID UI), so a guest cart would strand the user's bottles.
//
// Credentials are only ever sent to SAQ's generateCustomerToken endpoint and
// are never logged or persisted anywhere.

// CartItem is one line in the SAQ cart.
type CartItem struct {
	UID       string  `json:"uid"`
	Name      string  `json:"name"`
	SKU       string  `json:"sku"`
	Quantity  float64 `json:"quantity"`
	UnitPrice float64 `json:"unit_price"`
	RowTotal  float64 `json:"row_total"`
	Currency  string  `json:"currency"`
}

// Cart is the user's SAQ.com cart.
type Cart struct {
	Items    []CartItem `json:"items"`
	Total    float64    `json:"total"`
	Currency string     `json:"currency"`
}

// WithCredentials attaches SAQ.com account credentials used by the cart tools.
// Reads are unaffected.
func (c *Client) WithCredentials(email, password string) *Client {
	c.email, c.password = email, password
	return c
}

type gqlErr struct{ messages []string }

func (e *gqlErr) Error() string { return "saq: " + strings.Join(e.messages, "; ") }

func isAuthError(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "authoriz") ||
		strings.Contains(s, "unauthenticated") ||
		strings.Contains(s, "invalid token")
}

// gqlDo posts a GraphQL operation, attaching the bearer token when non-empty.
func (c *Client) gqlDo(ctx context.Context, query, token string) ([]byte, error) {
	payload, _ := json.Marshal(map[string]string{"query": query})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphqlURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return c.do(req)
}

// gqlParse unmarshals a GraphQL response envelope into out, surfacing any
// GraphQL errors as a single error.
func gqlParse(body []byte, out any) error {
	var res struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return err
	}
	if len(res.Errors) > 0 {
		msgs := make([]string, 0, len(res.Errors))
		for _, e := range res.Errors {
			msgs = append(msgs, e.Message)
		}
		return &gqlErr{messages: msgs}
	}
	return json.Unmarshal(res.Data, out)
}

// ensureToken returns a cached customer token, generating one on first use.
// The password is sent only to SAQ's generateCustomerToken endpoint.
func (c *Client) ensureToken(ctx context.Context) (string, error) {
	if c.email == "" || c.password == "" {
		return "", fmt.Errorf("saq: cart tools need SAQ_EMAIL and SAQ_PASSWORD set to your SAQ.com account " +
			"(the cart is built in your account so you can check out in the browser)")
	}
	if c.token != "" {
		return c.token, nil
	}
	q := fmt.Sprintf(`mutation { generateCustomerToken(email: %s, password: %s) { token } }`,
		strconv.Quote(c.email), strconv.Quote(c.password))
	body, err := c.gqlDo(ctx, q, "")
	if err != nil {
		return "", err
	}
	var res struct {
		GenerateCustomerToken struct {
			Token string `json:"token"`
		} `json:"generateCustomerToken"`
	}
	if err := gqlParse(body, &res); err != nil {
		return "", fmt.Errorf("saq: SAQ.com sign-in failed (check SAQ_EMAIL/SAQ_PASSWORD): %v", err)
	}
	if res.GenerateCustomerToken.Token == "" {
		return "", fmt.Errorf("saq: SAQ.com did not return a token — check SAQ_EMAIL/SAQ_PASSWORD")
	}
	c.token = res.GenerateCustomerToken.Token
	return c.token, nil
}

// authedQuery runs a GraphQL operation as the SAQ account holder, refreshing
// the token once if the server rejects it.
func (c *Client) authedQuery(ctx context.Context, query string, out any) error {
	token, err := c.ensureToken(ctx)
	if err != nil {
		return err
	}
	body, err := c.gqlDo(ctx, query, token)
	if err != nil {
		return err
	}
	if err := gqlParse(body, out); err != nil {
		if isAuthError(err) {
			c.token = "" // stale or expired; try once with a fresh token
			token, err = c.ensureToken(ctx)
			if err != nil {
				return err
			}
			body, err = c.gqlDo(ctx, query, token)
			if err != nil {
				return err
			}
			return gqlParse(body, out)
		}
		return err
	}
	return nil
}

const cartFields = `items { uid quantity product { name sku } prices { price { value currency } row_total { value } } } prices { grand_total { value currency } }`

type gqlCart struct {
	Items []struct {
		UID      string  `json:"uid"`
		Quantity float64 `json:"quantity"`
		Product  struct {
			Name string `json:"name"`
			SKU  string `json:"sku"`
		} `json:"product"`
		Prices struct {
			Price struct {
				Value    float64 `json:"value"`
				Currency string  `json:"currency"`
			} `json:"price"`
			RowTotal struct {
				Value float64 `json:"value"`
			} `json:"row_total"`
		} `json:"prices"`
	} `json:"items"`
	Prices struct {
		GrandTotal struct {
			Value    float64 `json:"value"`
			Currency string  `json:"currency"`
		} `json:"grand_total"`
	} `json:"prices"`
}

func toCart(g gqlCart) Cart {
	cart := Cart{
		Items:    make([]CartItem, 0, len(g.Items)),
		Total:    g.Prices.GrandTotal.Value,
		Currency: g.Prices.GrandTotal.Currency,
	}
	for _, it := range g.Items {
		cart.Items = append(cart.Items, CartItem{
			UID:       it.UID,
			Name:      it.Product.Name,
			SKU:       it.Product.SKU,
			Quantity:  it.Quantity,
			UnitPrice: it.Prices.Price.Value,
			RowTotal:  it.Prices.RowTotal.Value,
			Currency:  it.Prices.Price.Currency,
		})
	}
	return cart
}

// customerCartID resolves the account's cart ID (created on demand by SAQ).
func (c *Client) customerCartID(ctx context.Context) (string, error) {
	var res struct {
		CustomerCart struct {
			ID string `json:"id"`
		} `json:"customerCart"`
	}
	if err := c.authedQuery(ctx, `query { customerCart { id } }`, &res); err != nil {
		return "", err
	}
	if res.CustomerCart.ID == "" {
		return "", fmt.Errorf("saq: could not resolve your SAQ.com cart")
	}
	return res.CustomerCart.ID, nil
}

// GetCart returns the current cart from the user's SAQ.com account.
func (c *Client) GetCart(ctx context.Context) (Cart, error) {
	var res struct {
		CustomerCart gqlCart `json:"customerCart"`
	}
	if err := c.authedQuery(ctx, `query { customerCart { `+cartFields+` } }`, &res); err != nil {
		return Cart{}, err
	}
	return toCart(res.CustomerCart), nil
}

// AddToCart adds qty bottles of the product (by SAQ code/SKU) to the cart and
// returns the updated cart.
func (c *Client) AddToCart(ctx context.Context, sku string, qty int) (Cart, error) {
	sku = strings.TrimSpace(sku)
	if sku == "" {
		return Cart{}, fmt.Errorf("saq: sku is required")
	}
	if qty <= 0 || qty > 99 {
		return Cart{}, fmt.Errorf("saq: quantity must be between 1 and 99")
	}
	cartID, err := c.customerCartID(ctx)
	if err != nil {
		return Cart{}, err
	}
	q := fmt.Sprintf(`mutation { addProductsToCart(cartId: %s, cartItems: [{sku: %s, quantity: %d}]) { cart { %s } } }`,
		strconv.Quote(cartID), strconv.Quote(sku), qty, cartFields)
	var res struct {
		AddProductsToCart struct {
			Cart gqlCart `json:"cart"`
		} `json:"addProductsToCart"`
	}
	if err := c.authedQuery(ctx, q, &res); err != nil {
		return Cart{}, err
	}
	return toCart(res.AddProductsToCart.Cart), nil
}

// RemoveFromCart removes one line (by item UID from GetCart) and returns the
// updated cart.
func (c *Client) RemoveFromCart(ctx context.Context, uid string) (Cart, error) {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return Cart{}, fmt.Errorf("saq: item uid is required (see get_cart)")
	}
	cartID, err := c.customerCartID(ctx)
	if err != nil {
		return Cart{}, err
	}
	q := fmt.Sprintf(`mutation { removeItemFromCart(input: {cart_id: %s, cart_item_uid: %s}) { cart { %s } } }`,
		strconv.Quote(cartID), strconv.Quote(uid), cartFields)
	var res struct {
		RemoveItemFromCart struct {
			Cart gqlCart `json:"cart"`
		} `json:"removeItemFromCart"`
	}
	if err := c.authedQuery(ctx, q, &res); err != nil {
		return Cart{}, err
	}
	return toCart(res.RemoveItemFromCart.Cart), nil
}

// ClearCart removes every line from the cart, sequentially and politely.
func (c *Client) ClearCart(ctx context.Context) (Cart, error) {
	cart, err := c.GetCart(ctx)
	if err != nil {
		return Cart{}, err
	}
	for _, it := range cart.Items {
		cart, err = c.RemoveFromCart(ctx, it.UID)
		if err != nil {
			return Cart{}, err
		}
	}
	return cart, nil
}
