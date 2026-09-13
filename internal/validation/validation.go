// Package validation collects errors in application input.
package validation

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Validator struct {
	Errors map[string][]string `json:"errors"`
}

func New() Validator { return Validator{Errors: map[string][]string{}} }
func (v *Validator) Append(key, msg string) {
	if v.Errors == nil {
		v.Errors = map[string][]string{}
	}
	v.Errors[key] = append(v.Errors[key], msg)
}
func (v *Validator) HasErrors() bool { return len(v.Errors) > 0 }
func (v *Validator) ErrorOrNil() error {
	if v.HasErrors() {
		return v
	}
	return nil
}
func (v Validator) Code() int { return 400 }
func (v Validator) Error() string {
	var b strings.Builder
	for _, k := range slices.Sorted(maps.Keys(v.Errors)) {
		if k != "" {
			b.WriteString(k + ": ")
		}
		b.WriteString(strings.Join(v.Errors[k], ", "))
		b.WriteString(".\n")
	}
	return strings.TrimSpace(b.String())
}
func (v Validator) String() string { return v.Error() }
func (v *Validator) Merge(other Validator) {
	for k, errs := range other.Errors {
		for _, e := range errs {
			v.Append(k, e)
		}
	}
}
func (v *Validator) Required(key string, value any) {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() || rv.IsZero() {
		v.Append(key, "must be set")
	}
}
func (v *Validator) UTF8(key, value string) {
	if !utf8.ValidString(value) {
		v.Append(key, "must be UTF-8")
	}
}
func (v *Validator) Len(key, value string, lower, upper int) int {
	n := utf8.RuneCountInString(value)
	if n < lower {
		v.Append(key, fmt.Sprintf("must be longer than %d characters", lower))
	}
	if upper > 0 && n > upper {
		v.Append(key, fmt.Sprintf("must be shorter than %d characters", upper))
	}
	return n
}
func (v *Validator) Integer(key, value string) int64 {
	if value == "" {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		v.Append(key, "must be a whole number")
	}
	return n
}
func (v *Validator) Range(key string, value, lower, upper int64) {
	if value < lower {
		v.Append(key, fmt.Sprintf("must be %d or higher", lower))
	}
	if upper > 0 && value > upper {
		v.Append(key, fmt.Sprintf("must be %d or lower", upper))
	}
}
func (v *Validator) Domain(key, value string) {
	valid := len(value) > 0 && len(value) <= 253
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			valid = false
			break
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				valid = false
			}
		}
	}
	if !valid {
		v.Append(key, "must be a valid domain")
	}
}
