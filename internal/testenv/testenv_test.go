package testenv

import (
	"fmt"
	"testing"

	"github.com/marvinrabe/goatcounter/internal/log"
)

func TestDB(t *testing.T) {
	log.SetDebug([]string{"testenv"})
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
