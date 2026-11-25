package domain

import "testing"

func TestCalculateTotal(t *testing.T) {
	tests := []struct {
		name    string
		lines   []Line
		want    int64
		wantErr bool
	}{
		{"one item", []Line{{2, 1500}}, 3000, false}, {"several items", []Line{{2, 100}, {3, 250}}, 950, false}, {"zero price", []Line{{5, 0}}, 0, false}, {"empty", nil, 0, true}, {"zero quantity", []Line{{0, 100}}, 0, true}, {"negative quantity", []Line{{-1, 100}}, 0, true}, {"negative price", []Line{{1, -1}}, 0, true}, {"large quantity", []Line{{100, 999}}, 99900, false}, {"round cents", []Line{{1, 199}, {1, 1}}, 200, false}, {"multiple lines", []Line{{1, 100}, {2, 200}, {3, 300}}, 1400, false}, {"overflow", []Line{{2, 1 << 62}}, 0, true}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, e := CalculateTotal(tt.lines)
			if (e != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", e, tt.wantErr)
			}
			if e == nil && got != tt.want {
				t.Fatalf("got %d want %d", got, tt.want)
			}
		})
	}
}
func TestStatusTransitions(t *testing.T) {
	tests := []struct {
		from, to string
		want     bool
	}{{"NEW", "PAID", true}, {"NEW", "CANCELLED", true}, {"PAID", "PROCESSING", true}, {"PAID", "CANCELLED", true}, {"PROCESSING", "COMPLETED", true}, {"PROCESSING", "CANCELLED", true}, {"COMPLETED", "CANCELLED", false}, {"CANCELLED", "PAID", false}, {"NEW", "COMPLETED", false}, {"COMPLETED", "NEW", false}, {"UNKNOWN", "PAID", false}, {"PAID", "PAID", false}}
	for _, tt := range tests {
		t.Run(tt.from+"_"+tt.to, func(t *testing.T) {
			if got := CanTransition(tt.from, tt.to); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}
