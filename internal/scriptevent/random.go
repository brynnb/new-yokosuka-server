package scriptevent

import (
	"crypto/rand"
	"math/big"
)

// secureRandomInteger returns an unbiased value in the inclusive range. big.Int
// arithmetic keeps the full signed int64 range well-defined without overflow.
func secureRandomInteger(minimum, maximum int64) (int64, error) {
	min := big.NewInt(minimum)
	span := new(big.Int).Sub(big.NewInt(maximum), min)
	span.Add(span, big.NewInt(1))
	offset, err := rand.Int(rand.Reader, span)
	if err != nil {
		return 0, err
	}
	return offset.Add(offset, min).Int64(), nil
}
