package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestIsRemote(t *testing.T) {
	for _, tt := range []struct {
		connect string
		want    bool
	}{
		{"libsql+libsql://example.com", true},
		{"libsql+https://example.com", true},
		{"libsql+http://example.com", true},
		{"libsql://example.com", true},
		{"https://example.com", true},
		{"http://example.com", true},
		{"libsql+file:/data/goatcounter.db", false},
		{"file:/data/goatcounter.db", false},
		{":memory:", false},
	} {
		t.Run(tt.connect, func(t *testing.T) {
			if got := isRemote(tt.connect); got != tt.want {
				t.Fatalf("isRemote(%q) = %t; want %t", tt.connect, got, tt.want)
			}
		})
	}
}

func TestConcurrentEmptyDatabaseInitialization(t *testing.T) {
	connect := FileConnect(filepath.Join(t.TempDir(), "shared.db"))
	schema := "create table example (id integer primary key); insert into example values (1);"
	start := make(chan struct{})
	errs := make(chan error, 3)
	for range 3 {
		go func() {
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			db, err := Open(ctx, ConnectOptions{Connect: connect, Create: true, Schema: schema})
			if err == nil {
				db.Close()
			}
			errs <- err
		}()
	}
	close(start)
	for range 3 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}
