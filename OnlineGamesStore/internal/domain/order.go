package domain

import "errors"

var ErrInvalidTransition = errors.New("invalid order status transition")
var ErrInvalidLine = errors.New("invalid order line")

type Line struct {
	Quantity       int
	UnitPriceCents int64
}

func CalculateTotal(lines []Line) (int64, error) {
	var total int64
	if len(lines) == 0 {
		return 0, ErrInvalidLine
	}
	for _, l := range lines {
		if l.Quantity <= 0 || l.UnitPriceCents < 0 {
			return 0, ErrInvalidLine
		} // guard int64 overflow
		q := int64(l.Quantity)
		if q > 0 && l.UnitPriceCents > (int64(^uint64(0)>>1)-total)/q {
			return 0, errors.New("total overflow")
		}
		total += q * l.UnitPriceCents
	}
	return total, nil
}
func CanTransition(from, to string) bool {
	allowed := map[string][]string{"NEW": {"PAID", "CANCELLED"}, "PAID": {"PROCESSING", "CANCELLED"}, "PROCESSING": {"COMPLETED", "CANCELLED"}}
	for _, v := range allowed[from] {
		if v == to {
			return true
		}
	}
	return false
}
