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
	if result.MembershipMutation == nil || *result.MembershipMutation != (MembershipMutation{Kind: MembershipMutationJoin, ConnID: "conn-1", Room: "room-a"}) {
		t.Fatalf("unexpected first join membership mutation: %+v", result.MembershipMutation)
	}
	if !reflect.DeepEqual(result.Snapshot.Members, []string{"conn-1"}) {
		t.Fatalf("unexpected first join snapshot members: %#v", result.Snapshot.Members)
	}
	result, err = engine.Join("conn-1", "room-a", 2)
	if err != nil {
		t.Fatalf("duplicate join room-a: %v", err)
	}
	if !result.AlreadyJoined {
		t.Fatalf("expected duplicate join")
	}
	if result.MembershipMutation != nil {
		t.Fatalf("duplicate join should not produce membership mutation: %+v", result.MembershipMutation)
	}
	if !reflect.DeepEqual(result.Snapshot.Members, []string{"conn-1"}) {
		t.Fatalf("unexpected duplicate join snapshot members: %#v", result.Snapshot.Members)
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
	if left.MembershipMutation == nil || *left.MembershipMutation != (MembershipMutation{Kind: MembershipMutationLeave, ConnID: "conn-1", Room: "room-a"}) {
		t.Fatalf("unexpected leave membership mutation: %+v", left.MembershipMutation)
	}
	left = engine.Leave("conn-1", "room-a")
	if left.Left {
		t.Fatalf("duplicate leave should not report left")
	}
	if left.MembershipMutation != nil {
		t.Fatalf("duplicate leave should not produce membership mutation: %+v", left.MembershipMutation)
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

func TestEngineJoinResultSnapshotCopiesRoomState(t *testing.T) {
	engine := New()
	if _, err := engine.Join("conn-1", "room-a", 0); err != nil {
		t.Fatalf("join conn-1: %v", err)
	}
	engine.SetPresence("conn-1", "room-a", json.RawMessage(`{"cursor":{"x":1}}`))

	result, err := engine.Join("conn-2", "room-a", 0)
	if err != nil {
		t.Fatalf("join conn-2: %v", err)
	}
	if result.AlreadyJoined {
		t.Fatalf("conn-2 should be a new join")
	}
	if !reflect.DeepEqual(result.Snapshot.Members, []string{"conn-1", "conn-2"}) {
		t.Fatalf("unexpected join snapshot members: %#v", result.Snapshot.Members)
	}
	if string(result.Snapshot.Presence["conn-1"]) != `{"cursor":{"x":1}}` {
		t.Fatalf("unexpected join snapshot presence: %s", result.Snapshot.Presence["conn-1"])
	}
	page := result.PageSnapshot(SnapshotPageOptions{Limit: 1})
	if !reflect.DeepEqual(page.Members, []string{"conn-1"}) || page.NextCursor != "conn-1" {
		t.Fatalf("unexpected join result snapshot page: %#v next=%q", page.Members, page.NextCursor)
	}

	result.Snapshot.Members[0] = "changed"
	result.Snapshot.Presence["conn-1"][12] = '9'
	snapshot := engine.Snapshot("room-a")
	if !reflect.DeepEqual(snapshot.Members, []string{"conn-1", "conn-2"}) {
		t.Fatalf("join snapshot members should be copied, engine has %#v", snapshot.Members)
	}
	if string(snapshot.Presence["conn-1"]) != `{"cursor":{"x":1}}` {
		t.Fatalf("join snapshot presence should be copied, engine has %s", snapshot.Presence["conn-1"])
	}
}

func TestPageSnapshot(t *testing.T) {
	presenceState := json.RawMessage(`{"online":true}`)
	page := PageSnapshot(Snapshot{
		Members: []string{"c3", "c1", "c2"},
		Presence: map[string]json.RawMessage{
			"c1": presenceState,
			"c2": json.RawMessage(`{"online":false}`),
		},
	}, SnapshotPageOptions{Limit: 2})
	if !reflect.DeepEqual(page.Members, []string{"c1", "c2"}) || page.NextCursor != "c2" {
		t.Fatalf("unexpected page: %#v next=%q", page.Members, page.NextCursor)
	}
	if len(page.Presence) != 2 {
		t.Fatalf("expected presence subset, got %#v", page.Presence)
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal page: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("unmarshal page payload: %v", err)
	}
	if _, ok := payload["members"]; !ok {
		t.Fatalf("page should serialize members with wire key, got %s", encoded)
	}
	if _, ok := payload["next_cursor"]; !ok {
		t.Fatalf("page should serialize next cursor with wire key, got %s", encoded)
	}
	if _, ok := payload["NextCursor"]; ok {
		t.Fatalf("page should not serialize Go field names, got %s", encoded)
	}
	page.Presence["c1"][10] = 'f'
	if string(presenceState) != `{"online":true}` {
		t.Fatalf("page presence should be copied, source has %s", presenceState)
	}

	page = PageSnapshot(Snapshot{
		Members: []string{"c3", "c1", "c2"},
		Presence: map[string]json.RawMessage{
			"c3": json.RawMessage(`{"online":true}`),
		},
	}, SnapshotPageOptions{Cursor: "c2"})
	if !reflect.DeepEqual(page.Members, []string{"c3"}) || page.NextCursor != "" {
		t.Fatalf("unexpected cursor page: %#v next=%q", page.Members, page.NextCursor)
	}
	if len(page.Presence) != 1 || string(page.Presence["c3"]) != `{"online":true}` {
		t.Fatalf("unexpected cursor presence: %#v", page.Presence)
	}
}

func TestJoinReplayPlanFiltersEvents(t *testing.T) {
	plan := NewJoinReplayPlan(JoinReplayOptions{
		AfterSequence:      7,
		MaxEvents:          100,
		ExcludeConnID:      "conn-self",
		ExcludedEventNames: []string{"openrtc.storage.update", "openrtc.notifications.inbox.created"},
	})
	if !plan.Enabled() || plan.AfterSequence != 7 || plan.MaxEvents != 100 {
		t.Fatalf("unexpected enabled replay plan: %+v", plan)
	}

	payload := json.RawMessage(`{"ok":true}`)
	events := plan.ReplayEvents([]cluster.PublishedEvent{
		{Room: "room-a", Event: "doc.created", Payload: json.RawMessage(`{"ok":"listed-by-store"}`), Sequence: 7},
		{Room: "room-a", Event: "doc.update", Payload: payload, Sequence: 8, TraceID: "trace-8"},
		{Room: "room-a", Event: "openrtc.storage.update", Payload: json.RawMessage(`{"kind":"set"}`), Sequence: 9},
		{Room: "room-a", Event: "openrtc.notifications.inbox.created", Payload: json.RawMessage(`{"kind":"notification"}`), Sequence: 10},
		{Room: "room-a", Event: "self", Payload: json.RawMessage(`{"self":true}`), Sequence: 11, ExcludeSenderConnID: "conn-self"},
	})
	if len(events) != 2 {
		t.Fatalf("expected two replayable events, got %#v", events)
	}
	if events[0].Event != "doc.created" || events[1].Event != "doc.update" || events[1].TraceID != "trace-8" {
		t.Fatalf("unexpected replay events: %#v", events)
	}
	payload[0] = '['
	if string(events[1].Payload) != `{"ok":true}` {
		t.Fatalf("replay event payload should be copied, got %s", events[1].Payload)
	}
}

func TestJoinReplayPlanDisabledWithoutSequence(t *testing.T) {
	plan := NewJoinReplayPlan(JoinReplayOptions{})
	if plan.Enabled() {
		t.Fatalf("zero after sequence should disable replay")
	}
	if events := plan.ReplayEvents(nil); events != nil {
		t.Fatalf("empty event list should stay nil, got %#v", events)
	}
}

func TestJoinPlanCombinesJoinSnapshotAndReplayContracts(t *testing.T) {
	result := JoinResult{
		Snapshot: Snapshot{
			Members: []string{"conn-2", "conn-1"},
			Presence: map[string]json.RawMessage{
				"conn-1": json.RawMessage(`{"cursor":{"x":1}}`),
			},
		},
		MembershipMutation: &MembershipMutation{Kind: MembershipMutationJoin, ConnID: "conn-1", Room: "room-a"},
	}
	plan := NewJoinPlan(result, JoinPlanOptions{
		SnapshotPage: SnapshotPageOptions{Limit: 1},
		Replay: JoinReplayOptions{
			AfterSequence:      8,
			MaxEvents:          20,
			ExcludeConnID:      "conn-1",
			ExcludedEventNames: []string{"internal"},
		},
	})

	if plan.AlreadyJoined() {
		t.Fatalf("plan should expose join duplicate state")
	}
	mutation := plan.MembershipMutation()
	if mutation == nil || *mutation != (MembershipMutation{Kind: MembershipMutationJoin, ConnID: "conn-1", Room: "room-a"}) {
		t.Fatalf("unexpected membership mutation: %+v", mutation)
	}
	mutation.ConnID = "changed"
	if again := plan.MembershipMutation(); again == nil || again.ConnID != "conn-1" {
		t.Fatalf("membership mutation should be copied, got %+v", again)
	}

	page := plan.LocalSnapshotPage()
	if !reflect.DeepEqual(page.Members, []string{"conn-1"}) || page.NextCursor != "conn-1" {
		t.Fatalf("unexpected local snapshot page: %#v next=%q", page.Members, page.NextCursor)
	}
	storePage := plan.SnapshotPage(Snapshot{
		Members: []string{"store-2", "store-1"},
		Presence: map[string]json.RawMessage{
			"store-1": json.RawMessage(`{"store":true}`),
		},
	})
	if !reflect.DeepEqual(storePage.Members, []string{"store-1"}) || storePage.NextCursor != "store-1" {
		t.Fatalf("unexpected store snapshot page: %#v next=%q", storePage.Members, storePage.NextCursor)
	}

	afterSequence, maxEvents, ok := plan.ReplayLogRequest()
	if !ok || afterSequence != 8 || maxEvents != 20 {
		t.Fatalf("unexpected replay request after=%d max=%d ok=%v", afterSequence, maxEvents, ok)
	}
	events := plan.ReplayEvents([]cluster.PublishedEvent{
		{Room: "room-a", Event: "doc.update", Payload: json.RawMessage(`{"ok":true}`), Sequence: 9},
		{Room: "room-a", Event: "internal", Payload: json.RawMessage(`{"skip":true}`), Sequence: 10},
		{Room: "room-a", Event: "self", Payload: json.RawMessage(`{"skip":true}`), Sequence: 11, ExcludeSenderConnID: "conn-1"},
	})
	if len(events) != 1 || events[0].Event != "doc.update" {
		t.Fatalf("unexpected replay events: %#v", events)
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

func TestEngineDisconnectSessionIncludesCleanupIntent(t *testing.T) {
	engine := New()
	_, _ = engine.Join("conn-1", "room-b", 0)
	_, _ = engine.Join("conn-1", "room-a", 0)
	_, _ = engine.Join("conn-2", "room-a", 0)
	_, _ = engine.Join("conn-3", "room-b", 0)
	engine.SetPresence("conn-1", "room-a", json.RawMessage(`{"cursor":{"x":1}}`))
	engine.SetPresence("conn-1", "room-b", json.RawMessage(`{"cursor":{"x":2}}`))

	result := engine.DisconnectSession("conn-1", PresenceEventOptions{OriginNode: "node-a"})
	if !reflect.DeepEqual(result.Rooms, []string{"room-a", "room-b"}) {
		t.Fatalf("unexpected disconnected rooms: %#v", result.Rooms)
	}
	if result.Cleanup == nil || result.Cleanup.ConnID != "conn-1" {
		t.Fatalf("unexpected cleanup intent: %+v", result.Cleanup)
	}
	if len(result.PresenceFanouts) != 2 {
		t.Fatalf("expected two disconnect fanouts, got %#v", result.PresenceFanouts)
	}
	if result.PresenceFanouts[0].Event.Room != "room-a" || result.PresenceFanouts[0].Event.ConnID != "conn-1" || !result.PresenceFanouts[0].Event.Offline {
		t.Fatalf("unexpected first disconnect fanout event: %+v", result.PresenceFanouts[0].Event)
	}
	if !reflect.DeepEqual(result.PresenceFanouts[0].TargetConnIDs, []string{"conn-2"}) {
		t.Fatalf("unexpected room-a disconnect targets: %#v", result.PresenceFanouts[0].TargetConnIDs)
	}
	if result.PresenceFanouts[1].Event.Room != "room-b" || result.PresenceFanouts[1].Event.ConnID != "conn-1" || !result.PresenceFanouts[1].Event.Offline {
		t.Fatalf("unexpected second disconnect fanout event: %+v", result.PresenceFanouts[1].Event)
	}
	if !reflect.DeepEqual(result.PresenceFanouts[1].TargetConnIDs, []string{"conn-3"}) {
		t.Fatalf("unexpected room-b disconnect targets: %#v", result.PresenceFanouts[1].TargetConnIDs)
	}
	if got := engine.JoinedRooms("conn-1"); len(got) != 0 {
		t.Fatalf("expected disconnected connection rooms to be cleared, got %#v", got)
	}
}

func TestEngineDisconnectSessionCleanupWithoutRooms(t *testing.T) {
	engine := New()
	result := engine.DisconnectSession("conn-empty", PresenceEventOptions{OriginNode: "node-a"})
	if len(result.Rooms) != 0 || len(result.PresenceFanouts) != 0 {
		t.Fatalf("unexpected empty disconnect result: %+v", result)
	}
	if result.Cleanup == nil || result.Cleanup.ConnID != "conn-empty" {
		t.Fatalf("expected cleanup intent for active connection metadata, got %+v", result.Cleanup)
	}
}

func TestEngineEventAndStorageFanouts(t *testing.T) {
	engine := New()
	_, _ = engine.Join("conn-sender", "room-a", 0)
	_, _ = engine.Join("conn-b", "room-a", 0)
	_, _ = engine.Join("conn-a", "room-a", 0)

	payload := json.RawMessage(`{"ok":true}`)
	event := NewEvent("room-a", "doc.update", payload, EventOptions{
		ExcludeSenderConnID: "conn-sender",
		OriginNode:          "node-a",
		TraceID:             "trace-1",
	})
	event.Sequence = 7
	if event.Room != "room-a" || event.Event != "doc.update" || event.OriginNode != "node-a" || event.TraceID != "trace-1" || event.ExcludeSenderConnID != "conn-sender" {
		t.Fatalf("unexpected constructed event metadata: %+v", event)
	}
	payload[0] = '['
	if string(event.Payload) != `{"ok":true}` {
		t.Fatalf("constructed event payload should be copied, got %s", event.Payload)
	}

	eventFanout := engine.EventFanout(event)
	if !reflect.DeepEqual(eventFanout.TargetConnIDs, []string{"conn-a", "conn-b"}) {
		t.Fatalf("unexpected event fanout targets: %#v", eventFanout.TargetConnIDs)
	}
	if eventFanout.Event.Event != "doc.update" || eventFanout.Event.TraceID != "trace-1" || eventFanout.Event.Sequence != 7 {
		t.Fatalf("unexpected event fanout metadata: %+v", eventFanout.Event)
	}
	event.Payload[0] = '['
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

func TestNewClusterEventPlan(t *testing.T) {
	cases := []struct {
		name      string
		event     cluster.PublishedEvent
		localNode string
		wantKind  ClusterEventKind
	}{
		{
			name: "same node skips",
			event: cluster.PublishedEvent{
				Room:       "room-a",
				Event:      "doc.update",
				Payload:    json.RawMessage(`{"kind":"same-node"}`),
				OriginNode: "node-a",
				Sequence:   7,
			},
			localNode: "node-a",
			wantKind:  ClusterEventSkip,
		},
		{
			name: "storage update",
			event: cluster.PublishedEvent{
				Room:       "room-a",
				Event:      cluster.EventStorageUpdate,
				Payload:    json.RawMessage(`{"kind":"set"}`),
				OriginNode: "node-b",
			},
			localNode: "node-a",
			wantKind:  ClusterEventStorage,
		},
		{
			name: "notification delta",
			event: cluster.PublishedEvent{
				Room:       "notifications:user-1",
				Event:      NotificationInboxCreated,
				Payload:    json.RawMessage(`{"userId":"user-1"}`),
				OriginNode: "node-b",
			},
			localNode: "node-a",
			wantKind:  ClusterEventNotification,
		},
		{
			name: "room event",
			event: cluster.PublishedEvent{
				Room:                "room-a",
				Event:               "doc.update",
				Payload:             json.RawMessage(`{"ok":true}`),
				ExcludeSenderConnID: "conn-sender",
				OriginNode:          "node-b",
				TraceID:             "trace-1",
				Sequence:            9,
			},
			localNode: "node-a",
			wantKind:  ClusterEventRoom,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantPayload := append(json.RawMessage(nil), tc.event.Payload...)
			plan := NewClusterEventPlan(tc.event, tc.localNode)
			if plan.Kind != tc.wantKind {
				t.Fatalf("unexpected cluster event kind: got %q want %q", plan.Kind, tc.wantKind)
			}
			if plan.Event.Room != tc.event.Room || plan.Event.Event != tc.event.Event || plan.Event.OriginNode != tc.event.OriginNode || plan.Event.Sequence != tc.event.Sequence {
				t.Fatalf("unexpected planned event envelope: %+v", plan.Event)
			}
			if string(plan.Event.Payload) != string(wantPayload) {
				t.Fatalf("unexpected planned event payload: %s", plan.Event.Payload)
			}
			if len(tc.event.Payload) > 0 {
				tc.event.Payload[0] = '['
				if string(plan.Event.Payload) != string(wantPayload) {
					t.Fatalf("planned cluster event payload should be copied, got %s", plan.Event.Payload)
				}
			}
		})
	}

	for _, eventName := range []string{NotificationInboxCreated, NotificationInboxRead, NotificationInboxDeleted, NotificationInboxDeletedAll} {
		if !IsNotificationEvent(eventName) {
			t.Fatalf("expected notification event %q", eventName)
		}
	}
	if IsNotificationEvent("doc.update") {
		t.Fatalf("doc.update should not be treated as a notification event")
	}
}

func TestNewClusterPresenceAndYJSPlans(t *testing.T) {
	presenceState := json.RawMessage(`{"cursor":{"x":1}}`)
	presence := cluster.PresenceEvent{
		Room:       "room-a",
		ConnID:     "conn-1",
		State:      presenceState,
		OriginNode: "node-b",
	}
	presencePlan := NewClusterPresencePlan(presence, "node-a")
	if !presencePlan.Deliver {
		t.Fatalf("remote presence should be delivered")
	}
	if presencePlan.Event.Room != "room-a" || presencePlan.Event.ConnID != "conn-1" || presencePlan.Event.OriginNode != "node-b" {
		t.Fatalf("unexpected presence plan event: %+v", presencePlan.Event)
	}
	presenceState[0] = '['
	if string(presencePlan.Event.State) != `{"cursor":{"x":1}}` {
		t.Fatalf("presence plan state should be copied, got %s", presencePlan.Event.State)
	}
	sameNodePresence := NewClusterPresencePlan(cluster.PresenceEvent{
		Room:       "room-a",
		ConnID:     "conn-1",
		OriginNode: "node-a",
	}, "node-a")
	if sameNodePresence.Deliver {
		t.Fatalf("same-node presence should not be delivered")
	}

	yjsUpdate := []byte("subdoc-update")
	yjsPlan := NewClusterYJSEventPlan(cluster.YJSEvent{
		Room:         "room-a",
		Kind:         cluster.YJSEventSubdocUpdate,
		Update:       yjsUpdate,
		OriginNode:   "node-b",
		OriginConnID: "conn-1",
		Sequence:     12,
	}, "node-a")
	if !yjsPlan.Deliver {
		t.Fatalf("remote yjs event should be delivered")
	}
	if yjsPlan.Event.Kind != cluster.YJSEventSubdocUpdate || yjsPlan.Event.Sequence != 12 || yjsPlan.Event.OriginConnID != "conn-1" {
		t.Fatalf("unexpected yjs plan event: %+v", yjsPlan.Event)
	}
	yjsUpdate[0] = 'X'
	if string(yjsPlan.Event.Update) != "subdoc-update" {
		t.Fatalf("yjs plan update should be copied, got %q", yjsPlan.Event.Update)
	}
	sameNodeYJS := NewClusterYJSEventPlan(cluster.YJSEvent{
		Room:       "room-a",
		Kind:       cluster.YJSEventUpdate,
		OriginNode: "node-a",
	}, "node-a")
	if sameNodeYJS.Deliver {
		t.Fatalf("same-node yjs event should not be delivered")
	}
}

func TestEngineNotificationFanoutTargetsSessionsBySubject(t *testing.T) {
	engine := New()
	touch := engine.RegisterSession(SessionInfo{ConnID: "conn-b", Subject: "user-1", Tenant: "tenant-a"})
	if touch == nil || *touch != (ConnectionTouch{ConnID: "conn-b", Subject: "user-1", Tenant: "tenant-a"}) {
		t.Fatalf("unexpected register touch intent: %+v", touch)
	}
	if touch := engine.TouchSession("conn-b"); touch == nil || *touch != (ConnectionTouch{ConnID: "conn-b", Subject: "user-1", Tenant: "tenant-a"}) {
		t.Fatalf("unexpected session touch intent: %+v", touch)
	}
	if touch := engine.TouchSession("missing"); touch != nil {
		t.Fatalf("missing session should not produce touch intent: %+v", touch)
	}
	engine.RegisterSession(SessionInfo{ConnID: "conn-a", Subject: "user-1", Tenant: "tenant-a"})
	engine.RegisterSession(SessionInfo{ConnID: "conn-other", Subject: "user-2", Tenant: "tenant-a"})
	engine.RegisterSession(SessionInfo{ConnID: "conn-anonymous", Tenant: "tenant-a"})

	payload := json.RawMessage(`{"type":"created","userId":"user-1"}`)
	fanout := engine.NotificationFanout(cluster.PublishedEvent{
		Room:       "notifications:user-1",
		Event:      "openrtc.notifications.inbox.created",
		Payload:    payload,
		OriginNode: "admin:node-b",
		Sequence:   12,
	}, "user-1")
	if fanout.UserID != "user-1" || !reflect.DeepEqual(fanout.TargetConnIDs, []string{"conn-a", "conn-b"}) {
		t.Fatalf("unexpected notification fanout: %+v", fanout)
	}
	if fanout.Event.Event != "openrtc.notifications.inbox.created" || fanout.Event.Sequence != 12 {
		t.Fatalf("unexpected notification event metadata: %+v", fanout.Event)
	}
	payload[0] = '['
	if string(fanout.Event.Payload) != `{"type":"created","userId":"user-1"}` {
		t.Fatalf("notification fanout payload should be copied, got %s", fanout.Event.Payload)
	}
	fanout = engine.NotificationFanout(cluster.PublishedEvent{Event: "openrtc.notifications.inbox.created"}, "")
	if len(fanout.TargetConnIDs) != 0 {
		t.Fatalf("empty notification user should not target anonymous sessions, got %#v", fanout.TargetConnIDs)
	}

	disconnect := engine.DisconnectSession("conn-a", PresenceEventOptions{OriginNode: "node-a"})
	if disconnect.Cleanup == nil || disconnect.Cleanup.ConnID != "conn-a" {
		t.Fatalf("unexpected disconnect cleanup: %+v", disconnect.Cleanup)
	}
	if touch := engine.TouchSession("conn-a"); touch != nil {
		t.Fatalf("disconnected session should not produce touch intent: %+v", touch)
	}
	fanout = engine.NotificationFanout(cluster.PublishedEvent{Event: "openrtc.notifications.inbox.read"}, "user-1")
	if !reflect.DeepEqual(fanout.TargetConnIDs, []string{"conn-b"}) {
		t.Fatalf("expected disconnected session to be removed from notification targets, got %#v", fanout.TargetConnIDs)
	}
}

func TestEngineConnectionsSnapshot(t *testing.T) {
	engine := New()
	engine.RegisterSession(SessionInfo{ConnID: "conn-b", Subject: "user-b", Tenant: "tenant-a"})
	engine.RegisterSession(SessionInfo{ConnID: "conn-a", Subject: "user-a", Tenant: "tenant-a"})
	_, _ = engine.Join("conn-a", "room-2", 0)
	_, _ = engine.Join("conn-a", "room-1", 0)
	_, _ = engine.Join("conn-b", "room-1", 0)
	engine.RegisterYJSSession(YJSSessionInfo{ConnID: "yjs-b", Subject: "editor-b", Tenant: "tenant-a", Room: "doc-2"})
	engine.RegisterYJSSession(YJSSessionInfo{ConnID: "yjs-a", Subject: "editor-a", Tenant: "tenant-a", Room: "doc-1"})

	snapshot := engine.ConnectionsSnapshot()
	if snapshot.ActiveRoomCount != 2 {
		t.Fatalf("unexpected active room count: %d", snapshot.ActiveRoomCount)
	}
	if len(snapshot.Connections) != 2 || snapshot.Connections[0].ConnectionID != "conn-a" || snapshot.Connections[1].ConnectionID != "conn-b" {
		t.Fatalf("connections should be sorted by id: %+v", snapshot.Connections)
	}
	if snapshot.Connections[0].Subject != "user-a" || snapshot.Connections[0].Tenant != "tenant-a" {
		t.Fatalf("unexpected conn-a session metadata: %+v", snapshot.Connections[0])
	}
	if !reflect.DeepEqual(snapshot.Connections[0].Rooms, []string{"room-1", "room-2"}) {
		t.Fatalf("unexpected conn-a rooms: %#v", snapshot.Connections[0].Rooms)
	}
	if len(snapshot.YJSConnections) != 2 || snapshot.YJSConnections[0].ConnectionID != "yjs-a" || snapshot.YJSConnections[1].ConnectionID != "yjs-b" {
		t.Fatalf("yjs connections should be sorted by id: %+v", snapshot.YJSConnections)
	}
	if snapshot.YJSConnections[0].Subject != "editor-a" || snapshot.YJSConnections[0].Tenant != "tenant-a" || snapshot.YJSConnections[0].Room != "doc-1" {
		t.Fatalf("unexpected yjs-a session metadata: %+v", snapshot.YJSConnections[0])
	}

	snapshot.Connections[0].Rooms[0] = "changed"
	again := engine.ConnectionsSnapshot()
	if !reflect.DeepEqual(again.Connections[0].Rooms, []string{"room-1", "room-2"}) {
		t.Fatalf("connection snapshot rooms should be copied, got %#v", again.Connections[0].Rooms)
	}

	engine.DisconnectSession("conn-a", PresenceEventOptions{OriginNode: "node-a"})
	engine.UnregisterYJSConn("yjs-a", "doc-1")
	again = engine.ConnectionsSnapshot()
	if len(again.Connections) != 1 || again.Connections[0].ConnectionID != "conn-b" {
		t.Fatalf("expected disconnected json session to be removed, got %+v", again.Connections)
	}
	if len(again.YJSConnections) != 1 || again.YJSConnections[0].ConnectionID != "yjs-b" {
		t.Fatalf("expected unregistered yjs session to be removed, got %+v", again.YJSConnections)
	}
}

func TestEngineYJSRoomsAndDocuments(t *testing.T) {
	engine := New()
	engine.RegisterYJSConn("conn-1", "room-a")
	engine.RegisterYJSConn("conn-2", "room-a")
	engine.RegisterYJSConn("conn-3", "room-a")

	if got := engine.YJSTargetIDs("room-a", "conn-1"); !reflect.DeepEqual(got, []string{"conn-2", "conn-3"}) {
		t.Fatalf("unexpected yjs targets: %#v", got)
	}
	fanoutUpdate := []byte("fanout-update")
	fanout := engine.YJSFanout(cluster.YJSEvent{
		Room:         "room-a",
		Kind:         cluster.YJSEventSubdocDiff,
		Update:       fanoutUpdate,
		OriginConnID: "conn-2",
		OriginNode:   "node-a",
		Sequence:     9,
	})
	if !reflect.DeepEqual(fanout.TargetConnIDs, []string{"conn-1", "conn-3"}) {
		t.Fatalf("unexpected yjs fanout targets: %#v", fanout.TargetConnIDs)
	}
	if fanout.Event.Kind != cluster.YJSEventSubdocDiff || fanout.Event.Sequence != 9 || fanout.Event.OriginNode != "node-a" {
		t.Fatalf("unexpected yjs fanout event: %+v", fanout.Event)
	}
	fanoutUpdate[0] = 'X'
	if string(fanout.Event.Update) != "fanout-update" {
		t.Fatalf("yjs fanout update should be copied, got %q", fanout.Event.Update)
	}
	engine.UnregisterYJSConn("conn-2", "room-a")
	if got := engine.YJSTargetIDs("room-a", "conn-1"); !reflect.DeepEqual(got, []string{"conn-3"}) {
		t.Fatalf("unexpected yjs targets after unregister: %#v", got)
	}
	engine.UnregisterYJSConn("conn-3", "room-a")
	if got := engine.YJSTargetIDs("room-a", "conn-1"); len(got) != 0 {
		t.Fatalf("expected no yjs targets after unregistering peers, got %#v", got)
	}

	update := []byte("update-1")
	event, err := engine.StoreYJSEvent(cluster.YJSEvent{
		Room:   "room-a",
		Kind:   cluster.YJSEventUpdate,
		Update: update,
	})
	if err != nil {
		t.Fatalf("store yjs update: %v", err)
	}
	if event.Sequence != 1 {
		t.Fatalf("expected first sequence to be 1, got %d", event.Sequence)
	}
	update[0] = 'X'
	second, err := engine.StoreYJSEvent(cluster.YJSEvent{
		Room:   "room-a",
		Kind:   cluster.YJSEventSubdocUpdate,
		Update: []byte("update-2"),
	})
	if err != nil {
		t.Fatalf("store yjs subdoc update: %v", err)
	}
	if second.Sequence != 2 {
		t.Fatalf("expected second sequence to be 2, got %d", second.Sequence)
	}
	if _, err := engine.StoreYJSEvent(cluster.YJSEvent{
		Room:   "room-a",
		Kind:   cluster.YJSEventSnapshot,
		Update: []byte("snapshot-1"),
	}); err != nil {
		t.Fatalf("store yjs snapshot: %v", err)
	}
	if _, err := engine.StoreYJSEvent(cluster.YJSEvent{
		Room:   "room-a",
		Kind:   cluster.YJSEventStateVectorDiff,
		Update: []byte("transient"),
	}); !errors.Is(err, ErrYJSPersistenceKind) {
		t.Fatalf("expected transient yjs event persistence error, got %v", err)
	}

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

func TestNewYJSEvent(t *testing.T) {
	update := []byte("subdoc-diff")
	event := NewYJSEvent("room-a", cluster.YJSEventSubdocDiff, update, YJSEventOptions{
		OriginNode:   "node-a",
		OriginConnID: "conn-1",
	})
	if event.Room != "room-a" || event.Kind != cluster.YJSEventSubdocDiff || event.OriginNode != "node-a" || event.OriginConnID != "conn-1" {
		t.Fatalf("unexpected yjs event envelope: %+v", event)
	}
	update[0] = 'X'
	if string(event.Update) != "subdoc-diff" {
		t.Fatalf("yjs event update should be copied, got %q", event.Update)
	}
}

func TestNewYJSEventPlan(t *testing.T) {
	cases := []struct {
		name            string
		kind            cluster.YJSEventKind
		requiresPublish bool
		durable         bool
	}{
		{name: "update", kind: cluster.YJSEventUpdate, requiresPublish: true, durable: true},
		{name: "state vector request", kind: cluster.YJSEventStateVectorRequest},
		{name: "state vector diff", kind: cluster.YJSEventStateVectorDiff, requiresPublish: true},
		{name: "subdoc update", kind: cluster.YJSEventSubdocUpdate, requiresPublish: true, durable: true},
		{name: "subdoc state vector", kind: cluster.YJSEventSubdocStateVector},
		{name: "subdoc diff", kind: cluster.YJSEventSubdocDiff, requiresPublish: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			update := []byte("update")
			plan, ok := NewYJSEventPlan("room-a", tc.kind, update, YJSEventOptions{
				OriginNode:   "node-a",
				OriginConnID: "conn-1",
			})
			if !ok {
				t.Fatalf("expected valid yjs event plan")
			}
			if plan.RequiresPublish != tc.requiresPublish || plan.Durable != tc.durable {
				t.Fatalf("unexpected yjs plan flags: %+v", plan)
			}
			if plan.Event.Room != "room-a" || plan.Event.Kind != tc.kind || plan.Event.OriginNode != "node-a" || plan.Event.OriginConnID != "conn-1" {
				t.Fatalf("unexpected yjs plan event: %+v", plan.Event)
			}
			update[0] = 'X'
			if string(plan.Event.Update) != "update" {
				t.Fatalf("yjs plan event update should be copied, got %q", plan.Event.Update)
			}
		})
	}

	for _, kind := range []cluster.YJSEventKind{cluster.YJSEventSnapshot, cluster.YJSEventKind(99)} {
		if _, ok := NewYJSEventPlan("room-a", kind, []byte("update"), YJSEventOptions{}); ok {
			t.Fatalf("expected invalid yjs event plan for kind %d", kind)
		}
	}
}

func TestNewYJSPersistencePlan(t *testing.T) {
	cases := []struct {
		name string
		kind cluster.YJSEventKind
		mode string
	}{
		{name: "update", kind: cluster.YJSEventUpdate, mode: YJSPersistenceAppendUpdate},
		{name: "subdoc update", kind: cluster.YJSEventSubdocUpdate, mode: YJSPersistenceAppendUpdate},
		{name: "snapshot", kind: cluster.YJSEventSnapshot, mode: YJSPersistenceStoreSnapshot},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			update := []byte("persist")
			plan, err := NewYJSPersistencePlan(cluster.YJSEvent{
				Room:         "room-a",
				Kind:         tc.kind,
				Update:       update,
				OriginNode:   "node-a",
				OriginConnID: "conn-1",
			})
			if err != nil {
				t.Fatalf("new yjs persistence plan: %v", err)
			}
			if plan.Mode != tc.mode {
				t.Fatalf("unexpected persistence mode: %q", plan.Mode)
			}
			update[0] = 'X'
			if string(plan.Event.Update) != "persist" {
				t.Fatalf("persistence plan event update should be copied, got %q", plan.Event.Update)
			}
			sequenced := plan.WithSequence(42)
			if sequenced.Sequence != 42 || string(sequenced.Update) != "persist" {
				t.Fatalf("unexpected sequenced event: %+v", sequenced)
			}
			sequenced.Update[0] = 'X'
			if string(plan.Event.Update) != "persist" {
				t.Fatalf("sequenced event should be copied, got %q", plan.Event.Update)
			}
		})
	}

	for _, kind := range []cluster.YJSEventKind{cluster.YJSEventStateVectorRequest, cluster.YJSEventStateVectorDiff, cluster.YJSEventSubdocStateVector, cluster.YJSEventSubdocDiff} {
		if _, err := NewYJSPersistencePlan(cluster.YJSEvent{Room: "room-a", Kind: kind, Update: []byte("transient")}); !errors.Is(err, ErrYJSPersistenceKind) {
			t.Fatalf("expected persistence kind error for kind %d, got %v", kind, err)
		}
	}
}

func TestNewYJSDocumentFrames(t *testing.T) {
	document := cluster.YJSDocument{
		Snapshot:    []byte("snapshot"),
		Updates:     [][]byte{[]byte("update"), []byte("subdoc-update")},
		UpdateKinds: []cluster.YJSEventKind{cluster.YJSEventUpdate, cluster.YJSEventSubdocUpdate},
	}
	frames := NewYJSDocumentFrames(document)
	if len(frames) != 3 {
		t.Fatalf("expected three frames, got %#v", frames)
	}
	if frames[0].Kind != cluster.YJSEventSnapshot || string(frames[0].Update) != "snapshot" {
		t.Fatalf("unexpected snapshot frame: %+v", frames[0])
	}
	if frames[1].Kind != cluster.YJSEventUpdate || string(frames[1].Update) != "update" {
		t.Fatalf("unexpected update frame: %+v", frames[1])
	}
	if frames[2].Kind != cluster.YJSEventSubdocUpdate || string(frames[2].Update) != "subdoc-update" {
		t.Fatalf("unexpected subdoc frame: %+v", frames[2])
	}
	document.Snapshot[0] = 'X'
	document.Updates[0][0] = 'X'
	if string(frames[0].Update) != "snapshot" || string(frames[1].Update) != "update" {
		t.Fatalf("document frames should copy source bytes: %#v", frames)
	}

	fallbackFrames := NewYJSDocumentFrames(cluster.YJSDocument{
		Updates:     [][]byte{[]byte("legacy")},
		UpdateKinds: []cluster.YJSEventKind{cluster.YJSEventSubdocUpdate, cluster.YJSEventUpdate},
	})
	if len(fallbackFrames) != 1 || fallbackFrames[0].Kind != cluster.YJSEventUpdate || string(fallbackFrames[0].Update) != "legacy" {
		t.Fatalf("unexpected fallback frame: %#v", fallbackFrames)
	}

	if frames := NewYJSDocumentFrames(cluster.YJSDocument{}); len(frames) != 0 {
		t.Fatalf("empty document should produce no frames, got %#v", frames)
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

func TestNewStorageEvent(t *testing.T) {
	update := StorageMutation{
		Kind:         StorageMutationPatch,
		OpID:         "op-patch",
		OriginConnID: "conn-1",
		Operations: []cluster.JSONPatchOperation{
			{Op: "replace", Path: "/data/title", Value: json.RawMessage(`"Published"`)},
		},
		Document: json.RawMessage(`{"liveblocksType":"LiveObject","data":{"title":"Published"}}`),
	}
	event, err := NewStorageEvent("room-a", update, StorageEventOptions{
		OriginNode:          "node-a",
		ExcludeSenderConnID: "conn-1",
	})
	if err != nil {
		t.Fatalf("new storage event: %v", err)
	}
	if event.Room != "room-a" || event.Event != cluster.EventStorageUpdate || event.OriginNode != "node-a" || event.ExcludeSenderConnID != "conn-1" {
		t.Fatalf("unexpected storage event envelope: %+v", event)
	}

	update.Document[0] = '['
	update.Operations[0].Value = json.RawMessage(`"Changed"`)
	var decoded StorageMutation
	if err := json.Unmarshal(event.Payload, &decoded); err != nil {
		t.Fatalf("decode storage event payload: %v", err)
	}
	if decoded.Kind != StorageMutationPatch || decoded.OpID != "op-patch" || decoded.OriginConnID != "conn-1" {
		t.Fatalf("unexpected storage event metadata: %+v", decoded)
	}
	if len(decoded.Operations) != 1 || decoded.Operations[0].Op != "replace" || string(decoded.Operations[0].Value) != `"Published"` {
		t.Fatalf("unexpected storage event operations: %+v", decoded.Operations)
	}
	if string(decoded.Document) != `{"liveblocksType":"LiveObject","data":{"title":"Published"}}` {
		t.Fatalf("unexpected storage event document: %s", decoded.Document)
	}
}
