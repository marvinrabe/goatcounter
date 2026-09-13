// Package parse handles the application's numeric query parameters and lists.
package parse

import (
	"reflect"
	"strconv"
	"strings"
)

type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

func Int[T Integer](s string, base int) (T, error) {
	if reflect.TypeFor[T]().Kind() >= reflect.Uint && reflect.TypeFor[T]().Kind() <= reflect.Uint64 {
		n, err := strconv.ParseUint(s, base, reflect.TypeFor[T]().Bits())
		return T(n), err
	}
	n, err := strconv.ParseInt(s, base, reflect.TypeFor[T]().Bits())
	return T(n), err
}
func Ints[T Integer](s, sep string) ([]T, error) {
	s = strings.Trim(s, " \t\n"+sep)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, sep)
	result := make([]T, len(parts))
	for i, p := range parts {
		v, err := Int[T](strings.TrimSpace(p), 10)
		if err != nil {
			return nil, err
		}
		result[i] = v
	}
	return result, nil
}
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
