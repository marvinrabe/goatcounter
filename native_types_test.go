package goatcounter_test

import (
	"encoding/hex"
	. "github.com/marvinrabe/goatcounter"
	"testing"
	"uuid"
)

func TestSessionIDStorageCompatibility(t *testing.T) {
	id := uuid.MustParse("00112233-4455-6677-8899-aabbccddeeff")
	if got := hex.EncodeToString(id[:]); got != "00112233445566778899aabbccddeeff" {
		t.Fatal(got)
	}
	text, _ := id.MarshalText()
	var decoded SessionID
	if err := decoded.UnmarshalText(text); err != nil || decoded != id {
		t.Fatalf("text round trip: %v %v", decoded, err)
	}
}
