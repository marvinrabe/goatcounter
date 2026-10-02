package goatcounter

import (
	"context"
	"testing"
	"time"

	"github.com/marvinrabe/goatcounter/internal/database"
)

func TestContext(t *testing.T) {
	ctx := context.Background()
	{
		cfg := Config(ctx)
		if cfg == nil {
			t.Error("cfg is nil")
		}
	}

	ctx = NewConfig(ctx)
	{
		c1 := Config(ctx)
		c2 := Config(ctx)
		if c1 != c2 {
			t.Errorf("%v %v", c1, c2)
		}
	}
}

func TestNewContextPreservesParent(t *testing.T) {
	type requestKey struct{}
	parent, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
	defer cancel()
	parent = context.WithValue(parent, requestKey{}, "request-id")
	parent = NewConfig(parent)
	Config(parent).DomainStatic = "static.example.com"
	db := new(database.Database)

	ctx := NewContext(parent, db)
	if got := ctx.Value(requestKey{}); got != "request-id" {
		t.Fatalf("parent value lost: %v", got)
	}
	if database.MustGetDB(ctx) != db {
		t.Fatal("application dependencies missing from context")
	}
	if Config(ctx) == Config(parent) || Config(ctx).DomainStatic != "" {
		t.Fatal("new application context reused its parent's settings")
	}
	deadline, _ := parent.Deadline()
	if got, ok := ctx.Deadline(); !ok || !got.Equal(deadline) {
		t.Fatalf("parent deadline lost: %v, %t", got, ok)
	}
	cancel()
	if ctx.Err() != context.Canceled {
		t.Fatalf("parent cancellation lost: %v", ctx.Err())
	}
}
