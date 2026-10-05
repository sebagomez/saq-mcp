package saq

// Product is a single SAQ catalog item.
type Product struct {
	Name        string  `json:"name"`
	SKU         string  `json:"sku"`
	Price       float64 `json:"price"`
	Currency    string  `json:"currency"`
	StockStatus string  `json:"stock_status"`
	URL         string  `json:"url"`
}

// Store is an SAQ retail location with per-product inventory.
type Store struct {
	Name       string  `json:"name"`
	Address    string  `json:"address"`
	City       string  `json:"city"`
	Postcode   string  `json:"postcode"`
	Phone      string  `json:"phone"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	QtyBottles int     `json:"qty_bottles"`
	// DistanceKm is populated only when the caller filters by location.
	DistanceKm float64 `json:"distance_km,omitempty"`
}
