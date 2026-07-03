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
	fanout := engine.SetPresenceFanout("conn-1", "room-a", payload, PresenceEventOptions{OriginNode: "node-a"})
	event := fanout.Event
	if event.Room != "room-a" || event.ConnID != "conn-1" || event.OriginNode != "node-a" || event.Offline {
		t.Fatalf("unexpected presence event metadata: %+v", event)
	}
	if !reflect.DeepEqual(fanout.TargetConnIDs, []string{"conn-1", "conn-2"}) {
		t.Fatalf("unexpected presence fanout targets: %#v", fanout.TargetConnIDs)
	}
	payload[12] = '9'
	if string(event.State) != `{"cursor":{"x":1}}` {
		t.Fatalf("presence event state should be copied, got %s", event.State)
	}

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
	remotePayload := json.RawMessage(`{"remote":true}`)
	remoteFanout := engine.PresenceFanout(cluster.PresenceEvent{
		Room:       "room-a",
		ConnID:     "remote-1",
		State:      remotePayload,
		OriginNode: "node-b",
	})
	if !reflect.DeepEqual(remoteFanout.TargetConnIDs, []string{"conn-1", "conn-2"}) {
		t.Fatalf("unexpected remote presence fanout targets: %#v", remoteFanout.TargetConnIDs)
	}
	remotePayload[10] = 'f'
	if string(remoteFanout.Event.State) != `{"remote":true}` {
		t.Fatalf("remote fanout event state should be copied, got %s", remoteFanout.Event.State)
	}

	left := engine.LeaveWithPresenceFanout("conn-1", "room-a", PresenceEventOptions{OriginNode: "node-a"})
	if !left.Left || left.PresenceFanout == nil {
		t.Fatalf("expected leave presence fanout, got %+v", left)
	}
	offline := left.PresenceFanout.Event
	if offline.Room != "room-a" || offline.ConnID != "conn-1" || offline.OriginNode != "node-a" || !offline.Offline || len(offline.State) != 0 {
		t.Fatalf("unexpected offline presence event: %+v", offline)
	}
	if !reflect.DeepEqual(left.PresenceFanout.TargetConnIDs, []string{"conn-2"}) {
		t.Fatalf("unexpected offline fanout targets: %#v", left.PresenceFanout.TargetConnIDs)
	}
	snapshot = engine.Snapshot("room-a")
	if _, ok := snapshot.Presence["conn-1"]; ok {
		t.Fatalf("presence should be removed after leave")
	}
}

func TestEngineDisconnectPresenceFanouts(t *testing.T) {
	engine := New()
	_, _ = engine.Join("conn-1", "room-b", 0)
	_, _ = engine.Join("conn-1", "room-a", 0)
	_, _ = engine.Join("conn-2", "room-a", 0)
	_, _ = engine.Join("conn-3", "room-b", 0)
	engine.SetPresence("conn-1", "room-a", json.RawMessage(`{"cursor":{"x":1}}`))
	engine.SetPresence("conn-1", "room-b", json.RawMessage(`{"cursor":{"x":2}}`))

	fanouts := engine.DisconnectPresenceFanouts("conn-1", PresenceEventOptions{OriginNode: "node-a"})
	if len(fanouts) != 2 {
		t.Fatalf("expected two disconnect fanouts, got %#v", fanouts)
	}
	if fanouts[0].Event.Room != "room-a" || fanouts[0].Event.ConnID != "conn-1" || !fanouts[0].Event.Offline {
		t.Fatalf("unexpected first disconnect fanout event: %+v", fanouts[0].Event)
	}
	if !reflect.DeepEqual(fanouts[0].TargetConnIDs, []string{"conn-2"}) {
		t.Fatalf("unexpected room-a disconnect targets: %#v", fanouts[0].TargetConnIDs)
	}
	if fanouts[1].Event.Room != "room-b" || fanouts[1].Event.ConnID != "conn-1" || !fanouts[1].Event.Offline {
		t.Fatalf("unexpected second disconnect fanout event: %+v", fanouts[1].Event)
	}
	if !reflect.DeepEqual(fanouts[1].TargetConnIDs, []string{"conn-3"}) {
		t.Fatalf("unexpected room-b disconnect targets: %#v", fanouts[1].TargetConnIDs)
	}
	if got := engine.JoinedRooms("conn-1"); len(got) != 0 {
		t.Fatalf("expected disconnected connection rooms to be cleared, got %#v", got)
	}
	if _, ok := engine.Snapshot("room-a").Presence["conn-1"]; ok {
		t.Fatalf("expected room-a presence to be removed")
	}
	if _, ok := engine.Snapshot("room-b").Presence["conn-1"]; ok {
		t.Fatalf("expected room-b presence to be removed")
	}
}

func TestEngineEventAndStorageFanouts(t *testing.T) {
	engine := New()
	_, _ = engine.Join("conn-sender", "room-a", 0)
	_, _ = engine.Join("conn-b", "room-a", 0)
	_, _ = engine.Join("conn-a", "room-a", 0)

	payload := json.RawMessage(`{"ok":true}`)
	eventFanout := engine.EventFanout(cluster.PublishedEvent{
		Room:                "room-a",
		Event:               "doc.update",
		Payload:             payload,
		ExcludeSenderConnID: "conn-sender",
		OriginNode:          "node-a",
		TraceID:             "trace-1",
		Sequence:            7,
	})
	if !reflect.DeepEqual(eventFanout.TargetConnIDs, []string{"conn-a", "conn-b"}) {
		t.Fatalf("unexpected event fanout targets: %#v", eventFanout.TargetConnIDs)
	}
	if eventFanout.Event.Event != "doc.update" || eventFanout.Event.TraceID != "trace-1" || eventFanout.Event.Sequence != 7 {
		t.Fatalf("unexpected event fanout metadata: %+v", eventFanout.Event)
	}
	payload[0] = '['
	if string(eventFanout.Event.Payload) != `{"ok":true}` {
		t.Fatalf("event fanout payload should be copied, got %s", eventFanout.Event.Payload)
	}

	update := StorageMutation{
		Kind:         StorageMutationPatch,
		OpID:         "op-patch",
		OriginConnID: "conn-sender",
		Operations: []cluster.JSONPatchOperation{
			{Op: "replace", Path: "/data/title", Value: json.RawMessage(`"Published"`)},
		},
		Document: json.RawMessage(`{"liveblocksType":"LiveObject","data":{"title":"Published"}}`),
	}
	storageFanout := engine.StorageFanout("room-a", update, "conn-sender")
	if storageFanout.Room != "room-a" || !reflect.DeepEqual(storageFanout.TargetConnIDs, []string{"conn-a", "conn-b"}) {
		t.Fatalf("unexpected storage fanout: %+v", storageFanout)
	}
	if storageFanout.Update.Kind != StorageMutationPatch || storageFanout.Update.OpID != "op-patch" || len(storageFanout.Update.Operations) != 1 {
		t.Fatalf("unexpected storage fanout update: %+v", storageFanout.Update)
	}
	update.Document[0] = '['
	update.Operations[0].Value[1] = 'X'
	if string(storageFanout.Update.Document) != `{"liveblocksType":"LiveObject","data":{"title":"Published"}}` {
		t.Fatalf("storage fanout document should be copied, got %s", storageFanout.Update.Document)
	}
	if string(storageFanout.Update.Operations[0].Value) != `"Published"` {
		t.Fatalf("storage fanout operations should be copied, got %#v", storageFanout.Update.Operations)
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

func TestEngineStorageMutations(t *testing.T) {
	engine := New()
	options := StorageMutationOptions{
		OpID:         "op-set",
		OriginConnID: "conn-1",
	}
	setMutation, err := engine.SetStorageMutation("room-a", json.RawMessage(`{
		"liveblocksType":"LiveObject",
		"data":{"title":"Draft"}
	}`), options)
	if err != nil {
		t.Fatalf("set storage mutation: %v", err)
	}
	if setMutation.Kind != StorageMutationSet || setMutation.OpID != "op-set" || setMutation.OriginConnID != "conn-1" {
		t.Fatalf("unexpected set mutation metadata: %+v", setMutation)
	}
	if len(setMutation.Operations) != 0 {
		t.Fatalf("set mutation should not include operations: %#v", setMutation.Operations)
	}
	if string(setMutation.Document) != `{"liveblocksType":"LiveObject","data":{"title":"Draft"}}` {
		t.Fatalf("unexpected set mutation document: %s", setMutation.Document)
	}
	setMutation.Document[0] = 'X'
	loaded, err := engine.GetStorage("room-a")
	if err != nil {
		t.Fatalf("get storage after set mutation: %v", err)
	}
	if string(loaded) != `{"liveblocksType":"LiveObject","data":{"title":"Draft"}}` {
		t.Fatalf("mutation document should be defensively copied, got %s", loaded)
	}

	operations := []cluster.JSONPatchOperation{
		{Op: "replace", Path: "/data/title", Value: json.RawMessage(`"Published"`)},
	}
	patchMutation, err := engine.ApplyStoragePatchMutation("room-a", operations, StorageMutationOptions{
		OpID:         "op-patch",
		OriginConnID: "conn-1",
	})
	if err != nil {
		t.Fatalf("patch storage mutation: %v", err)
	}
	if patchMutation.Kind != StorageMutationPatch || patchMutation.OpID != "op-patch" || len(patchMutation.Operations) != 1 {
		t.Fatalf("unexpected patch mutation metadata: %+v", patchMutation)
	}
	operations[0].Value = json.RawMessage(`"Changed"`)
	if string(patchMutation.Operations[0].Value) != `"Published"` {
		t.Fatalf("patch mutation operations should be defensively copied: %#v", patchMutation.Operations)
	}

	constructed, err := NewStorageMutation(StorageMutationPatch, json.RawMessage(`{"title":"Constructed"}`), patchMutation.Operations, StorageMutationOptions{OpID: "op-constructed"})
	if err != nil {
		t.Fatalf("new storage mutation: %v", err)
	}
	if constructed.Kind != StorageMutationPatch || constructed.OpID != "op-constructed" || string(constructed.Document) != `{"title":"Constructed"}` {
		t.Fatalf("unexpected constructed mutation: %+v", constructed)
	}

	recorded, err := engine.RecordStorageMutation("room-a", StorageMutationPatch, json.RawMessage(`{
		"liveblocksType":"LiveObject",
		"data":{"title":"Remote"}
	}`), patchMutation.Operations, StorageMutationOptions{OpID: "op-remote"})
	if err != nil {
		t.Fatalf("record storage mutation: %v", err)
	}
	if recorded.Kind != StorageMutationPatch || recorded.OpID != "op-remote" || string(recorded.Document) != `{"liveblocksType":"LiveObject","data":{"title":"Remote"}}` {
		t.Fatalf("unexpected recorded mutation: %+v", recorded)
	}
	loaded, err = engine.GetStorage("room-a")
	if err != nil {
		t.Fatalf("get storage after record mutation: %v", err)
	}
	if string(loaded) != `{"liveblocksType":"LiveObject","data":{"title":"Remote"}}` {
		t.Fatalf("recorded mutation should update storage, got %s", loaded)
	}

	if _, err := engine.RecordStorageMutation("room-a", "unknown", json.RawMessage(`{}`), nil, StorageMutationOptions{}); !errors.Is(err, ErrStorageMutationKind) {
		t.Fatalf("expected invalid mutation kind error, got %v", err)
	}
	if _, err := NewStorageMutation("unknown", json.RawMessage(`{}`), nil, StorageMutationOptions{}); !errors.Is(err, ErrStorageMutationKind) {
		t.Fatalf("expected invalid constructor mutation kind error, got %v", err)
	}
}
