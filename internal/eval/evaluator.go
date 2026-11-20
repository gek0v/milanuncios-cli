package eval

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/store"
)

var (
	nonAlphanumericRegex = regexp.MustCompile(`[^\p{L}\p{N}]+`)
	stopWords            = map[string]bool{
		"de": true, "del": true, "en": true, "el": true, "la": true,
		"los": true, "las": true, "un": true, "una": true, "unos": true,
		"unas": true, "con": true, "para": true, "por": true, "y": true,
		"o": true, "a": true, "al": true, "se": true, "su": true, "sus": true,
	}
)

// Evaluator evaluates ad pricing against market benchmarks.
type Evaluator struct {
	Client *milanuncios.Client
	Store  *store.Store
}

// NewEvaluator creates a new price evaluator.
func NewEvaluator(client *milanuncios.Client, st *store.Store) *Evaluator {
	return &Evaluator{
		Client: client,
		Store:  st,
	}
}

// ExtractSearchTokens extracts meaningful search keywords from an ad title,
// removing punctuation and common Spanish stop words.
func ExtractSearchTokens(title string) []string {
	normalized := strings.ToLower(title)
	words := nonAlphanumericRegex.Split(normalized, -1)
	var tokens []string
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" || stopWords[w] {
			continue
		}
		tokens = append(tokens, w)
	}
	if len(tokens) == 0 {
		for _, w := range words {
			if w != "" {
				tokens = append(tokens, w)
			}
		}
	}
	return tokens
}

// tokenSimilarity computes Jaccard similarity between two titles based on their extracted tokens.
func tokenSimilarity(t1, t2 string) float64 {
	tokens1 := ExtractSearchTokens(t1)
	tokens2 := ExtractSearchTokens(t2)
	if len(tokens1) == 0 || len(tokens2) == 0 {
		return 0.0
	}
	set1 := make(map[string]bool, len(tokens1))
	for _, t := range tokens1 {
		set1[t] = true
	}
	intersection := 0
	unionMap := make(map[string]bool, len(tokens1)+len(tokens2))
	for _, t := range tokens1 {
		unionMap[t] = true
	}
	for _, t := range tokens2 {
		if set1[t] {
			intersection++
		}
		unionMap[t] = true
	}
	if len(unionMap) == 0 {
		return 0.0
	}
	return float64(intersection) / float64(len(unionMap))
}

// EvaluateAd evaluates an ad listing and produces an EvaluationReport.
func (e *Evaluator) EvaluateAd(ctx context.Context, adID string, includeComparables bool) (*EvaluationReport, error) {
	ad, err := e.Client.GetAd(ctx, adID)
	if err != nil {
		return nil, err
	}

	catID := ad.CategoryID
	if catID == 0 {
		catID = ad.Category
	}

	tokens := ExtractSearchTokens(ad.Title)
	queryTokens := tokens
	if len(queryTokens) > 5 {
		queryTokens = queryTokens[:5]
	}
	searchKeywords := strings.Join(queryTokens, " ")

	searchQuery := milanuncios.SearchQuery{
		Keywords: searchKeywords,
		Category: catID,
		Limit:    50,
	}

	searchRes, err := e.Client.Search(ctx, searchQuery)
	if err != nil {
		return nil, err
	}

	// If category filter returned no results, retry without category filter
	if (searchRes == nil || len(searchRes.Items) == 0) && catID > 0 {
		fallbackQuery := milanuncios.SearchQuery{
			Keywords: searchKeywords,
			Limit:    50,
		}
		if fbRes, fbErr := e.Client.Search(ctx, fallbackQuery); fbErr == nil && fbRes != nil && len(fbRes.Items) > 0 {
			searchRes = fbRes
		}
	}

	var rawPrices []float64
	var candidateAds []milanuncios.AdItem
	if searchRes != nil {
		for _, it := range searchRes.Items {
			if it.ID == ad.ID {
				continue
			}
			if it.Price > 0 {
				rawPrices = append(rawPrices, it.Price)
				candidateAds = append(candidateAds, it)
			}
		}
	}

	cleanPrices, _ := FilterIQROutliers(rawPrices)
	sampleSize := len(cleanPrices)
	confidence := CalculateConfidence(sampleSize)

	var median, q1, q3 float64
	var discount float64
	var score int
	var rating DealRating
	var verdict string

	if sampleSize == 0 {
		score, rating = 50, RatingFairPrice
		verdict = "Muestra de mercado insuficiente para calcular el Deal Score de forma fiable."
	} else {
		q1, median, q3 = CalculateQuartiles(cleanPrices)
		discount = CalculateDiscountPercent(ad.Price, median)
		score, rating = CalculateDealScore(ad.Price, median, sampleSize)

		if confidence < 0.4 {
			verdict = fmt.Sprintf("Muestra insuficiente para valoración estadística definitiva (%d anuncios analizados).", sampleSize)
		} else {
			diff := median - ad.Price
			if diff > 0 {
				verdict = fmt.Sprintf("El anuncio se encuentra %.0f € por debajo de la mediana del mercado (%.0f €) basada en %d anuncios comparables.", diff, median, sampleSize)
			} else if diff < 0 {
				verdict = fmt.Sprintf("El anuncio se encuentra %.0f € por encima de la mediana del mercado (%.0f €) basada en %d anuncios comparables.", -diff, median, sampleSize)
			} else {
				verdict = fmt.Sprintf("El anuncio tiene un precio idéntico a la mediana del mercado (%.0f €) basada en %d anuncios comparables.", median, sampleSize)
			}
		}
	}

	var comparables []ComparableAd
	if includeComparables && len(candidateAds) > 0 {
		type scoredCand struct {
			ad         milanuncios.AdItem
			similarity float64
		}
		var scored []scoredCand
		for _, cand := range candidateAds {
			sim := tokenSimilarity(ad.Title, cand.Title)
			scored = append(scored, scoredCand{
				ad:         cand,
				similarity: math.Round(sim*100) / 100,
			})
		}
		sort.Slice(scored, func(i, j int) bool {
			if scored[i].similarity != scored[j].similarity {
				return scored[i].similarity > scored[j].similarity
			}
			distI := math.Abs(scored[i].ad.Price - median)
			distJ := math.Abs(scored[j].ad.Price - median)
			return distI < distJ
		})

		maxC := 5
		if len(scored) < maxC {
			maxC = len(scored)
		}
		for i := 0; i < maxC; i++ {
			c := scored[i]
			comparables = append(comparables, ComparableAd{
				ID:         c.ad.ID,
				Title:      c.ad.Title,
				Price:      c.ad.Price,
				Similarity: c.similarity,
				URL:        c.ad.URL,
			})
		}
	}

	var priceHistory []PricePoint
	if e.Store != nil {
		pts, err := e.Store.GetPriceHistory(ctx, ad.ID)
		if err == nil && len(pts) > 0 {
			for _, p := range pts {
				priceHistory = append(priceHistory, PricePoint{
					Date:  p.Date,
					Price: p.Price,
				})
			}
		}
	}

	report := &EvaluationReport{
		AdID:            ad.ID,
		Title:           ad.Title,
		Price:           ad.Price,
		CategoryID:      catID,
		URL:             ad.URL,
		DealScore:       score,
		Rating:          rating,
		DiscountPercent: discount,
		MarketMedian:    median,
		MarketQ1:        q1,
		MarketQ3:        q3,
		SampleSize:      sampleSize,
		Confidence:      confidence,
		Verdict:         verdict,
		PriceHistory:    priceHistory,
		Comparables:     comparables,
		EvaluatedAt:     time.Now().UTC(),
	}

	return report, nil
}
