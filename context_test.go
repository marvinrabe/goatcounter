package goatcounter

import (
	"context"
	"testing"
)

func TestContext(t *testing.T) {
	ctx := context.Background()
	{
		cfg := Config(ctx)
		if cfg == nil {
			t.Error("cfg is nil")
		}
	}

	ctx = NewCache(ctx)
	ctx = NewConfig(ctx)
	{
		c1 := Config(ctx)
		c2 := Config(ctx)
		if c1 != c2 {
			t.Errorf("%v %v", c1, c2)
		}
	}
}
