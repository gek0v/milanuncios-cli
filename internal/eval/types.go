package eval

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// DealRating clasifica la calidad del precio.
type DealRating string

const (
	RatingSuperDeal  DealRating = "super_deal"
	RatingGreatPrice DealRating = "great_price"
	RatingFairPrice  DealRating = "fair_price"
	RatingHighPrice  DealRating = "high_price"
	RatingOverpriced DealRating = "overpriced"
)

// EvaluationReport contiene el análisis exhaustivo de precio de un anuncio.
type EvaluationReport struct {
	AdID            string         `json:"ad_id"`
	Title           string         `json:"title"`
	Price           float64        `json:"price"`
	CategoryID      int            `json:"category_id"`
	CategoryName    string         `json:"category_name,omitempty"`
	URL             string         `json:"url"`
	DealScore       int            `json:"deal_score"`
	Rating          DealRating     `json:"rating"`
	DiscountPercent float64        `json:"discount_percent"`
	MarketMedian    float64        `json:"market_median"`
	MarketQ1        float64        `json:"market_q1"`
	MarketQ3        float64        `json:"market_q3"`
	SampleSize      int            `json:"sample_size"`
	Confidence      float64        `json:"confidence"`
	Verdict         string         `json:"verdict"`
	PriceHistory    []PricePoint   `json:"price_history,omitempty"`
	Comparables     []ComparableAd `json:"comparables,omitempty"`
	EvaluatedAt     time.Time      `json:"evaluated_at"`
}

// PricePoint representa un punto histórico de precio para un anuncio.
type PricePoint struct {
	Date  string  `json:"date"`
	Price float64 `json:"price"`
}

// ComparableAd representa un anuncio similar utilizado en el análisis de mercado.
type ComparableAd struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Price      float64 `json:"price"`
	Similarity float64 `json:"similarity"`
	URL        string  `json:"url"`
}

// FormatEuro formats a float64 into standard Spanish currency format (e.g. "950 €", "1.200 €", "950,50 €").
func FormatEuro(val float64) string {
	isInteger := math.Abs(val-math.Round(val)) < 0.001
	var numStr string
	if isInteger {
		numStr = strconv.FormatInt(int64(math.Round(val)), 10)
	} else {
		numStr = fmt.Sprintf("%.2f", val)
		numStr = strings.Replace(numStr, ".", ",", 1)
	}

	parts := strings.Split(numStr, ",")
	intPart := parts[0]
	neg := false
	if strings.HasPrefix(intPart, "-") {
		neg = true
		intPart = strings.TrimPrefix(intPart, "-")
	}

	var formatted []byte
	for i := len(intPart) - 1; i >= 0; i-- {
		formatted = append([]byte{intPart[i]}, formatted...)
		if (len(intPart)-i)%3 == 0 && i > 0 {
			formatted = append([]byte{'.'}, formatted...)
		}
	}
	res := string(formatted)
	if neg {
		res = "-" + res
	}
	if len(parts) > 1 {
		res += "," + parts[1]
	}
	return res + " €"
}

func dealStars(score int) string {
	switch {
	case score >= 85:
		return "[★★★★★]"
	case score >= 70:
		return "[★★★★☆]"
	case score >= 50:
		return "[★★★☆☆]"
	case score >= 35:
		return "[★★☆☆☆]"
	default:
		return "[★☆☆☆☆]"
	}
}

func ratingLabel(r DealRating) string {
	switch r {
	case RatingSuperDeal:
		return "CHOLLO EXCEPCIONAL (super_deal)"
	case RatingGreatPrice:
		return "MUY BUEN PRECIO (great_price)"
	case RatingFairPrice:
		return "PRECIO JUSTO (fair_price)"
	case RatingHighPrice:
		return "PRECIO ELEVADO (high_price)"
	case RatingOverpriced:
		return "SOBREPRECIO (overpriced)"
	default:
		return string(r)
	}
}

func colorizeRating(r DealRating, color bool) string {
	lbl := ratingLabel(r)
	if !color {
		return lbl
	}
	switch r {
	case RatingSuperDeal:
		return fmt.Sprintf("\x1b[1;32m%s\x1b[0m", lbl)
	case RatingGreatPrice:
		return fmt.Sprintf("\x1b[32m%s\x1b[0m", lbl)
	case RatingFairPrice:
		return fmt.Sprintf("\x1b[33m%s\x1b[0m", lbl)
	case RatingHighPrice:
		return fmt.Sprintf("\x1b[38;5;208m%s\x1b[0m", lbl)
	case RatingOverpriced:
		return fmt.Sprintf("\x1b[31m%s\x1b[0m", lbl)
	default:
		return lbl
	}
}

// Pretty prints the evaluation report in human-friendly terminal format.
func (r *EvaluationReport) Pretty(w io.Writer, color bool) {
	if r == nil {
		return
	}

	border := "======================================================================"
	fmt.Fprintln(w, border)
	if color {
		fmt.Fprintln(w, "\x1b[1m  📊 EVALUACIÓN DE PRECIO & DEAL SCORE\x1b[0m")
	} else {
		fmt.Fprintln(w, "  📊 EVALUACIÓN DE PRECIO & DEAL SCORE")
	}
	fmt.Fprintln(w, border)

	fmt.Fprintf(w, "Anuncio:  %s\n", r.Title)
	fmt.Fprintf(w, "ID:       %s\n", r.AdID)
	fmt.Fprintf(w, "Precio:   %s\n", FormatEuro(r.Price))

	fmt.Fprintln(w, "\nRESULTADO:")
	stars := dealStars(r.DealScore)
	fmt.Fprintf(w, "  Deal Score:        %d / 100  %s\n", r.DealScore, stars)
	fmt.Fprintf(w, "  Calificación:      %s\n", colorizeRating(r.Rating, color))

	var discountStr string
	if r.DiscountPercent > 0 {
		discountStr = fmt.Sprintf("-%0.1f%% frente a la mediana de mercado", r.DiscountPercent)
	} else if r.DiscountPercent < 0 {
		discountStr = fmt.Sprintf("+%0.1f%% sobre la mediana de mercado", -r.DiscountPercent)
	} else {
		discountStr = "0.0% frente a la mediana de mercado"
	}
	fmt.Fprintf(w, "  Descuento:         %s\n", discountStr)

	fmt.Fprintln(w, "\nBENCHMARK DE MERCADO:")
	if r.SampleSize > 0 {
		fmt.Fprintf(w, "  Mediana:           %s\n", FormatEuro(r.MarketMedian))
		fmt.Fprintf(w, "  Rango Típico:      %s (Q1) - %s (Q3)\n", FormatEuro(r.MarketQ1), FormatEuro(r.MarketQ3))
		fmt.Fprintf(w, "  Muestra analizada: %d anuncios similares (Confianza: %.0f%%)\n", r.SampleSize, r.Confidence*100)
	} else {
		fmt.Fprintln(w, "  Sin muestra suficiente en el mercado.")
	}

	if len(r.PriceHistory) > 0 {
		fmt.Fprintln(w, "\nHISTÓRICO DEL ANUNCIO:")
		for i, pt := range r.PriceHistory {
			line := fmt.Sprintf("  %s: %s", pt.Date, FormatEuro(pt.Price))
			if i > 0 {
				diff := pt.Price - r.PriceHistory[i-1].Price
				if diff < 0 {
					if color {
						line += fmt.Sprintf("  \x1b[32m(Bajada de %s)\x1b[0m", FormatEuro(-diff))
					} else {
						line += fmt.Sprintf("  (Bajada de %s)", FormatEuro(-diff))
					}
				} else if diff > 0 {
					if color {
						line += fmt.Sprintf("  \x1b[31m(Subida de %s)\x1b[0m", FormatEuro(diff))
					} else {
						line += fmt.Sprintf("  (Subida de %s)", FormatEuro(diff))
					}
				}
			}
			fmt.Fprintln(w, line)
		}
	}

	if r.Verdict != "" {
		fmt.Fprintln(w, "\nVEREDICTO:")
		fmt.Fprintf(w, "  %s\n", r.Verdict)
	}

	if len(r.Comparables) > 0 {
		fmt.Fprintln(w, "\nCOMPARABLES:")
		for _, c := range r.Comparables {
			fmt.Fprintf(w, "  - %s (%s) [%.0f%% similar] %s\n", c.Title, FormatEuro(c.Price), c.Similarity*100, c.URL)
		}
	}

	fmt.Fprintln(w, border)
}
