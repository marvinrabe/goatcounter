package testenv

import (
	"fmt"
	"testing"
)

func TestDB(t *testing.T) {
	t.Run("", func(t *testing.T) {
		fmt.Println("Run 1")
		DB(t)
	})

	t.Run("", func(t *testing.T) {
		fmt.Println("\nRun 2")
		DB(t)
	})

	t.Run("", func(t *testing.T) {
		fmt.Println("\nRun 3")
		DB(t)
	})
}
