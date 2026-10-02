// Package parse handles the application's numeric query parameters and lists.
package parse

import (
	"strconv"
	"strings"
)

func Floats(s, sep string) ([]float64, error) {
	s = strings.Trim(s, " \t\n"+sep)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, sep)
	result := make([]float64, len(parts))
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return nil, err
		}
		result[i] = v
	}
	return result, nil
}
