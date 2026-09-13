package goatcounter

import (
	"testing"

	"zgo.at/zstd/zint"
)

func TestMergeSessionSnapshots(t *testing.T) {
	olderID := zint.Uint128{1, 1}
	newerID := zint.Uint128{2, 2}
	key := sessionKey("example.com:visitor")

	m := new(ms)
	m.Reset()
	m.mergeSessions(storedSession{
		Sessions: map[sessionKey]zint.Uint128{key: olderID},
		Hashes:   map[zint.Uint128]sessionKey{olderID: key},
		Paths:    map[zint.Uint128]map[PathID]struct{}{olderID: {10: {}}},
		Seen:     map[zint.Uint128]int64{olderID: 100},
	})
	m.mergeSessions(storedSession{
		Sessions: map[sessionKey]zint.Uint128{key: newerID},
		Hashes:   map[zint.Uint128]sessionKey{newerID: key},
		Paths:    map[zint.Uint128]map[PathID]struct{}{newerID: {20: {}}},
		Seen:     map[zint.Uint128]int64{newerID: 200},
	})

	if got := m.sessions[key]; got != newerID {
		t.Fatalf("session ID = %v; want latest %v", got, newerID)
	}
	if _, ok := m.sessionPaths[newerID][10]; !ok {
		t.Error("path from older snapshot was discarded")
	}
	if _, ok := m.sessionPaths[newerID][20]; !ok {
		t.Error("path from newer snapshot was discarded")
	}
	if _, ok := m.sessionHashes[olderID]; ok {
		t.Error("superseded reverse session mapping was retained")
	}
	if _, ok := m.sessionSeen[olderID]; ok {
		t.Error("superseded last-seen value was retained")
	}
}

func TestMergeSameSessionSnapshot(t *testing.T) {
	id := zint.Uint128{1, 1}
	key := sessionKey("example.com:visitor")
	m := new(ms)
	m.Reset()

	for path, seen := range map[PathID]int64{10: 100, 20: 200} {
		m.mergeSessions(storedSession{
			Sessions: map[sessionKey]zint.Uint128{key: id},
			Paths:    map[zint.Uint128]map[PathID]struct{}{id: {path: {}}},
			Seen:     map[zint.Uint128]int64{id: seen},
		})
	}

	if len(m.sessionPaths[id]) != 2 {
		t.Errorf("paths = %v; want both snapshots", m.sessionPaths[id])
	}
	if m.sessionSeen[id] != 200 {
		t.Errorf("last seen = %d; want 200", m.sessionSeen[id])
	}
}
