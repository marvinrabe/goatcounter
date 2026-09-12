package libsql

import (
	"context"
	"path/filepath"
	"testing"

	"zgo.at/zdb"
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

func TestConfigureRemotePool(t *testing.T) {
	db, err := Open(context.Background(), zdb.ConnectOptions{
		Connect: FileConnect(filepath.Join(t.TempDir(), "pool.db")),
		Create:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	sqlDB, _ := db.DBSQL()
	sqlDB.SetMaxIdleConns(1)
	if err := sqlDB.Ping(); err != nil {
		t.Fatal(err)
	}
	if got := sqlDB.Stats().Idle; got != 1 {
		t.Fatalf("idle connections before remote configuration = %d; want 1", got)
	}

	configureRemotePool(db, "libsql+libsql://example.com")
	if err := sqlDB.Ping(); err != nil {
		t.Fatal(err)
	}
	if got := sqlDB.Stats().Idle; got != 0 {
		t.Fatalf("idle connections after remote configuration = %d; want 0", got)
	}
}
