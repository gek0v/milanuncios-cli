package milanuncios

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

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
