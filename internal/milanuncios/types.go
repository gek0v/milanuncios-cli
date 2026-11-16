package milanuncios

import (
	"fmt"
	"io"
	"strings"

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

// Pretty renders a detailed view of an ad.
func (a *AdItem) Pretty(w io.Writer, color bool) {
	priceStr := fmt.Sprintf("%.0f %s", a.Price, a.Currency)
	if a.Currency == "" {
		priceStr = fmt.Sprintf("%.0f €", a.Price)
	}
	if a.Price == 0 {
		priceStr = "Gratis"
	}

	if color {
		fmt.Fprintf(w, "\x1b[1m%s\x1b[0m  (%s)\n", a.Title, priceStr)
	} else {
		fmt.Fprintf(w, "%s  (%s)\n", a.Title, priceStr)
	}
	fmt.Fprintf(w, "ID: %s  |  URL: %s\n", a.ID, a.URL)

	loc := a.Location.Municipality
	if loc == "" {
		loc = a.Location.Province
	} else if a.Location.Province != "" {
		loc += " (" + a.Location.Province + ")"
	}
	if loc != "" {
		fmt.Fprintf(w, "Localización: %s\n", loc)
	}

	if a.Seller.Name != "" {
		sellerType := a.Seller.Type
		if sellerType != "" {
			sellerType = " [" + sellerType + "]"
		}
		fmt.Fprintf(w, "Vendedor: %s%s\n", a.Seller.Name, sellerType)
	}
	if a.PublishedAt != "" {
		fmt.Fprintf(w, "Publicado: %s\n", a.PublishedAt)
	}

	if len(a.Photos) > 0 {
		fmt.Fprintf(w, "Fotos (%d):\n", len(a.Photos))
		for i, photo := range a.Photos {
			fmt.Fprintf(w, "  [%d] %s\n", i+1, photo)
		}
	}

	if a.Description != "" {
		fmt.Fprintf(w, "\n%s\n", strings.TrimSpace(a.Description))
	}
}

// ImagesResult wraps photo URLs for an ad.
type ImagesResult struct {
	AdID   string   `json:"ad_id"`
	Photos []string `json:"photos"`
}

// Pretty renders photos of an ad.
func (r *ImagesResult) Pretty(w io.Writer, color bool) {
	if len(r.Photos) == 0 {
		fmt.Fprintf(w, "No photos available for ad %s\n", r.AdID)
		return
	}
	fmt.Fprintf(w, "Photos for ad %s (%d):\n", r.AdID, len(r.Photos))
	for i, photo := range r.Photos {
		fmt.Fprintf(w, "  [%d] %s\n", i+1, photo)
	}
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
