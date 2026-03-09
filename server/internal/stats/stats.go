package stats

type Snapshot struct {
	ActiveConnections    int64 `json:"active_connections"`
	ActiveRooms          int64 `json:"active_rooms"`
	JoinsTotal           int64 `json:"joins_total"`
	LeavesTotal          int64 `json:"leaves_total"`
	EventsTotal          int64 `json:"events_total"`
	PresenceUpdatesTotal int64 `json:"presence_updates_total"`
	QueueOverflowsTotal  int64 `json:"queue_overflows_total"`
	AdminPublishesTotal  int64 `json:"admin_publishes_total"`
}

func (s *Snapshot) Merge(other Snapshot) {
	s.ActiveConnections += other.ActiveConnections
	s.ActiveRooms += other.ActiveRooms
	s.JoinsTotal += other.JoinsTotal
	s.LeavesTotal += other.LeavesTotal
	s.EventsTotal += other.EventsTotal
	s.PresenceUpdatesTotal += other.PresenceUpdatesTotal
	s.QueueOverflowsTotal += other.QueueOverflowsTotal
	s.AdminPublishesTotal += other.AdminPublishesTotal
}
