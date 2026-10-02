package main

import (
	"archive/zip"
	"bytes"
	"context"
	"slices"
	"testing"
	"time"

	"github.com/marvinrabe/goatcounter/internal/testenv"
)

func TestMigrateDevicesLaptopIsDesktop(t *testing.T) {
	db, ctx := testenv.Store(t).DB, context.Background()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("imported_devices_20260101_20260131.csv")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("date,device,visitors,visits,visit_duration,bounces,pageviews\n" +
		"2026-01-10,Laptop,3,3,0,0,3\n" +
		"2026-01-10,Desktop,2,2,0,0,2\n" +
		"2026-01-10,Mobile,1,1,0,0,1\n"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrateFile(ctx, db, zr.File[0], "example.com", "devices", time.UTC); err != nil {
		t.Fatal(err)
	}

	var devices []string
	err = db.Select(ctx, &devices, `select device from events where aggregate = 'devices' order by device`)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Desktop", "Desktop", "Mobile"}; !slices.Equal(devices, want) {
		t.Errorf("have %q, want %q", devices, want)
	}
}
