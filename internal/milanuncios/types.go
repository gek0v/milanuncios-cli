package milanuncios

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
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

// Category represents a node in the Milanuncios category taxonomy.
type Category struct {
	ID       int        `json:"id"`
	Name     string     `json:"name"`
	Slug     string     `json:"slug,omitempty"`
	Children []Category `json:"children,omitempty"`
}

// CategoryListResult is a slice of categories with human-readable table rendering.
type CategoryListResult []Category

// Pretty prints the categories taxonomy with indentation.
func (l CategoryListResult) Pretty(w io.Writer, color bool) {
	if len(l) == 0 {
		fmt.Fprintln(w, "No categories available.")
		return
	}

	header := []string{"ID", "NAME", "SLUG"}
	var rows [][]string
	for _, c := range l {
		rows = append(rows, []string{
			strconv.Itoa(c.ID),
			c.Name,
			c.Slug,
		})
		for _, child := range c.Children {
			rows = append(rows, []string{
				"  " + strconv.Itoa(child.ID),
				"  └─ " + child.Name,
				child.Slug,
			})
		}
	}
	output.Table(w, color, header, rows)
}

// FilterDefinition describes an attribute filter available for a category.
type FilterDefinition struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Type        string   `json:"type"` // "string", "number", "select", "boolean"
	Options     []string `json:"options,omitempty"`
	Description string   `json:"description,omitempty"`
}

// CategoryFiltersResult holds category metadata and valid filter keys.
type CategoryFiltersResult struct {
	CategoryID   int                `json:"category_id"`
	CategoryName string             `json:"category_name,omitempty"`
	Filters      []FilterDefinition `json:"filters"`
}

// Pretty prints category filter definitions in a structured table.
func (r *CategoryFiltersResult) Pretty(w io.Writer, color bool) {
	if len(r.Filters) == 0 {
		fmt.Fprintf(w, "No specific filters found for category %d\n", r.CategoryID)
		return
	}

	catName := r.CategoryName
	if catName == "" {
		catName = fmt.Sprintf("ID %d", r.CategoryID)
	}
	fmt.Fprintf(w, "Available filters for %s:\n\n", catName)

	header := []string{"KEY", "LABEL", "TYPE", "OPTIONS"}
	var rows [][]string
	for _, f := range r.Filters {
		opts := strings.Join(f.Options, ", ")
		rows = append(rows, []string{
			f.Key,
			f.Label,
			f.Type,
			output.Truncate(opts, 45),
		})
	}
	output.Table(w, color, header, rows)
	fmt.Fprintln(w, "\nApply filters in search with: milanuncios search --category <id> --filter key=value")
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
	Filters  map[string]string
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

// UserProfile represents the authenticated user's account details.
type UserProfile struct {
	ID        string  `json:"id"`
	Hash      string  `json:"hash,omitempty"`
	Name      string  `json:"name"`
	Email     string  `json:"email,omitempty"`
	Phone     string  `json:"phone,omitempty"`
	Verified  bool    `json:"verified"`
	Reviews   int     `json:"reviews_count"`
	Rating    float64 `json:"rating"`
	CreatedAt string  `json:"created_at,omitempty"`
}

// Pretty prints the user account profile.
func (u *UserProfile) Pretty(w io.Writer, color bool) {
	if color {
		fmt.Fprintf(w, "\x1b[1m%s\x1b[0m\n", u.Name)
	} else {
		fmt.Fprintf(w, "%s\n", u.Name)
	}
	fmt.Fprintf(w, "ID: %s\n", u.ID)
	if u.Email != "" {
		fmt.Fprintf(w, "Email: %s\n", u.Email)
	}
	if u.Phone != "" {
		fmt.Fprintf(w, "Teléfono: %s\n", u.Phone)
	}
	ver := "No verificado"
	if u.Verified {
		ver = "Verificado"
	}
	fmt.Fprintf(w, "Estado: %s\n", ver)
	if u.Reviews > 0 {
		fmt.Fprintf(w, "Valoraciones: %.1f ★ (%d opiniones)\n", u.Rating, u.Reviews)
	}
	if u.CreatedAt != "" {
		fmt.Fprintf(w, "Miembro desde: %s\n", u.CreatedAt)
	}
}

// Conversation represents a chat thread associated with an ad listing.
type Conversation struct {
	ID          string  `json:"id"`
	AdID        string  `json:"ad_id"`
	AdTitle     string  `json:"ad_title"`
	AdPrice     float64 `json:"ad_price"`
	UserName    string  `json:"user_name"`
	LastMessage string  `json:"last_message"`
	LastSentAt  string  `json:"last_sent_at"`
	Unread      bool    `json:"unread"`
}

// ConversationList wraps conversations for tabular rendering.
type ConversationList []Conversation

// Pretty renders conversation threads in a table.
func (cl ConversationList) Pretty(w io.Writer, color bool) {
	if len(cl) == 0 {
		fmt.Fprintln(w, "No conversations found.")
		return
	}

	header := []string{"ID", "USER", "ITEM", "PRICE", "LAST MESSAGE", "UNREAD"}
	rows := make([][]string, 0, len(cl))

	for _, c := range cl {
		unreadStr := "no"
		if c.Unread {
			unreadStr = "YES"
		}

		priceStr := fmt.Sprintf("%.0f €", c.AdPrice)
		if c.AdPrice == 0 {
			priceStr = "-"
		}

		rows = append(rows, []string{
			c.ID,
			output.Truncate(c.UserName, 20),
			output.Truncate(c.AdTitle, 26),
			priceStr,
			output.Truncate(c.LastMessage, 30),
			unreadStr,
		})
	}

	output.Table(w, color, header, rows)
}

// Message represents an individual message in a chat conversation.
type Message struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	SenderID       string `json:"sender_id"`
	SenderName     string `json:"sender_name"`
	Text           string `json:"text"`
	SentAt         string `json:"sent_at"`
	FromMe         bool   `json:"from_me"`
}

// ConversationDetails contains full message history for a conversation thread.
type ConversationDetails struct {
	Conversation Conversation `json:"conversation"`
	Messages     []Message    `json:"messages"`
}

// Pretty prints the full conversation history.
func (cd *ConversationDetails) Pretty(w io.Writer, color bool) {
	c := cd.Conversation
	if color {
		fmt.Fprintf(w, "\x1b[1mConversación #%s con %s\x1b[0m\n", c.ID, c.UserName)
	} else {
		fmt.Fprintf(w, "Conversación #%s con %s\n", c.ID, c.UserName)
	}
	fmt.Fprintf(w, "Anuncio: %s (%.0f €) [ID: %s]\n\n", c.AdTitle, c.AdPrice, c.AdID)

	if len(cd.Messages) == 0 {
		fmt.Fprintln(w, "No messages in this conversation yet.")
		return
	}

	for _, m := range cd.Messages {
		sender := m.SenderName
		if m.FromMe {
			sender = "Tú"
		}

		if color {
			if m.FromMe {
				fmt.Fprintf(w, "\x1b[34m[%s] %s:\x1b[0m %s\n", m.SentAt, sender, m.Text)
			} else {
				fmt.Fprintf(w, "\x1b[32m[%s] %s:\x1b[0m %s\n", m.SentAt, sender, m.Text)
			}
		} else {
			fmt.Fprintf(w, "[%s] %s: %s\n", m.SentAt, sender, m.Text)
		}
	}
}

// ConversationStartResult represents the response when initiating contact on an ad.
type ConversationStartResult struct {
	ConversationID string `json:"conversation_id"`
	AdID           string `json:"ad_id"`
	AdTitle        string `json:"ad_title,omitempty"`
	RecipientName  string `json:"recipient_name,omitempty"`
	MessageID      string `json:"message_id"`
	Text           string `json:"text"`
	SentAt         string `json:"sent_at"`
}

func (r *ConversationStartResult) UnmarshalJSON(data []byte) error {
	type Alias ConversationStartResult
	var aux struct {
		Alias
		SellerName string `json:"seller_name,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*r = ConversationStartResult(aux.Alias)
	if r.RecipientName == "" && aux.SellerName != "" {
		r.RecipientName = aux.SellerName
	}
	return nil
}

// Pretty renders the started conversation in a clean terminal format.
func (r *ConversationStartResult) Pretty(w io.Writer, color bool) {
	if r == nil {
		return
	}
	if color {
		fmt.Fprintf(w, "\x1b[1mConversación iniciada (#%s)\x1b[0m\n", r.ConversationID)
	} else {
		fmt.Fprintf(w, "Conversación iniciada (#%s)\n", r.ConversationID)
	}
	if r.AdTitle != "" {
		fmt.Fprintf(w, "Anuncio: %s (ID: %s)\n", r.AdTitle, r.AdID)
	} else {
		fmt.Fprintf(w, "Anuncio ID: %s\n", r.AdID)
	}
	if r.RecipientName != "" {
		fmt.Fprintf(w, "Destinatario: %s\n", r.RecipientName)
	}
	if r.MessageID != "" {
		fmt.Fprintf(w, "ID Mensaje: %s\n", r.MessageID)
	}
	if r.SentAt != "" {
		fmt.Fprintf(w, "Enviado: %s\n", r.SentAt)
	}
	fmt.Fprintln(w)
	if color {
		fmt.Fprintf(w, "\x1b[34m[Tú]:\x1b[0m %s\n", r.Text)
	} else {
		fmt.Fprintf(w, "[Tú]: %s\n", r.Text)
	}
}
