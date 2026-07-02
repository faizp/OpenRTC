package roomengine

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/openrtc/openrtc/server/internal/cluster"
)

func TestEngineJoinLeaveAndDisconnect(t *testing.T) {
	engine := New()

	result, err := engine.Join("conn-1", "room-a", 2)
	if err != nil {
		t.Fatalf("join room-a: %v", err)
	}
	if result.AlreadyJoined {
		t.Fatalf("first join should not be duplicate")
	}
	result, err = engine.Join("conn-1", "room-a", 2)
	if err != nil {
		t.Fatalf("duplicate join room-a: %v", err)
	}
	if !result.AlreadyJoined {
		t.Fatalf("expected duplicate join")
	}
	if _, err := engine.Join("conn-1", "room-b", 2); err != nil {
		t.Fatalf("join room-b: %v", err)
	}
	if _, err := engine.Join("conn-1", "room-c", 2); !errors.Is(err, ErrRoomLimitExceeded) {
		t.Fatalf("expected room limit error, got %v", err)
	}
	if got := engine.ActiveRoomCount(); got != 2 {
		t.Fatalf("expected 2 active rooms, got %d", got)
	}
	if got := engine.JoinedRooms("conn-1"); !reflect.DeepEqual(got, []string{"room-a", "room-b"}) {
		t.Fatalf("unexpected joined rooms: %#v", got)
	}

	left := engine.Leave("conn-1", "room-a")
	if !left.Left {
		t.Fatalf("expected leave to report left")
	}
	left = engine.Leave("conn-1", "room-a")
	if left.Left {
		t.Fatalf("duplicate leave should not report left")
	}
	if got := engine.JoinedRooms("conn-1"); !reflect.DeepEqual(got, []string{"room-b"}) {
		t.Fatalf("unexpected joined rooms after leave: %#v", got)
	}

	rooms := engine.Disconnect("conn-1")
	if !reflect.DeepEqual(rooms, []string{"room-b"}) {
		t.Fatalf("unexpected disconnected rooms: %#v", rooms)
	}
	if got := engine.ActiveRoomCount(); got != 0 {
		t.Fatalf("expected no active rooms, got %d", got)
	}
}

func TestEnginePresenceSnapshotAndTargets(t *testing.T) {
	engine := New()
	_, _ = engine.Join("conn-1", "room-a", 0)
	_, _ = engine.Join("conn-2", "room-a", 0)

	payload := json.RawMessage(`{"cursor":{"x":1}}`)
	engine.SetPresence("conn-1", "room-a", payload)
	payload[12] = '9'

	snapshot := engine.Snapshot("room-a")
	if !reflect.DeepEqual(snapshot.Members, []string{"conn-1", "conn-2"}) {
		t.Fatalf("unexpected members: %#v", snapshot.Members)
	}
	if string(snapshot.Presence["conn-1"]) != `{"cursor":{"x":1}}` {
		t.Fatalf("presence should be copied, got %s", snapshot.Presence["conn-1"])
	}
	snapshot.Presence["conn-1"][12] = '8'
	again := engine.Snapshot("room-a")
	if string(again.Presence["conn-1"]) != `{"cursor":{"x":1}}` {
		t.Fatalf("snapshot mutation should not affect engine, got %s", again.Presence["conn-1"])
	}

	if got := engine.MemberIDs("room-a", "conn-1"); !reflect.DeepEqual(got, []string{"conn-2"}) {
		t.Fatalf("unexpected target ids: %#v", got)
	}
	engine.Leave("conn-1", "room-a")
	snapshot = engine.Snapshot("room-a")
	if _, ok := snapshot.Presence["conn-1"]; ok {
		t.Fatalf("presence should be removed after leave")
	}
}

func TestEngineYJSRoomsAndDocuments(t *testing.T) {
	engine := New()
	engine.RegisterYJSConn("conn-1", "room-a")
	engine.RegisterYJSConn("conn-2", "room-a")

	if got := engine.YJSTargetIDs("room-a", "conn-1"); !reflect.DeepEqual(got, []string{"conn-2"}) {
		t.Fatalf("unexpected yjs targets: %#v", got)
	}
	engine.UnregisterYJSConn("conn-2", "room-a")
	if got := engine.YJSTargetIDs("room-a", "conn-1"); len(got) != 0 {
		t.Fatalf("expected no yjs targets after unregister, got %#v", got)
	}

	update := []byte("update-1")
	event := engine.StoreYJSEvent(cluster.YJSEvent{
		Room:   "room-a",
		Kind:   cluster.YJSEventUpdate,
		Update: update,
	})
	if event.Sequence != 1 {
		t.Fatalf("expected first sequence to be 1, got %d", event.Sequence)
	}
	update[0] = 'X'
	second := engine.StoreYJSEvent(cluster.YJSEvent{
		Room:   "room-a",
		Kind:   cluster.YJSEventSubdocUpdate,
		Update: []byte("update-2"),
	})
	if second.Sequence != 2 {
		t.Fatalf("expected second sequence to be 2, got %d", second.Sequence)
	}
	engine.StoreYJSEvent(cluster.YJSEvent{
		Room:   "room-a",
		Kind:   cluster.YJSEventSnapshot,
		Update: []byte("snapshot-1"),
	})

	doc := engine.LoadYJSDocument("room-a")
	if string(doc.Snapshot) != "snapshot-1" {
		t.Fatalf("unexpected snapshot: %q", doc.Snapshot)
	}
	if len(doc.Updates) != 2 || string(doc.Updates[0]) != "update-1" || string(doc.Updates[1]) != "update-2" {
		t.Fatalf("unexpected updates: %#v", doc.Updates)
	}
	if !reflect.DeepEqual(doc.UpdateSequences, []int64{1, 2}) {
		t.Fatalf("unexpected update sequences: %#v", doc.UpdateSequences)
	}
	if !reflect.DeepEqual(doc.UpdateKinds, []cluster.YJSEventKind{cluster.YJSEventUpdate, cluster.YJSEventSubdocUpdate}) {
		t.Fatalf("unexpected update kinds: %#v", doc.UpdateKinds)
	}

	doc.Snapshot[0] = 'X'
	doc.Updates[0][0] = 'X'
	doc.UpdateSequences[0] = 99
	doc.UpdateKinds[0] = cluster.YJSEventSnapshot
	reloaded := engine.LoadYJSDocument("room-a")
	if string(reloaded.Snapshot) != "snapshot-1" || string(reloaded.Updates[0]) != "update-1" || reloaded.UpdateSequences[0] != 1 || reloaded.UpdateKinds[0] != cluster.YJSEventUpdate {
		t.Fatalf("document load should return defensive copies, got %+v", reloaded)
	}
}

func TestEngineStorageSetGetAndPatch(t *testing.T) {
	engine := New()
	if _, err := engine.GetStorage("room-a"); !errors.Is(err, cluster.ErrStorageNotFound) {
		t.Fatalf("expected missing storage, got %v", err)
	}
	if _, err := engine.SetStorage("room-a", json.RawMessage(`[]`), 0); !errors.Is(err, cluster.ErrStoragePatch) {
		t.Fatalf("expected invalid storage root error, got %v", err)
	}

	stored, err := engine.SetStorage("room-a", json.RawMessage(`{
		"liveblocksType":"LiveObject",
		"data":{"title":"Draft","items":{"liveblocksType":"LiveList","data":["a"]}}
	}`), 0)
	if err != nil {
		t.Fatalf("set typed storage: %v", err)
	}
	if string(stored) != `{"liveblocksType":"LiveObject","data":{"title":"Draft","items":{"liveblocksType":"LiveList","data":["a"]}}}` {
		t.Fatalf("unexpected compacted storage: %s", stored)
	}
	stored[0] = 'X'
	loaded, err := engine.GetStorage("room-a")
	if err != nil {
		t.Fatalf("get storage: %v", err)
	}
	if string(loaded) != `{"liveblocksType":"LiveObject","data":{"title":"Draft","items":{"liveblocksType":"LiveList","data":["a"]}}}` {
		t.Fatalf("storage should be defensively copied, got %s", loaded)
	}

	patched, err := engine.ApplyStoragePatch("room-a", []cluster.JSONPatchOperation{
		{Op: "replace", Path: "/data/title", Value: json.RawMessage(`"Published"`)},
		{Op: "add", Path: "/data/items/data/-", Value: json.RawMessage(`"b"`)},
	}, 0)
	if err != nil {
		t.Fatalf("patch storage: %v", err)
	}
	if string(patched) != `{"data":{"items":{"data":["a","b"],"liveblocksType":"LiveList"},"title":"Published"},"liveblocksType":"LiveObject"}` {
		t.Fatalf("unexpected patched storage: %s", patched)
	}

	if _, err := engine.ApplyStoragePatch("missing", []cluster.JSONPatchOperation{
		{Op: "add", Path: "/title", Value: json.RawMessage(`"Draft"`)},
	}, 0); !errors.Is(err, cluster.ErrStorageNotFound) {
		t.Fatalf("expected missing patch error, got %v", err)
	}
}
