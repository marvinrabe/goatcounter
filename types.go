package goatcounter

import "github.com/marvinrabe/goatcounter/internal/parse"

// Floats accepts the tracker's comma-separated screen dimensions.
type Floats []float64

func (l *Floats) UnmarshalText(v []byte) error {
	var err error
	*l, err = parse.Floats(string(v), ",")
	return err
}
