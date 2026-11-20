package eval_test

import (
	"math"
	"testing"

	"github.com/gek0v/milanuncios-cli/internal/eval"
)

func TestCalculateMedian(t *testing.T) {
	tests := []struct {
		name     string
		prices   []float64
		expected float64
	}{
		{
			name:     "empty slice",
			prices:   nil,
			expected: 0.0,
		},
		{
			name:     "single element",
			prices:   []float64{150.0},
			expected: 150.0,
		},
		{
			name:     "odd length unordered",
			prices:   []float64{300.0, 100.0, 200.0},
			expected: 200.0,
		},
		{
			name:     "odd length five elements",
			prices:   []float64{50.0, 10.0, 30.0, 40.0, 20.0},
			expected: 30.0,
		},
		{
			name:     "even length unordered",
			prices:   []float64{400.0, 100.0, 200.0, 300.0},
			expected: 250.0,
		},
		{
			name:     "even length two elements",
			prices:   []float64{100.0, 200.0},
			expected: 150.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := make([]float64, len(tt.prices))
			copy(orig, tt.prices)

			got := eval.CalculateMedian(tt.prices)
			if math.Abs(got-tt.expected) > 0.0001 {
				t.Errorf("CalculateMedian() = %v, want %v", got, tt.expected)
			}

			// Ensure original slice was not mutated
			for i := range orig {
				if orig[i] != tt.prices[i] {
					t.Errorf("CalculateMedian mutated input slice at index %d", i)
				}
			}
		})
	}
}

func TestCalculateQuartiles(t *testing.T) {
	tests := []struct {
		name    string
		prices  []float64
		wantQ1  float64
		wantMed float64
		wantQ3  float64
	}{
		{
			name:    "empty slice",
			prices:  nil,
			wantQ1:  0,
			wantMed: 0,
			wantQ3:  0,
		},
		{
			name:    "single element",
			prices:  []float64{100.0},
			wantQ1:  100.0,
			wantMed: 100.0,
			wantQ3:  100.0,
		},
		{
			name:    "four elements",
			prices:  []float64{10.0, 20.0, 30.0, 40.0},
			wantQ1:  15.0,
			wantMed: 25.0,
			wantQ3:  35.0,
		},
		{
			name:    "five elements",
			prices:  []float64{10.0, 20.0, 30.0, 40.0, 50.0},
			wantQ1:  15.0,
			wantMed: 30.0,
			wantQ3:  45.0,
		},
		{
			name:    "seven elements",
			prices:  []float64{50.0, 60.0, 70.0, 80.0, 90.0, 100.0, 110.0},
			wantQ1:  60.0,
			wantMed: 80.0,
			wantQ3:  100.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q1, med, q3 := eval.CalculateQuartiles(tt.prices)
			if math.Abs(q1-tt.wantQ1) > 0.0001 || math.Abs(med-tt.wantMed) > 0.0001 || math.Abs(q3-tt.wantQ3) > 0.0001 {
				t.Errorf("CalculateQuartiles() = (%v, %v, %v), want (%v, %v, %v)",
					q1, med, q3, tt.wantQ1, tt.wantMed, tt.wantQ3)
			}
		})
	}
}

func TestFilterIQROutliers(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		clean, outliers := eval.FilterIQROutliers(nil)
		if len(clean) != 0 || len(outliers) != 0 {
			t.Errorf("expected empty slices for nil input, got clean=%v outliers=%v", clean, outliers)
		}
	})

	t.Run("synthetic dataset with 1€ dummy ads and extreme price tags", func(t *testing.T) {
		// Normal prices around 100-140€, plus fake 1€ ads and a 2000€ extreme price
		prices := []float64{
			1.0,    // fake 1€ ad
			100.0,  // valid
			110.0,  // valid
			115.0,  // valid
			120.0,  // valid
			125.0,  // valid
			130.0,  // valid
			140.0,  // valid
			2000.0, // extreme outlier
		}

		clean, outliers := eval.FilterIQROutliers(prices)

		// Verify 1.0 and 2000.0 are in outliers
		has1 := false
		has2000 := false
		for _, o := range outliers {
			if o == 1.0 {
				has1 = true
			}
			if o == 2000.0 {
				has2000 = true
			}
		}
		if !has1 {
			t.Errorf("expected 1.0 in outliers, got %v", outliers)
		}
		if !has2000 {
			t.Errorf("expected 2000.0 in outliers, got %v", outliers)
		}

		// Verify clean contains the realistic middle prices
		for _, c := range clean {
			if c < 5.0 || c > 500.0 {
				t.Errorf("unexpected price in clean list: %v", c)
			}
		}
		if len(clean) != 7 {
			t.Errorf("expected 7 clean prices, got %d (%v)", len(clean), clean)
		}
	})

	t.Run("all prices below 5€ are dropped", func(t *testing.T) {
		prices := []float64{0.0, 1.0, 2.0, 4.99}
		clean, outliers := eval.FilterIQROutliers(prices)
		if len(clean) != 0 {
			t.Errorf("expected 0 clean prices, got %v", clean)
		}
		if len(outliers) != 4 {
			t.Errorf("expected 4 outliers, got %v", outliers)
		}
	})
}

func TestCalculateDiscountPercent(t *testing.T) {
	tests := []struct {
		name        string
		targetPrice float64
		median      float64
		expected    float64
	}{
		{
			name:        "canonical bargain: 950 vs 1200",
			targetPrice: 950.0,
			median:      1200.0,
			expected:    20.8, // 250/1200 * 100 = 20.833%
		},
		{
			name:        "at median",
			targetPrice: 1000.0,
			median:      1000.0,
			expected:    0.0,
		},
		{
			name:        "overpriced: 1500 vs 1000",
			targetPrice: 1500.0,
			median:      1000.0,
			expected:    -50.0,
		},
		{
			name:        "zero median",
			targetPrice: 100.0,
			median:      0.0,
			expected:    0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eval.CalculateDiscountPercent(tt.targetPrice, tt.median)
			if math.Abs(got-tt.expected) > 0.1 {
				t.Errorf("CalculateDiscountPercent() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCalculateConfidence(t *testing.T) {
	tests := []struct {
		sampleSize int
		expected   float64
	}{
		{sampleSize: -5, expected: 0.0},
		{sampleSize: 0, expected: 0.0},
		{sampleSize: 2, expected: 0.2},
		{sampleSize: 5, expected: 0.5},
		{sampleSize: 9, expected: 0.9},
		{sampleSize: 10, expected: 1.0},
		{sampleSize: 26, expected: 1.0},
	}

	for _, tt := range tests {
		got := eval.CalculateConfidence(tt.sampleSize)
		if math.Abs(got-tt.expected) > 0.001 {
			t.Errorf("CalculateConfidence(%d) = %v, want %v", tt.sampleSize, got, tt.expected)
		}
	}
}

func TestCalculateDealScore(t *testing.T) {
	t.Run("exact 35% discount with full confidence -> 100 score, super_deal", func(t *testing.T) {
		median := 100.0
		target := 65.0 // 35% discount
		sampleSize := 10

		score, rating := eval.CalculateDealScore(target, median, sampleSize)
		if score != 100 {
			t.Errorf("expected score 100, got %d", score)
		}
		if rating != eval.RatingSuperDeal {
			t.Errorf("expected rating super_deal, got %v", rating)
		}
	})

	t.Run("at median -> 65 score, fair_price", func(t *testing.T) {
		median := 100.0
		target := 100.0
		sampleSize := 10

		score, rating := eval.CalculateDealScore(target, median, sampleSize)
		if score != 65 {
			t.Errorf("expected score 65, got %d", score)
		}
		if rating != eval.RatingFairPrice {
			t.Errorf("expected rating fair_price, got %v", rating)
		}
	})

	t.Run("50% overpriced -> 10 score, overpriced", func(t *testing.T) {
		median := 100.0
		target := 150.0 // 50% above median
		sampleSize := 10

		score, rating := eval.CalculateDealScore(target, median, sampleSize)
		if score != 10 {
			t.Errorf("expected score 10, got %d", score)
		}
		if rating != eval.RatingOverpriced {
			t.Errorf("expected rating overpriced, got %v", rating)
		}
	})

	t.Run("small sample confidence scaling", func(t *testing.T) {
		median := 100.0
		target := 65.0 // 35% discount

		// With sampleSize = 5 (C = 0.5):
		// score = 65 + int(round(35 * 1.0 * 0.5)) = 65 + 18 = 83
		score5, rating5 := eval.CalculateDealScore(target, median, 5)
		if score5 < 80 || score5 > 84 {
			t.Errorf("expected scaled score around 82-83, got %d", score5)
		}
		if rating5 != eval.RatingGreatPrice {
			t.Errorf("expected great_price with partial confidence, got %v", rating5)
		}

		// With sampleSize = 2 (C = 0.2):
		// score = 65 + int(round(35 * 1.0 * 0.2)) = 65 + 7 = 72
		score2, rating2 := eval.CalculateDealScore(target, median, 2)
		if score2 != 72 {
			t.Errorf("expected score 72 for sampleSize 2, got %d", score2)
		}
		if rating2 != eval.RatingGreatPrice {
			t.Errorf("expected great_price, got %v", rating2)
		}
	})

	t.Run("marketMedian <= 0 defaults to 50 fair_price", func(t *testing.T) {
		score, rating := eval.CalculateDealScore(100.0, 0.0, 10)
		if score != 50 || rating != eval.RatingFairPrice {
			t.Errorf("expected (50, fair_price), got (%d, %v)", score, rating)
		}
	})

	t.Run("rating tiers coverage", func(t *testing.T) {
		// >= 85: super_deal
		s85, r85 := eval.CalculateDealScore(70.0, 100.0, 10)
		if s85 < 85 || r85 != eval.RatingSuperDeal {
			t.Errorf("score=%d, rating=%v, want >=85 and super_deal", s85, r85)
		}

		// >= 70: great_price
		s75, r75 := eval.CalculateDealScore(90.0, 100.0, 10)
		if s75 < 70 || s75 >= 85 || r75 != eval.RatingGreatPrice {
			t.Errorf("score=%d, rating=%v, want [70, 84] and great_price", s75, r75)
		}

		// >= 50: fair_price
		s55, r55 := eval.CalculateDealScore(105.0, 100.0, 10)
		if s55 < 50 || s55 >= 70 || r55 != eval.RatingFairPrice {
			t.Errorf("score=%d, rating=%v, want [50, 69] and fair_price", s55, r55)
		}

		// >= 35: high_price
		s40, r40 := eval.CalculateDealScore(125.0, 100.0, 10)
		if s40 < 35 || s40 >= 50 || r40 != eval.RatingHighPrice {
			t.Errorf("score=%d, rating=%v, want [35, 49] and high_price", s40, r40)
		}

		// < 35: overpriced
		s20, r20 := eval.CalculateDealScore(145.0, 100.0, 10)
		if s20 >= 35 || r20 != eval.RatingOverpriced {
			t.Errorf("score=%d, rating=%v, want <35 and overpriced", s20, r20)
		}
	})
}
