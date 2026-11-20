package eval

import (
	"math"
	"sort"
)

// CalculateMedian returns the median value of a slice of prices.
// Returns 0 if the slice is empty. Does not mutate the input slice.
func CalculateMedian(prices []float64) float64 {
	n := len(prices)
	if n == 0 {
		return 0
	}
	sorted := make([]float64, n)
	copy(sorted, prices)
	sort.Float64s(sorted)

	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2.0
}

// CalculateQuartiles returns Q1 (25th percentile), median, and Q3 (75th percentile)
// using Tukey's hinges. Does not mutate the input slice.
func CalculateQuartiles(prices []float64) (q1, median, q3 float64) {
	n := len(prices)
	if n == 0 {
		return 0, 0, 0
	}
	sorted := make([]float64, n)
	copy(sorted, prices)
	sort.Float64s(sorted)

	if n == 1 {
		return sorted[0], sorted[0], sorted[0]
	}

	median = CalculateMedian(sorted)
	var lower, upper []float64
	if n%2 == 0 {
		lower = sorted[:n/2]
		upper = sorted[n/2:]
	} else {
		lower = sorted[:n/2]
		upper = sorted[n/2+1:]
	}

	q1 = CalculateMedian(lower)
	q3 = CalculateMedian(upper)
	return q1, median, q3
}

// FilterIQROutliers filters extreme outliers using the Interquartile Range (IQR).
// Lower bound: max(5.0, Q1 - 1.5 * IQR)
// Upper bound: Q3 + 1.5 * IQR
// Drops fake 1€ ads, free placeholders, and extreme price tags.
func FilterIQROutliers(prices []float64) (clean []float64, outliers []float64) {
	if len(prices) == 0 {
		return nil, nil
	}

	q1, _, q3 := CalculateQuartiles(prices)
	iqr := q3 - q1
	pMin := math.Max(5.0, q1-1.5*iqr)
	pMax := q3 + 1.5*iqr

	for _, p := range prices {
		if p < pMin || p > pMax {
			outliers = append(outliers, p)
		} else {
			clean = append(clean, p)
		}
	}
	return clean, outliers
}

// CalculateDiscountPercent calculates relative discount percentage compared to the market median.
// Positive values indicate a price lower than median (bargain).
// Negative values indicate a price higher than median (overpriced).
// Returns 0.0 if marketMedian <= 0.
func CalculateDiscountPercent(targetPrice, marketMedian float64) float64 {
	if marketMedian <= 0 {
		return 0.0
	}
	pct := ((marketMedian - targetPrice) / marketMedian) * 100.0
	return math.Round(pct*10) / 10
}

// CalculateConfidence calculates statistical confidence factor C based on clean sample size.
// C = min(1.0, float64(sampleSize) / 10.0).
func CalculateConfidence(sampleSize int) float64 {
	if sampleSize <= 0 {
		return 0.0
	}
	c := float64(sampleSize) / 10.0
	if c > 1.0 {
		c = 1.0
	}
	return math.Round(c*100) / 100
}

// CalculateDealScore calculates the normalized Deal Score (0-100) and assigns a DealRating.
// Follows the canonical formula in Section 3.1:
// - If marketMedian <= 0: returns score 50, RatingFairPrice.
// - Delta = (marketMedian - targetPrice) / marketMedian.
// - If Delta == 0 -> score 65, RatingFairPrice.
// - If Delta > 0 -> score = 65 + int(round(35.0 * min(1.0, Delta/0.35) * C)).
// - If Delta < 0 -> score = max(10, 65 - int(round(55.0 * min(1.0, |Delta|/0.50)))).
// Score is clamped between 0 and 100.
func CalculateDealScore(targetPrice, marketMedian float64, sampleSize int) (score int, rating DealRating) {
	if marketMedian <= 0 {
		return 50, RatingFairPrice
	}

	delta := (marketMedian - targetPrice) / marketMedian
	if delta == 0 {
		score = 65
	} else if delta > 0 {
		c := CalculateConfidence(sampleSize)
		score = 65 + int(math.Round(35.0*math.Min(1.0, delta/0.35)*c))
	} else {
		over := int(math.Round(55.0 * math.Min(1.0, math.Abs(delta)/0.50)))
		score = int(math.Max(10, float64(65-over)))
	}

	if score < 0 {
		score = 0
	} else if score > 100 {
		score = 100
	}

	switch {
	case score >= 85:
		rating = RatingSuperDeal
	case score >= 70:
		rating = RatingGreatPrice
	case score >= 50:
		rating = RatingFairPrice
	case score >= 35:
		rating = RatingHighPrice
	default:
		rating = RatingOverpriced
	}

	return score, rating
}
