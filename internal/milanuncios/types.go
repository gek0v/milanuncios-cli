package milanuncios

import (
	"fmt"
	"io"

	"github.com/gek0v/milanuncios-cli/internal/output"
)

// AdLocation represents location details for an ad listing.
type AdLocation struct {
	Province     string  `json:"province,omitempty"`
	Municipality string  `json:"municipality,omitempty"`
	PostalCode   string  `json:"postal_code,omitempty"`
	Lat          float64 `json:"lat,omitempty"`
	Lng          float64 `json:"lng,omitempty"`
}

// AdSeller represents advertiser details.
type AdSeller struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"` // "particular" or "profesional"
}

// AdItem represents a single ad listing on Milanuncios.
type AdItem struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Price       float64    `json:"price"`
	Currency    string     `json:"currency"`
	Location    AdLocation `json:"location"`
	Photos      []string   `json:"photos,omitempty"`
	URL         string     `json:"url"`
	Seller      AdSeller   `json:"seller"`
	Category    int        `json:"category,omitempty"`
	PublishedAt string     `json:"published_at,omitempty"`
	ModifiedAt  string     `json:"modified_at,omitempty"`
}

// SearchQuery specifies criteria for querying Milanuncios listings.
type SearchQuery struct {
	Keywords string
	MinPrice float64
	MaxPrice float64
	Province string
	Category int
	Sort     string // "relevance", "newest", "price_asc", "price_desc"
	Limit    int
	Next     string
}

// SearchResult contains a page of listings and pagination info.
type SearchResult struct {
	Items    []AdItem `json:"items"`
	NextPage string   `json:"next_page,omitempty"`
	Total    int      `json:"total"`
}

// Pretty renders search results in a clean human-readable table.
func (r *SearchResult) Pretty(w io.Writer, color bool) {
	if len(r.Items) == 0 {
		fmt.Fprintln(w, "No ads found matching search criteria.")
		return
	}

	header := []string{"ID", "TITLE", "PRICE", "LOCATION", "SELLER"}
	rows := make([][]string, 0, len(r.Items))

	for _, it := range r.Items {
		loc := it.Location.Municipality
		if loc == "" {
			loc = it.Location.Province
		} else if it.Location.Province != "" {
			loc += " (" + it.Location.Province + ")"
		}

		priceStr := fmt.Sprintf("%.0f %s", it.Price, it.Currency)
		if it.Currency == "" {
			priceStr = fmt.Sprintf("%.0f €", it.Price)
		}
		if it.Price == 0 {
			priceStr = "Gratis"
		}

		sellerStr := it.Seller.Name
		if it.Seller.Type != "" {
			sellerStr += " [" + it.Seller.Type + "]"
		}

		rows = append(rows, []string{
			it.ID,
			output.Truncate(it.Title, 40),
			priceStr,
			output.Truncate(loc, 24),
			output.Truncate(sellerStr, 20),
		})
	}

	output.Table(w, color, header, rows)
	if r.NextPage != "" {
		fmt.Fprintf(w, "\nNext page cursor: %s (pass --next %s to fetch next page)\n", r.NextPage, r.NextPage)
	}
}
