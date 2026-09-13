package main

import (
	"testing"
)

func TestFlagEnvironmentPrecedence(t *testing.T) {
	t.Setenv("GOATCOUNTER_LISTEN", ":9000")
	t.Setenv("GOATCOUNTER_DEV", "true")
	f := newFlags("test")
	listen := f.String("listen", ":8080", "")
	dev := f.Bool("dev", false, "")
	if err := parseFlags(f, []string{"-listen", ":9001", "-dev=false"}, true, false); err != nil {
		t.Fatal(err)
	}
	if *listen != ":9001" || *dev {
		t.Fatalf("flags = %s %t", *listen, *dev)
	}
}
func TestFlagErrors(t *testing.T) {
	for _, args := range [][]string{{"-missing"}, {"extra"}, {"-timeout", "bad"}} {
		f := newFlags("test")
		f.Int("timeout", 3, "")
		if err := parseFlags(f, args, false, false); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}
