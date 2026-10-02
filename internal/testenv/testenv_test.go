package testenv

import "testing"

func TestStore(t *testing.T) {
	for range 3 {
		t.Run("", func(t *testing.T) { Store(t) })
	}
}
