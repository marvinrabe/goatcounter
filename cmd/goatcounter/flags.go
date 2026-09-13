package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func newFlags(name string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	return f
}
func parseFlags(f *flag.FlagSet, args []string, env, ignoreUnknown bool) error {
	if env {
		for _, pair := range os.Environ() {
			key, value, _ := strings.Cut(pair, "=")
			if !strings.HasPrefix(key, "GOATCOUNTER_") || key == "GOATCOUNTER_TMPDIR" {
				continue
			}
			name := strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(key, "GOATCOUNTER_"), "_", "-"))
			if f.Lookup(name) == nil {
				if ignoreUnknown {
					continue
				}
				return fmt.Errorf("unknown environment setting: %s", key)
			}
			if err := f.Set(name, value); err != nil {
				return fmt.Errorf("invalid value for %s", key)
			}
		}
	}
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected argument: %s", f.Arg(0))
	}
	return nil
}
