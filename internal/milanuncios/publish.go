package milanuncios

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/output"
)

// GetCategorySchema retrieves category filters and builds a JSON Schema Draft-07 specification.
func (c *Client) GetCategorySchema(ctx context.Context, categoryID int) (*CategoryJSONSchema, error) {
	if categoryID <= 0 {
		return nil, output.Usagef("category ID must be greater than 0")
	}

	filtersResult, err := c.GetCategoryFilters(ctx, categoryID)
	if err != nil {
		return nil, err
	}

	catName := filtersResult.CategoryName
	if catName == "" {
		catName = fmt.Sprintf("Category %d", categoryID)
	}

	attrProperties := make(map[string]interface{})
	for _, f := range filtersResult.Filters {
		prop := make(map[string]interface{})
		if len(f.Options) > 0 {
			prop["type"] = "string"
			prop["enum"] = f.Options
		} else {
			switch f.Type {
			case "number":
				prop["type"] = "number"
			case "boolean":
				prop["type"] = "boolean"
			default:
				prop["type"] = "string"
			}
		}
		if f.Label != "" {
			prop["description"] = f.Label
		}
		attrProperties[f.Key] = prop
	}

	properties := map[string]interface{}{
		"category_id": map[string]interface{}{
			"type":        "integer",
			"description": "Category ID",
		},
		"client_request_id": map[string]interface{}{
			"type":        "string",
			"description": "Optional UUIDv4 for request idempotency",
		},
		"title": map[string]interface{}{
			"type":        "string",
			"minLength":   10,
			"maxLength":   100,
			"description": "Ad title (10-100 characters)",
		},
		"description": map[string]interface{}{
			"type":        "string",
			"minLength":   20,
			"maxLength":   5000,
			"description": "Detailed ad description (20-5000 characters)",
		},
		"price": map[string]interface{}{
			"type":        "number",
			"minimum":     1,
			"maximum":     1000000,
			"description": "Price in euros (€). Minimum 1.",
		},
		"negotiable": map[string]interface{}{
			"type":        "boolean",
			"default":     false,
			"description": "Indicates whether the price is negotiable",
		},
		"location": map[string]interface{}{
			"type":     "object",
			"required": []string{"province"},
			"properties": map[string]interface{}{
				"province": map[string]interface{}{
					"type":        "string",
					"description": "Spanish province name (e.g. Madrid, Barcelona)",
				},
				"municipality": map[string]interface{}{
					"type":        "string",
					"description": "Municipality or town",
				},
				"postal_code": map[string]interface{}{
					"type":        "string",
					"pattern":     "^[0-9]{5}$",
					"description": "Spanish 5-digit postal code",
				},
			},
		},
		"photos": map[string]interface{}{
			"type": "array",
			"items": map[string]interface{}{
				"type":        "string",
				"description": "Local file path, remote HTTP/HTTPS URL, or Base64 data URI",
			},
		},
		"contact": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"allow_phone": map[string]interface{}{"type": "boolean", "default": false},
				"allow_chat":  map[string]interface{}{"type": "boolean", "default": true},
				"phone":       map[string]interface{}{"type": "string"},
				"email":       map[string]interface{}{"type": "string"},
			},
		},
		"shipping": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"allowed":   map[string]interface{}{"type": "boolean", "default": false},
				"weight_kg": map[string]interface{}{"type": "number", "minimum": 0.1, "maximum": 30.0},
			},
		},
		"attributes": map[string]interface{}{
			"type":       "object",
			"properties": attrProperties,
		},
	}

	schema := &CategoryJSONSchema{
		Schema:       "http://json-schema.org/draft-07/schema#",
		Title:        fmt.Sprintf("MilanunciosAdSpec_Category_%d", categoryID),
		CategoryID:   categoryID,
		CategoryName: catName,
		Type:         "object",
		Required:     []string{"category_id", "title", "description", "price", "location"},
		Properties:   properties,
		Filters:      filtersResult.Filters,
	}

	return schema, nil
}

// ValidateAdSpec checks an ad specification against core Milanuncios business rules
// and category-specific dynamic filters.
func (c *Client) ValidateAdSpec(ctx context.Context, spec *AdSpec) (*ValidationReport, error) {
	if spec == nil {
		return &ValidationReport{
			Valid:   false,
			Message: "AdSpec is nil",
			Errors: []ValidationError{
				{
					Field:      "spec",
					Code:       "E_VAL_MISSING_REQUIRED",
					Message:    "Ad specification cannot be null",
					Suggestion: "Provide a valid AdSpec JSON object",
				},
			},
		}, nil
	}

	report := &ValidationReport{
		Valid:      true,
		CategoryID: spec.CategoryID,
		Errors:     make([]ValidationError, 0),
		Warnings:   make([]string, 0),
	}

	// 1. Core Validations
	// Category ID
	if spec.CategoryID <= 0 {
		report.Errors = append(report.Errors, ValidationError{
			Field:      "category_id",
			Code:       "E_VAL_MISSING_REQUIRED",
			Message:    "Category ID is required and must be greater than 0",
			Actual:     spec.CategoryID,
			Expected:   "> 0",
			Suggestion: "Specify a valid category ID (see 'milanuncios category list')",
		})
	}

	// Title: 10-100 characters
	titleLen := len(strings.TrimSpace(spec.Title))
	if titleLen < 10 {
		report.Errors = append(report.Errors, ValidationError{
			Field:      "title",
			Code:       "E_VAL_STRING_TOO_SHORT",
			Message:    "Title must be at least 10 characters",
			Actual:     spec.Title,
			Expected:   ">= 10 characters",
			Suggestion: "Extend the title with more descriptive keywords",
		})
	} else if len(spec.Title) > 100 {
		report.Errors = append(report.Errors, ValidationError{
			Field:      "title",
			Code:       "E_VAL_STRING_TOO_LONG",
			Message:    "Title must not exceed 100 characters",
			Actual:     spec.Title,
			Expected:   "<= 100 characters",
			Suggestion: "Shorten the title to 100 characters or fewer",
		})
	}

	// Description: 20-5000 characters
	descLen := len(strings.TrimSpace(spec.Description))
	if descLen < 20 {
		report.Errors = append(report.Errors, ValidationError{
			Field:      "description",
			Code:       "E_VAL_STRING_TOO_SHORT",
			Message:    "Description must be at least 20 characters",
			Actual:     spec.Description,
			Expected:   ">= 20 characters",
			Suggestion: "Add more details about the item's condition and features",
		})
	} else if len(spec.Description) > 5000 {
		report.Errors = append(report.Errors, ValidationError{
			Field:      "description",
			Code:       "E_VAL_STRING_TOO_LONG",
			Message:    "Description must not exceed 5000 characters",
			Actual:     spec.Description,
			Expected:   "<= 5000 characters",
			Suggestion: "Shorten the description to 5000 characters or fewer",
		})
	}

	// Description warning for short descriptions
	if descLen >= 20 && descLen < 200 {
		report.Warnings = append(report.Warnings, fmt.Sprintf("Description is short (%d characters). Ads with descriptions over 200 characters receive 40%% more views.", descLen))
	}

	// Price: >= 1
	if spec.Price < 1 {
		report.Errors = append(report.Errors, ValidationError{
			Field:      "price",
			Code:       "E_VAL_NUMBER_TOO_LOW",
			Message:    "Price must be greater than or equal to 1",
			Actual:     spec.Price,
			Expected:   ">= 1.0",
			Suggestion: "Set a valid price in Euros",
		})
	}

	// Location: Province required
	if strings.TrimSpace(spec.Location.Province) == "" {
		report.Errors = append(report.Errors, ValidationError{
			Field:      "location.province",
			Code:       "E_VAL_MISSING_REQUIRED",
			Message:    "Location province is required",
			Actual:     spec.Location.Province,
			Expected:   "Spanish province name (e.g. Madrid, Barcelona)",
			Suggestion: "Provide a valid Spanish province name in location.province",
		})
	}

	// Photos
	report.PhotosDetected = len(spec.Photos)
	if report.PhotosDetected == 0 {
		report.Warnings = append(report.Warnings, "No photos provided. Listings with photos receive up to 5x more inquiries.")
	} else {
		for i, photo := range spec.Photos {
			trimmed := strings.TrimSpace(photo)
			if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
				report.EstimatedUploadSizeBytes += 1024 * 1024
			} else if strings.HasPrefix(trimmed, "data:") {
				commaIdx := strings.Index(trimmed, ",")
				if commaIdx != -1 {
					b64Data := trimmed[commaIdx+1:]
					report.EstimatedUploadSizeBytes += int64((len(b64Data) * 3) / 4)
				}
			} else if strings.HasPrefix(trimmed, "media://") {
				// Already uploaded media
			} else {
				fi, err := os.Stat(trimmed)
				if err != nil {
					report.Errors = append(report.Errors, ValidationError{
						Field:      fmt.Sprintf("photos[%d]", i),
						Code:       "E_VAL_FILE_NOT_FOUND",
						Message:    fmt.Sprintf("Local photo file %q does not exist or is not readable", trimmed),
						Actual:     trimmed,
						Suggestion: "Verify file path or provide a public image URL / base64 string",
					})
				} else if fi.IsDir() {
					report.Errors = append(report.Errors, ValidationError{
						Field:      fmt.Sprintf("photos[%d]", i),
						Code:       "E_VAL_FILE_NOT_FOUND",
						Message:    fmt.Sprintf("Local photo file %q is a directory", trimmed),
						Actual:     trimmed,
						Suggestion: "Provide a path to an image file",
					})
				} else {
					report.EstimatedUploadSizeBytes += fi.Size()
				}
			}
		}
	}

	// 2. Category Attribute Validations
	if spec.CategoryID > 0 {
		filtersResult, err := c.GetCategoryFilters(ctx, spec.CategoryID)
		if err != nil {
			var de *output.DomainError
			if errors.As(err, &de) && de.Kind == output.KindNotFound {
				report.Errors = append(report.Errors, ValidationError{
					Field:      "category_id",
					Code:       "E_VAL_CATEGORY_NOT_FOUND",
					Message:    fmt.Sprintf("Category %d not found", spec.CategoryID),
					Actual:     spec.CategoryID,
					Expected:   "Valid Milanuncios category ID",
					Suggestion: "Check available categories with 'milanuncios category list'",
				})
			} else {
				return nil, err
			}
		} else if len(spec.Attributes) > 0 {
			filterMap := make(map[string]FilterDefinition)
			for _, f := range filtersResult.Filters {
				filterMap[f.Key] = f
			}

			// Sort attribute keys for deterministic output
			attrKeys := make([]string, 0, len(spec.Attributes))
			for k := range spec.Attributes {
				attrKeys = append(attrKeys, k)
			}
			sort.Strings(attrKeys)

			for _, key := range attrKeys {
				val := spec.Attributes[key]
				filterDef, found := filterMap[key]
				if !found {
					continue
				}

				if len(filterDef.Options) > 0 {
					valStr := attrValueToString(val)
					match := false
					for _, opt := range filterDef.Options {
						if strings.EqualFold(strings.TrimSpace(opt), strings.TrimSpace(valStr)) {
							match = true
							break
						}
					}

					if !match {
						suggestion := findSuggestion(valStr, filterDef.Options)
						report.Errors = append(report.Errors, ValidationError{
							Field:         fmt.Sprintf("attributes.%s", key),
							Code:          "E_VAL_INVALID_ENUM",
							Message:       fmt.Sprintf("Value '%v' is not allowed for attribute '%s'", val, key),
							Actual:        val,
							Expected:      fmt.Sprintf("One of: ['%s']", strings.Join(filterDef.Options, "', '")),
							AllowedValues: filterDef.Options,
							Suggestion:    suggestion,
						})
					}
				}
			}
		}
	}

	if len(report.Errors) > 0 {
		report.Valid = false
		report.Message = fmt.Sprintf("AdSpec validation failed with %d error(s)", len(report.Errors))
	} else {
		report.Valid = true
		report.Message = "Dry-run validation successful. AdSpec is compliant and ready to be published."
	}

	return report, nil
}

func attrValueToString(val interface{}) string {
	switch v := val.(type) {
	case float64:
		if v == math.Trunc(v) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		if float64(v) == math.Trunc(float64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

var knownAliases = map[string]string{
	"gasoil":     "diesel",
	"gasóleo":    "diesel",
	"gasoleo":    "diesel",
	"diésel":     "diesel",
	"nafta":      "gasolina",
	"bencina":    "gasolina",
	"automatica": "automatico",
	"automática": "automatico",
	"manuale":    "manual",
	"m":          "manual",
	"a":          "automatico",
}

func findSuggestion(actual string, allowed []string) string {
	lowerActual := strings.ToLower(strings.TrimSpace(actual))

	// 1. Check direct alias mapping
	if alias, ok := knownAliases[lowerActual]; ok {
		for _, opt := range allowed {
			if strings.EqualFold(opt, alias) {
				return fmt.Sprintf("Change '%s' to '%s'", actual, opt)
			}
		}
	}

	// 2. Check Levenshtein distance
	bestDist := 999
	bestMatch := ""
	for _, opt := range allowed {
		lowerOpt := strings.ToLower(opt)
		dist := levenshteinDistance(lowerActual, lowerOpt)
		if dist < bestDist {
			bestDist = dist
			bestMatch = opt
		}
	}

	if bestDist <= 3 && bestMatch != "" {
		return fmt.Sprintf("Change '%s' to '%s'", actual, bestMatch)
	}

	return fmt.Sprintf("Select one of the allowed values: %s", strings.Join(allowed, ", "))
}

func levenshteinDistance(s1, s2 string) int {
	r1, r2 := []rune(s1), []rune(s2)
	len1, len2 := len(r1), len(r2)

	column := make([]int, len1+1)
	for y := 1; y <= len1; y++ {
		column[y] = y
	}

	for x := 1; x <= len2; x++ {
		column[0] = x
		lastkey := x - 1
		for y := 1; y <= len1; y++ {
			oldkey := column[y]
			cost := 0
			if r1[y-1] != r2[x-1] {
				cost = 1
			}
			column[y] = min(min(column[y]+1, column[y-1]+1), lastkey+cost)
			lastkey = oldkey
		}
	}
	return column[len1]
}

// UploadPhoto ingests a single photo reference from local file, remote HTTP/HTTPS URL,
// base64 data URI, or existing media ID, streams it as multipart/form-data to POST /api/v1/media/upload,
// and returns the uploaded photo metadata.
func (c *Client) UploadPhoto(ctx context.Context, photoRef string, index int) (*UploadedPhoto, error) {
	trimmed := strings.TrimSpace(photoRef)
	if trimmed == "" {
		return nil, output.Usagef("photo reference cannot be empty")
	}

	// 1. Existing media ID reference: media://...
	if strings.HasPrefix(trimmed, "media://") {
		mediaID := strings.TrimPrefix(trimmed, "media://")
		return &UploadedPhoto{
			Index:   index,
			Source:  trimmed,
			MediaID: mediaID,
			URL:     trimmed,
		}, nil
	}

	var (
		data     []byte
		mimeType string
		filename string
	)

	// 2. Remote HTTP/HTTPS URL
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, trimmed, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request for remote photo %q: %w", trimmed, err)
		}
		req.Header.Set("User-Agent", c.currentUA())
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, c.wrapNetError("GET "+trimmed, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, output.Usagef("remote photo %q returned HTTP %d", trimmed, resp.StatusCode)
		}

		data, err = io.ReadAll(io.LimitReader(resp.Body, 15*1024*1024+1))
		if err != nil {
			return nil, fmt.Errorf("reading remote photo %q: %w", trimmed, err)
		}
		if len(data) > 15*1024*1024 {
			return nil, output.Usagef("remote photo %q exceeds 15 MB limit", trimmed)
		}

		ct := resp.Header.Get("Content-Type")
		mimeType = strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
		if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/webp" {
			mimeType = detectImageMime(data, trimmed)
		}
		if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/webp" {
			return nil, output.Usagef("unsupported remote image format %q (detected: %s)", trimmed, mimeType)
		}
		filename = extractFilenameFromURL(trimmed, mimeType)

	} else if strings.HasPrefix(trimmed, "data:") {
		// 3. Base64 Data URI
		commaIdx := strings.Index(trimmed, ",")
		if commaIdx == -1 {
			return nil, output.Usagef("invalid data URI format: missing comma separator")
		}
		header := trimmed[:commaIdx]
		b64Part := trimmed[commaIdx+1:]

		mimeType = "image/jpeg"
		headerContent := strings.TrimPrefix(header, "data:")
		if scIdx := strings.Index(headerContent, ";"); scIdx != -1 {
			parsedMime := strings.TrimSpace(headerContent[:scIdx])
			if parsedMime != "" {
				mimeType = parsedMime
			}
		}

		cleanedB64 := strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, b64Part)

		var err error
		data, err = base64.StdEncoding.DecodeString(cleanedB64)
		if err != nil {
			data, err = base64.RawStdEncoding.DecodeString(cleanedB64)
			if err != nil {
				return nil, output.Usagef("failed to decode base64 photo data: %v", err)
			}
		}
		if len(data) > 15*1024*1024 {
			return nil, output.Usagef("base64 photo exceeds 15 MB limit")
		}

		if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/webp" {
			mimeType = detectImageMime(data, "")
		}
		if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/webp" {
			return nil, output.Usagef("unsupported data URI image format %q", mimeType)
		}
		ext := mimeToExt(mimeType)
		filename = fmt.Sprintf("photo_%d%s", index, ext)

	} else {
		// 4. Local filesystem path
		fi, err := os.Stat(trimmed)
		if err != nil {
			return nil, output.Usagef("local photo file %q does not exist: %v", trimmed, err)
		}
		if fi.IsDir() {
			return nil, output.Usagef("local photo path %q is a directory", trimmed)
		}
		if fi.Size() > 15*1024*1024 {
			return nil, output.Usagef("photo %q exceeds 15 MB limit (%d bytes)", trimmed, fi.Size())
		}
		data, err = os.ReadFile(trimmed)
		if err != nil {
			return nil, fmt.Errorf("reading local photo file %q: %w", trimmed, err)
		}
		mimeType = detectImageMime(data, trimmed)
		if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/webp" {
			return nil, output.Usagef("unsupported local image format %q (%s). Milanuncios accepts JPEG, PNG, or WebP", trimmed, mimeType)
		}
		filename = filepath.Base(trimmed)
	}

	// Build multipart/form-data body
	bodyBuf := &bytes.Buffer{}
	mpWriter := multipart.NewWriter(bodyBuf)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
	h.Set("Content-Type", mimeType)
	part, err := mpWriter.CreatePart(h)
	if err != nil {
		return nil, fmt.Errorf("creating multipart form part: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return nil, fmt.Errorf("writing image data to multipart stream: %w", err)
	}
	if err := mpWriter.Close(); err != nil {
		return nil, fmt.Errorf("closing multipart writer: %w", err)
	}

	uploadURL := strings.TrimRight(c.BaseURL, "/") + "/api/v1/media/upload"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bodyBuf)
	if err != nil {
		return nil, fmt.Errorf("creating media upload request: %w", err)
	}
	req.Header.Set("User-Agent", c.currentUA())
	req.Header.Set("Accept", "application/json")
	c.setBrowserHeaders(req.Header)
	req.Header.Set("Content-Type", mpWriter.FormDataContentType())
	if c.SessionToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.SessionToken)
		req.Header.Set("Cookie", "token="+c.SessionToken)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, c.wrapNetError("POST /api/v1/media/upload", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, c.wrapNetError("POST /api/v1/media/upload", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, &output.DomainError{
			Kind:     output.KindAuth,
			Status:   resp.StatusCode,
			Endpoint: "POST /api/v1/media/upload",
			Msg:      "media upload requires authentication. Run `milanuncios auth login`",
		}
	}
	if isBotStatus(resp.StatusCode, raw) || isWAFChallenge(resp.Header.Get("Content-Type"), raw) {
		if c.OnBlock != nil {
			c.OnBlock(ctx)
		}
		return nil, &output.DomainError{
			Kind:      output.KindBlocked,
			Status:    resp.StatusCode,
			Endpoint:  "POST /api/v1/media/upload",
			Body:      c.snippet("POST /api/v1/media/upload", raw),
			Retryable: true,
			Msg:       "milanuncios blocked or rate-limited the request. Wait 15 minutes before retrying",
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &output.DomainError{
			Kind:     output.KindGeneric,
			Status:   resp.StatusCode,
			Endpoint: "POST /api/v1/media/upload",
			Msg:      fmt.Sprintf("unexpected HTTP %d from media upload: %s", resp.StatusCode, c.snippet("POST /api/v1/media/upload", raw)),
		}
	}

	var uploadResp struct {
		MediaID string `json:"media_id"`
		URL     string `json:"url"`
		ID      string `json:"id,omitempty"`
	}
	if err := json.Unmarshal(raw, &uploadResp); err != nil {
		if isWAFChallenge(resp.Header.Get("Content-Type"), raw) {
			if c.OnBlock != nil {
				c.OnBlock(ctx)
			}
			return nil, &output.DomainError{
				Kind:      output.KindBlocked,
				Status:    resp.StatusCode,
				Endpoint:  "POST /api/v1/media/upload",
				Body:      c.snippet("POST /api/v1/media/upload", raw),
				Retryable: true,
				Msg:       "milanuncios blocked or rate-limited the request. Wait 15 minutes before retrying",
			}
		}
		return nil, &output.DomainError{
			Kind:     output.KindAPIChanged,
			Status:   resp.StatusCode,
			Endpoint: "POST /api/v1/media/upload",
			Body:     "json decode: " + err.Error() + "; body: " + c.snippet("POST /api/v1/media/upload", raw),
			Msg:      "milanuncios returned an unexpected response structure from media upload",
		}
	}

	mediaID := uploadResp.MediaID
	if mediaID == "" {
		mediaID = uploadResp.ID
	}
	cdnURL := uploadResp.URL
	if cdnURL == "" {
		cdnURL = fmt.Sprintf("https://img.milanuncios.com/%s", mediaID)
	}

	return &UploadedPhoto{
		Index:   index,
		Source:  photoRef,
		MediaID: mediaID,
		URL:     cdnURL,
	}, nil
}

// UploadPhotos uploads multiple photos sequentially and returns their uploaded metadata.
func (c *Client) UploadPhotos(ctx context.Context, photoRefs []string) ([]UploadedPhoto, error) {
	uploaded := make([]UploadedPhoto, 0, len(photoRefs))
	for i, ref := range photoRefs {
		photo, err := c.UploadPhoto(ctx, ref, i)
		if err != nil {
			return nil, fmt.Errorf("uploading photo [%d] %q: %w", i, ref, err)
		}
		uploaded = append(uploaded, *photo)
	}
	return uploaded, nil
}

// CreateAd validates the ad specification, uploads all photos in sequence,
// and publishes the listing to the Milanuncios API.
func (c *Client) CreateAd(ctx context.Context, spec *AdSpec) (*AdItem, error) {
	if spec == nil {
		return nil, output.Usagef("ad spec cannot be nil")
	}

	if c.SessionToken == "" {
		return nil, &output.DomainError{
			Kind:     output.KindAuth,
			Endpoint: "POST /api/v1/ads",
			Msg:      "publishing an ad requires an authenticated profile. Run `milanuncios auth login`",
		}
	}

	report, err := c.ValidateAdSpec(ctx, spec)
	if err != nil {
		return nil, err
	}
	if !report.Valid {
		return nil, output.Usagef("ad validation failed with %d error(s)", len(report.Errors))
	}

	var uploadedPhotos []UploadedPhoto
	if len(spec.Photos) > 0 {
		var err error
		uploadedPhotos, err = c.UploadPhotos(ctx, spec.Photos)
		if err != nil {
			return nil, err
		}
	}

	payload := map[string]interface{}{
		"category_id": spec.CategoryID,
		"title":       spec.Title,
		"description": spec.Description,
		"price":       spec.Price,
		"negotiable":  spec.Negotiable,
		"location":    spec.Location,
	}
	if spec.ClientRequestID != "" {
		payload["client_request_id"] = spec.ClientRequestID
		ctx = WithClientRequestID(ctx, spec.ClientRequestID)
	}
	if spec.Contact.AllowChat || spec.Contact.AllowPhone || spec.Contact.Phone != "" || spec.Contact.Email != "" {
		payload["contact"] = spec.Contact
	}
	if spec.Shipping.Allowed || spec.Shipping.WeightKG > 0 {
		payload["shipping"] = spec.Shipping
	}
	if len(spec.Attributes) > 0 {
		payload["attributes"] = spec.Attributes
	}
	if len(uploadedPhotos) > 0 {
		var mediaIDs []string
		for _, up := range uploadedPhotos {
			mediaIDs = append(mediaIDs, up.MediaID)
		}
		payload["photos"] = mediaIDs
		payload["media_ids"] = mediaIDs
	}

	var created AdItem
	if err := c.doRequest(ctx, http.MethodPost, "/api/v1/ads", nil, payload, true, &created); err != nil {
		var de *output.DomainError
		if errors.As(err, &de) {
			if de.Status == http.StatusBadRequest || de.Status == http.StatusUnprocessableEntity {
				return nil, output.Usagef("ad creation failed (%d): %s", de.Status, de.Msg)
			}
			if de.Status == http.StatusNotFound || de.Kind == output.KindNotFound {
				return nil, output.NotFoundf("category or resource not found: %d", spec.CategoryID)
			}
		}
		return nil, err
	}

	if created.Title == "" {
		created.Title = spec.Title
	}
	if created.Price == 0 {
		created.Price = spec.Price
	}
	if created.CategoryID == 0 {
		created.CategoryID = spec.CategoryID
	}
	if created.Category == 0 {
		created.Category = spec.CategoryID
	}
	if created.ClientRequestID == "" {
		created.ClientRequestID = spec.ClientRequestID
	}
	if created.Status == "" {
		created.Status = "published"
	}
	if len(created.Photos) == 0 && len(uploadedPhotos) > 0 {
		created.Photos = uploadedPhotos
	}
	if created.URL == "" && created.ID != "" {
		created.URL = fmt.Sprintf("https://www.milanuncios.com/anuncios/%s.htm", created.ID)
	}

	return &created, nil
}

// UpdateAd applies a partial patch (RFC 7396) to an existing ad listing.
func (c *Client) UpdateAd(ctx context.Context, adID string, patch map[string]interface{}) (*AdUpdateResult, error) {
	id, err := ExtractAdID(adID)
	if err != nil {
		return nil, err
	}
	if len(patch) == 0 {
		return nil, output.Usagef("patch payload cannot be empty")
	}

	var res AdUpdateResult
	endpoint := fmt.Sprintf("/api/v1/ads/%s", id)
	if err := c.doRequest(ctx, http.MethodPatch, endpoint, nil, patch, true, &res); err != nil {
		var de *output.DomainError
		if errors.As(err, &de) {
			if de.Status == http.StatusNotFound || de.Kind == output.KindNotFound {
				return nil, output.NotFoundf("ad not found: %s", id)
			}
			if de.Status == http.StatusBadRequest || de.Status == http.StatusUnprocessableEntity {
				return nil, output.Usagef("ad update failed (%d): %s", de.Status, de.Msg)
			}
		}
		return nil, err
	}

	if res.ID == "" {
		res.ID = id
	}
	if len(res.UpdatedFields) == 0 {
		for k := range patch {
			res.UpdatedFields = append(res.UpdatedFields, k)
		}
		sort.Strings(res.UpdatedFields)
	}
	if res.UpdatedAt == "" {
		res.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	return &res, nil
}

// DeleteAd withdraws or deletes an ad listing with a structured reason.
func (c *Client) DeleteAd(ctx context.Context, adID string, reason, feedback string) (*AdDeleteResult, error) {
	id, err := ExtractAdID(adID)
	if err != nil {
		return nil, err
	}

	validReasons := map[string]bool{
		"sold_milanuncios": true,
		"sold_other":       true,
		"withdrawn":        true,
		"expired":          true,
	}
	cleanReason := strings.ToLower(strings.TrimSpace(reason))
	if !validReasons[cleanReason] {
		return nil, output.Usagef("invalid reason %q: must be one of 'sold_milanuncios', 'sold_other', 'withdrawn', 'expired'", reason)
	}

	payload := map[string]string{
		"reason": cleanReason,
	}
	if feedback != "" {
		payload["feedback"] = feedback
	}

	var res AdDeleteResult
	endpoint := fmt.Sprintf("/api/v1/ads/%s", id)
	if err := c.doRequest(ctx, http.MethodDelete, endpoint, nil, payload, true, &res); err != nil {
		var de *output.DomainError
		if errors.As(err, &de) {
			if de.Status == http.StatusNotFound || de.Kind == output.KindNotFound {
				return nil, output.NotFoundf("ad not found: %s", id)
			}
			if de.Status == http.StatusBadRequest || de.Status == http.StatusUnprocessableEntity {
				return nil, output.Usagef("ad delete failed (%d): %s", de.Status, de.Msg)
			}
		}
		return nil, err
	}

	if res.ID == "" {
		res.ID = id
	}
	if res.Status == "" {
		res.Status = "deleted"
	}
	if res.Reason == "" {
		res.Reason = cleanReason
	}

	return &res, nil
}

func detectImageMime(data []byte, filename string) string {
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	if len(data) >= 8 && data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' {
		return "image/png"
	}
	detected := http.DetectContentType(data)
	if detected == "image/jpeg" || detected == "image/png" || detected == "image/webp" {
		return detected
	}
	if filename != "" {
		ext := strings.ToLower(filepath.Ext(filename))
		switch ext {
		case ".jpg", ".jpeg":
			return "image/jpeg"
		case ".png":
			return "image/png"
		case ".webp":
			return "image/webp"
		}
	}
	return detected
}

func mimeToExt(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

func extractFilenameFromURL(rawURL, mimeType string) string {
	u, err := url.Parse(rawURL)
	if err == nil {
		base := filepath.Base(u.Path)
		if base != "." && base != "/" && base != "" {
			return base
		}
	}
	return "photo" + mimeToExt(mimeType)
}
