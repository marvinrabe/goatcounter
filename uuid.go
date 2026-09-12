package goatcounter

import (
	"uuid"

	"zgo.at/zstd/zint"
)

// UUID creates a new UUID v4.
func UUID() zint.Uint128 {
	u := uuid.NewV4()
	i, err := zint.NewUint128(u[:])
	if err != nil {
		panic(err)
	}
	return i
}
