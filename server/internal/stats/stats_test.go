package stats

import "testing"

func TestSnapshotMergeAddsEveryCounter(t *testing.T) {
	base := Snapshot{
		ActiveConnections:    1,
		ActiveRooms:          2,
		JoinsTotal:           3,
		LeavesTotal:          4,
		EventsTotal:          5,
		PresenceUpdatesTotal: 6,
		QueueOverflowsTotal:  7,
		AdminPublishesTotal:  8,
	}
	base.Merge(Snapshot{
		ActiveConnections:    10,
		ActiveRooms:          20,
		JoinsTotal:           30,
		LeavesTotal:          40,
		EventsTotal:          50,
		PresenceUpdatesTotal: 60,
		QueueOverflowsTotal:  70,
		AdminPublishesTotal:  80,
	})

	want := Snapshot{
		ActiveConnections:    11,
		ActiveRooms:          22,
		JoinsTotal:           33,
		LeavesTotal:          44,
		EventsTotal:          55,
		PresenceUpdatesTotal: 66,
		QueueOverflowsTotal:  77,
		AdminPublishesTotal:  88,
	}
	if base != want {
		t.Fatalf("unexpected merged snapshot: got %+v want %+v", base, want)
	}
}
