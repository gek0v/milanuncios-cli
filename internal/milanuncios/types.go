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

// UploadedPhoto represents an image associated with an ad listing.
type UploadedPhoto struct {
	Index   int    `json:"index"`
	Source  string `json:"source,omitempty"`
	MediaID string `json:"media_id,omitempty"`
	URL     string `json:"url"`
}

// UnmarshalJSON supports unmarshaling UploadedPhoto from either a plain string (URL)
// or a JSON object with index, source, media_id, and url.
func (p *UploadedPhoto) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		p.URL = s
		p.Source = s
		return nil
	}
	type Alias UploadedPhoto
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*p = UploadedPhoto(aux)
	return nil
}

// AdItem represents a single ad listing on Milanuncios.
type AdItem struct {
	ID              string          `json:"id"`
	ClientRequestID string          `json:"client_request_id,omitempty"`
	Status          string          `json:"status,omitempty"`
	Title           string          `json:"title"`
	Description     string          `json:"description,omitempty"`
	Price           float64         `json:"price"`
	Currency        string          `json:"currency,omitempty"`
	Negotiable      bool            `json:"negotiable,omitempty"`
	Location        AdLocation      `json:"location,omitempty"`
	Photos          []UploadedPhoto `json:"photos,omitempty"`
	URL             string          `json:"url"`
	Seller          AdSeller        `json:"seller,omitempty"`
	Category        int             `json:"category,omitempty"`
	CategoryID      int             `json:"category_id,omitempty"`
	PublishedAt     string          `json:"published_at,omitempty"`
	ModifiedAt      string          `json:"modified_at,omitempty"`
	CreatedAt       string          `json:"created_at,omitempty"`
	ExpiresAt       string          `json:"expires_at,omitempty"`
}

// AdPublishResult represents the confirmation of a newly published listing.
type AdPublishResult = AdItem

// PhotoURLs returns a slice of photo URLs.
func (a *AdItem) PhotoURLs() []string {
	urls := make([]string, len(a.Photos))
	for i, p := range a.Photos {
		if p.URL != "" {
			urls[i] = p.URL
		} else {
			urls[i] = p.Source
		}
	}
	return urls
}

// Pretty renders a detailed view of an ad or published listing confirmation.
func (a *AdItem) Pretty(w io.Writer, color bool) {
	if a == nil {
		return
	}
	priceStr := fmt.Sprintf("%.0f %s", a.Price, a.Currency)
	if a.Currency == "" {
		priceStr = fmt.Sprintf("%.0f €", a.Price)
	}
	if a.Price == 0 {
		priceStr = "Gratis"
	}

	if a.Status == "published" {
		if color {
			fmt.Fprintf(w, "\x1b[32m✔ Ad published successfully!\x1b[0m (ID: %s)\n", a.ID)
		} else {
			fmt.Fprintf(w, "✔ Ad published successfully! (ID: %s)\n", a.ID)
		}
		if a.ClientRequestID != "" {
			fmt.Fprintf(w, "Client Request ID: %s\n", a.ClientRequestID)
		}
		fmt.Fprintf(w, "Title: %s  (%s)\n", output.StripANSI(a.Title), priceStr)
		if a.URL != "" {
			fmt.Fprintf(w, "URL: %s\n", a.URL)
		}
		catID := a.CategoryID
		if catID == 0 {
			catID = a.Category
		}
		if catID > 0 {
			fmt.Fprintf(w, "Category ID: %d\n", catID)
		}
		createdAt := a.CreatedAt
		if createdAt == "" {
			createdAt = a.PublishedAt
		}
		if createdAt != "" {
			fmt.Fprintf(w, "Created At: %s\n", createdAt)
		}
		if a.ExpiresAt != "" {
			fmt.Fprintf(w, "Expires At: %s\n", a.ExpiresAt)
		}
		if len(a.Photos) > 0 {
			fmt.Fprintf(w, "Photos uploaded (%d):\n", len(a.Photos))
			for i, p := range a.Photos {
				target := p.URL
				if target == "" {
					target = p.Source
				}
				if p.MediaID != "" {
					fmt.Fprintf(w, "  [%d] %s (media: %s)\n", i+1, target, p.MediaID)
				} else {
					fmt.Fprintf(w, "  [%d] %s\n", i+1, target)
				}
			}
		}
		return
	}

	cleanTitle := output.StripANSI(a.Title)
	if color {
		fmt.Fprintf(w, "\x1b[1m%s\x1b[0m  (%s)\n", cleanTitle, priceStr)
	} else {
		fmt.Fprintf(w, "%s  (%s)\n", cleanTitle, priceStr)
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
		fmt.Fprintf(w, "Vendedor: %s%s\n", output.StripANSI(a.Seller.Name), sellerType)
	}
	if a.PublishedAt != "" {
		fmt.Fprintf(w, "Publicado: %s\n", a.PublishedAt)
	}

	if len(a.Photos) > 0 {
		fmt.Fprintf(w, "Fotos (%d):\n", len(a.Photos))
		for i, photo := range a.Photos {
			u := photo.URL
			if u == "" {
				u = photo.Source
			}
			fmt.Fprintf(w, "  [%d] %s\n", i+1, u)
		}
	}

	if a.Description != "" {
		fmt.Fprintf(w, "\n%s\n", strings.TrimSpace(output.StripANSI(a.Description)))
	}
}

// AdUpdateResult represents the outcome of an ad update operation.
type AdUpdateResult struct {
	ID            string   `json:"id"`
	UpdatedFields []string `json:"updated_fields"`
	PreviousPrice float64  `json:"previous_price,omitempty"`
	NewPrice      float64  `json:"new_price,omitempty"`
	UpdatedAt     string   `json:"updated_at"`
}

// Pretty prints the ad update result in human-readable terminal format.
func (r *AdUpdateResult) Pretty(w io.Writer, color bool) {
	if r == nil {
		return
	}
	if color {
		fmt.Fprintf(w, "\x1b[32m✔ Ad updated successfully (#%s)\x1b[0m\n", r.ID)
	} else {
		fmt.Fprintf(w, "✔ Ad updated successfully (#%s)\n", r.ID)
	}
	if len(r.UpdatedFields) > 0 {
		fmt.Fprintf(w, "Updated fields: %s\n", strings.Join(r.UpdatedFields, ", "))
	}
	if r.PreviousPrice > 0 || r.NewPrice > 0 {
		fmt.Fprintf(w, "Price change: %.2f € -> %.2f €\n", r.PreviousPrice, r.NewPrice)
	}
	if r.UpdatedAt != "" {
		fmt.Fprintf(w, "Updated at: %s\n", r.UpdatedAt)
	}
}

// AdDeleteResult represents the outcome of an ad delete or withdrawal operation.
type AdDeleteResult struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message,omitempty"`
}

// Pretty prints the ad delete result in human-readable terminal format.
func (r *AdDeleteResult) Pretty(w io.Writer, color bool) {
	if r == nil {
		return
	}
	if color {
		fmt.Fprintf(w, "\x1b[32m✔ Ad deleted/withdrawn successfully (#%s)\x1b[0m\n", r.ID)
	} else {
		fmt.Fprintf(w, "✔ Ad deleted/withdrawn successfully (#%s)\n", r.ID)
	}
	fmt.Fprintf(w, "ID: %s\n", r.ID)
	fmt.Fprintf(w, "Status: %s\n", r.Status)
	fmt.Fprintf(w, "Reason: %s\n", r.Reason)
	if r.Message != "" {
		fmt.Fprintf(w, "Message: %s\n", r.Message)
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
			output.Truncate(output.StripANSI(it.Title), 40),
			priceStr,
			output.Truncate(loc, 24),
			output.Truncate(output.StripANSI(sellerStr), 20),
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
			output.Truncate(output.StripANSI(c.UserName), 20),
			output.Truncate(output.StripANSI(c.AdTitle), 26),
			priceStr,
			output.Truncate(output.StripANSI(c.LastMessage), 30),
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
	cleanUser := output.StripANSI(c.UserName)
	if color {
		fmt.Fprintf(w, "\x1b[1mConversación #%s con %s\x1b[0m\n", c.ID, cleanUser)
	} else {
		fmt.Fprintf(w, "Conversación #%s con %s\n", c.ID, cleanUser)
	}
	fmt.Fprintf(w, "Anuncio: %s (%.0f €) [ID: %s]\n\n", output.StripANSI(c.AdTitle), c.AdPrice, c.AdID)

	if len(cd.Messages) == 0 {
		fmt.Fprintln(w, "No messages in this conversation yet.")
		return
	}

	for _, m := range cd.Messages {
		sender := output.StripANSI(m.SenderName)
		if m.FromMe {
			sender = "Tú"
		}
		text := output.StripANSI(m.Text)

		if color {
			if m.FromMe {
				fmt.Fprintf(w, "\x1b[34m[%s] %s:\x1b[0m %s\n", m.SentAt, sender, text)
			} else {
				fmt.Fprintf(w, "\x1b[32m[%s] %s:\x1b[0m %s\n", m.SentAt, sender, text)
			}
		} else {
			fmt.Fprintf(w, "[%s] %s: %s\n", m.SentAt, sender, text)
		}
	}
}

// ChatMessageReceivedEvent represents an incoming chat message event.
type ChatMessageReceivedEvent struct {
	EventType      string  `json:"event_type"`
	ConversationID string  `json:"conversation_id"`
	AdID           string  `json:"ad_id"`
	AdTitle        string  `json:"ad_title,omitempty"`
	AdPrice        float64 `json:"ad_price,omitempty"`
	SenderName     string  `json:"sender_name"`
	Text           string  `json:"text"`
	SentAt         string  `json:"sent_at"`
}

// Pretty renders the chat event for human terminals.
func (e ChatMessageReceivedEvent) Pretty(w io.Writer, color bool) {
	cleanSender := output.StripANSI(e.SenderName)
	cleanText := output.StripANSI(e.Text)
	if color {
		fmt.Fprintf(w, "\x1b[1;32m[MENSAJE ENTRANTE]\x1b[0m \x1b[1m%s\x1b[0m en conversación %s (anuncio %s)\n", cleanSender, e.ConversationID, e.AdID)
	} else {
		fmt.Fprintf(w, "[MENSAJE ENTRANTE] %s en conversación %s (anuncio %s)\n", cleanSender, e.ConversationID, e.AdID)
	}
	fmt.Fprintf(w, "Fecha: %s\n", e.SentAt)
	fmt.Fprintf(w, "Texto: %s\n\n", cleanText)
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

// AdSpec defines the full declarative specification for an ad listing.
type AdSpec struct {
	ClientRequestID string                 `json:"client_request_id,omitempty"`
	CategoryID      int                    `json:"category_id"`
	Title           string                 `json:"title"`
	Description     string                 `json:"description"`
	Price           float64                `json:"price"`
	Negotiable      bool                   `json:"negotiable,omitempty"`
	Location        AdLocationSpec         `json:"location"`
	Contact         AdContactSpec          `json:"contact,omitempty"`
	Shipping        AdShippingSpec         `json:"shipping,omitempty"`
	Photos          []string               `json:"photos,omitempty"`
	Attributes      map[string]interface{} `json:"attributes,omitempty"`
}

type AdLocationSpec struct {
	Province     string `json:"province"`
	Municipality string `json:"municipality,omitempty"`
	PostalCode   string `json:"postal_code,omitempty"`
}

type AdContactSpec struct {
	AllowPhone bool   `json:"allow_phone"`
	AllowChat  bool   `json:"allow_chat"`
	Phone      string `json:"phone,omitempty"`
	Email      string `json:"email,omitempty"`
}

type AdShippingSpec struct {
	Allowed  bool    `json:"allowed"`
	WeightKG float64 `json:"weight_kg,omitempty"`
}

// ValidationReport represents the diagnostics returned in --dry-run and validation failures.
type ValidationReport struct {
	Valid                    bool              `json:"valid"`
	CategoryID               int               `json:"category_id,omitempty"`
	PhotosDetected           int               `json:"photos_detected,omitempty"`
	EstimatedUploadSizeBytes int64             `json:"estimated_upload_size_bytes,omitempty"`
	Errors                   []ValidationError `json:"errors,omitempty"`
	Warnings                 []string          `json:"warnings,omitempty"`
	Message                  string            `json:"message,omitempty"`
}

type ValidationError struct {
	Field         string      `json:"field"`
	Code          string      `json:"code"`
	Message       string      `json:"message"`
	Actual        interface{} `json:"actual,omitempty"`
	Expected      string      `json:"expected,omitempty"`
	AllowedValues []string    `json:"allowed_values,omitempty"`
	Suggestion    string      `json:"suggestion,omitempty"`
}

// Pretty renders the validation report in a clean human-readable terminal format.
func (r *ValidationReport) Pretty(w io.Writer, color bool) {
	if r == nil {
		return
	}

	if r.Valid {
		if color {
			fmt.Fprintln(w, "\x1b[32m✔ Validation successful!\x1b[0m")
		} else {
			fmt.Fprintln(w, "✔ Validation successful!")
		}
		if r.CategoryID > 0 {
			fmt.Fprintf(w, "Category ID: %d\n", r.CategoryID)
		}
		if r.PhotosDetected > 0 {
			sizeStr := formatBytes(r.EstimatedUploadSizeBytes)
			fmt.Fprintf(w, "Photos detected: %d (estimated upload size: %s)\n", r.PhotosDetected, sizeStr)
		}
		if r.Message != "" {
			fmt.Fprintf(w, "Message: %s\n", r.Message)
		}
		if len(r.Warnings) > 0 {
			fmt.Fprintln(w, "\nWarnings:")
			for _, warn := range r.Warnings {
				if color {
					fmt.Fprintf(w, "  \x1b[33m⚠ %s\x1b[0m\n", warn)
				} else {
					fmt.Fprintf(w, "  ⚠ %s\n", warn)
				}
			}
		}
		return
	}

	if color {
		fmt.Fprintf(w, "\x1b[31m✖ Validation failed with %d error(s):\x1b[0m\n\n", len(r.Errors))
	} else {
		fmt.Fprintf(w, "✖ Validation failed with %d error(s):\n\n", len(r.Errors))
	}

	for _, err := range r.Errors {
		if color {
			fmt.Fprintf(w, "  \x1b[1m[%s]\x1b[0m \x1b[31m%s\x1b[0m: %s\n", err.Field, err.Code, err.Message)
		} else {
			fmt.Fprintf(w, "  [%s] %s: %s\n", err.Field, err.Code, err.Message)
		}
		if err.Actual != nil {
			fmt.Fprintf(w, "    Actual:     %v\n", err.Actual)
		}
		if err.Expected != "" {
			fmt.Fprintf(w, "    Expected:   %s\n", err.Expected)
		}
		if len(err.AllowedValues) > 0 {
			fmt.Fprintf(w, "    Allowed:    %s\n", strings.Join(err.AllowedValues, ", "))
		}
		if err.Suggestion != "" {
			if color {
				fmt.Fprintf(w, "    \x1b[33mSuggestion: %s\x1b[0m\n", err.Suggestion)
			} else {
				fmt.Fprintf(w, "    Suggestion: %s\n", err.Suggestion)
			}
		}
		fmt.Fprintln(w)
	}

	if len(r.Warnings) > 0 {
		fmt.Fprintln(w, "Warnings:")
		for _, warn := range r.Warnings {
			if color {
				fmt.Fprintf(w, "  \x1b[33m⚠ %s\x1b[0m\n", warn)
			} else {
				fmt.Fprintf(w, "  ⚠ %s\n", warn)
			}
		}
	}
}

func formatBytes(b int64) string {
	if b <= 0 {
		return "0 B"
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// CategoryJSONSchema represents a Draft-07 JSON Schema publication contract.
type CategoryJSONSchema struct {
	Schema       string                 `json:"$schema"`
	Title        string                 `json:"title"`
	CategoryID   int                    `json:"category_id"`
	CategoryName string                 `json:"category_name,omitempty"`
	Type         string                 `json:"type"`
	Required     []string               `json:"required"`
	Properties   map[string]interface{} `json:"properties"`
	Filters      []FilterDefinition     `json:"-"`
}

// Pretty prints the JSON schema contract in human-readable tabular form.
func (s *CategoryJSONSchema) Pretty(w io.Writer, color bool) {
	if s == nil {
		return
	}
	name := s.CategoryName
	if name == "" {
		name = fmt.Sprintf("Category %d", s.CategoryID)
	}
	if color {
		fmt.Fprintf(w, "\x1b[1mCategory Schema: %s (ID: %d)\x1b[0m\n", name, s.CategoryID)
	} else {
		fmt.Fprintf(w, "Category Schema: %s (ID: %d)\n", name, s.CategoryID)
	}
	fmt.Fprintln(w, "Draft-07 JSON Schema Specification")
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Required Core Fields:")
	reqHeader := []string{"FIELD", "TYPE", "CONSTRAINTS", "REQUIRED"}
	reqRows := [][]string{
		{"category_id", "integer", fmt.Sprintf("Must match %d", s.CategoryID), "YES"},
		{"title", "string", "10-100 characters", "YES"},
		{"description", "string", "20-5000 characters", "YES"},
		{"price", "number", ">= 1.0 €", "YES"},
		{"location.province", "string", "Spanish province name", "YES"},
		{"photos", "array[string]", "Local file path, HTTP(S) URL, or Base64", "no"},
		{"contact", "object", "allow_chat (bool), allow_phone (bool), phone, email", "no"},
		{"shipping", "object", "allowed (bool), weight_kg (number)", "no"},
	}
	output.Table(w, color, reqHeader, reqRows)

	if len(s.Filters) > 0 {
		fmt.Fprintf(w, "\nCategory-Specific Dynamic Attributes (%d):\n", len(s.Filters))
		attrHeader := []string{"KEY", "LABEL", "TYPE", "OPTIONS / ENUMS"}
		var attrRows [][]string
		for _, f := range s.Filters {
			opts := strings.Join(f.Options, ", ")
			if opts == "" {
				opts = "-"
			}
			attrRows = append(attrRows, []string{
				f.Key,
				f.Label,
				f.Type,
				output.Truncate(opts, 45),
			})
		}
		output.Table(w, color, attrHeader, attrRows)
	} else {
		fmt.Fprintln(w, "\nNo category-specific attributes required for this category.")
	}

	fmt.Fprintln(w, "\nValidate your ad specification with: milanuncios ad create --from-file <spec.json> --dry-run")
}
