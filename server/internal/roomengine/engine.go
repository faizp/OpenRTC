package roomengine

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"sync"

	"github.com/openrtc/openrtc/server/internal/cluster"
)

var (
	ErrRoomLimitExceeded   = errors.New("room limit exceeded")
	ErrStorageMutationKind = errors.New("invalid storage mutation kind")
)

const (
	StorageMutationSet   = "set"
	StorageMutationPatch = "patch"

	MembershipMutationJoin  = "join"
	MembershipMutationLeave = "leave"
)

type Engine struct {
	mu          sync.RWMutex
	rooms       map[string]map[string]struct{}
	connRooms   map[string]map[string]struct{}
	sessions    map[string]SessionInfo
	presence    map[string]map[string]json.RawMessage
	yjsRooms    map[string]map[string]struct{}
	yjsSessions map[string]YJSSessionInfo
	yjsDocs     map[string]*memoryYJSDocument
	storage     map[string]json.RawMessage
}

type memoryYJSDocument struct {
	Snapshot           []byte
	SnapshotCheckpoint int64
	Updates            [][]byte
	UpdateSequences    []int64
	UpdateKinds        []cluster.YJSEventKind
	NextSequence       int64
}

type JoinResult struct {
	AlreadyJoined      bool
	Snapshot           Snapshot
	MembershipMutation *MembershipMutation
}

type JoinPlanOptions struct {
	SnapshotPage SnapshotPageOptions
	Replay       JoinReplayOptions
}

type JoinPlan struct {
	result          JoinResult
	snapshotOptions SnapshotPageOptions
	replayPlan      JoinReplayPlan
}

type LeaveResult struct {
	Left               bool
	PresenceFanout     *PresenceFanout
	MembershipMutation *MembershipMutation
}

type DisconnectResult struct {
	Rooms           []string
	PresenceFanouts []PresenceFanout
	Cleanup         *ConnectionCleanup
}

type Snapshot struct {
	Members  []string
	Presence map[string]json.RawMessage
}

type MembershipMutation struct {
	Kind   string
	ConnID string
	Room   string
}

type SessionInfo struct {
	ConnID  string
	Subject string
	Tenant  string
}

type YJSSessionInfo struct {
	ConnID  string
	Subject string
	Tenant  string
	Room    string
}

type ConnectionTouch struct {
	ConnID  string
	Subject string
	Tenant  string
}

type ConnectionCleanup struct {
	ConnID string
}

type SnapshotPageOptions struct {
	Limit  int
	Cursor string
}

type SnapshotPage struct {
	Members    []string                   `json:"members"`
	Presence   map[string]json.RawMessage `json:"presence"`
	NextCursor string                     `json:"next_cursor"`
}

type JoinReplayOptions struct {
	AfterSequence      uint64
	MaxEvents          int
	ExcludeConnID      string
	ExcludedEventNames []string
}

type JoinReplayPlan struct {
	AfterSequence      uint64
	MaxEvents          int
	ExcludeConnID      string
	excludedEventNames map[string]struct{}
}

type PresenceEventOptions struct {
	OriginNode string
}

type EventOptions struct {
	OriginNode          string
	TraceID             string
	ExcludeSenderConnID string
}

type StorageEventOptions struct {
	OriginNode          string
	ExcludeSenderConnID string
}

type YJSEventOptions struct {
	OriginNode   string
	OriginConnID string
}

type YJSEventPlan struct {
	Event           cluster.YJSEvent
	RequiresPublish bool
	Durable         bool
}

type PresenceFanout struct {
	Event         cluster.PresenceEvent
	TargetConnIDs []string
}

type EventFanout struct {
	Event         cluster.PublishedEvent
	TargetConnIDs []string
}

type StorageMutationOptions struct {
	MaxBytes     int
	OpID         string
	OriginConnID string
}

type StorageMutation struct {
	Kind         string                       `json:"kind"`
	OpID         string                       `json:"op_id,omitempty"`
	OriginConnID string                       `json:"origin_conn_id,omitempty"`
	Operations   []cluster.JSONPatchOperation `json:"operations,omitempty"`
	Document     json.RawMessage              `json:"document"`
}

type StorageFanout struct {
	Room          string
	Update        StorageMutation
	TargetConnIDs []string
}

type YJSFanout struct {
	Event         cluster.YJSEvent
	TargetConnIDs []string
}

type NotificationFanout struct {
	Event         cluster.PublishedEvent
	UserID        string
	TargetConnIDs []string
}

type ConnectionsSnapshot struct {
	Connections     []ConnectionSnapshot
	YJSConnections  []YJSConnectionSnapshot
	ActiveRoomCount int
}

type ConnectionSnapshot struct {
	ConnectionID string   `json:"connection_id"`
	Subject      string   `json:"subject,omitempty"`
	Tenant       string   `json:"tenant,omitempty"`
	Rooms        []string `json:"rooms"`
}

type YJSConnectionSnapshot struct {
	ConnectionID string `json:"connection_id"`
	Subject      string `json:"subject,omitempty"`
	Tenant       string `json:"tenant,omitempty"`
	Room         string `json:"room"`
}

func New() *Engine {
	return &Engine{
		rooms:       make(map[string]map[string]struct{}),
		connRooms:   make(map[string]map[string]struct{}),
		sessions:    make(map[string]SessionInfo),
		presence:    make(map[string]map[string]json.RawMessage),
		yjsRooms:    make(map[string]map[string]struct{}),
		yjsSessions: make(map[string]YJSSessionInfo),
		yjsDocs:     make(map[string]*memoryYJSDocument),
		storage:     make(map[string]json.RawMessage),
	}
}

func (e *Engine) RegisterSession(info SessionInfo) *ConnectionTouch {
	if info.ConnID == "" {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	e.sessions[info.ConnID] = info
	return newConnectionTouch(info)
}

func (e *Engine) TouchSession(connID string) *ConnectionTouch {
	e.mu.RLock()
	defer e.mu.RUnlock()

	info, ok := e.sessions[connID]
	if !ok {
		return nil
	}
	return newConnectionTouch(info)
}

func (e *Engine) Join(connID string, room string, roomLimit int) (JoinResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	joinedRooms := e.connRooms[connID]
	if joinedRooms == nil {
		joinedRooms = make(map[string]struct{})
		e.connRooms[connID] = joinedRooms
	}
	if _, exists := joinedRooms[room]; exists {
		return JoinResult{AlreadyJoined: true, Snapshot: e.snapshotLocked(room)}, nil
	}
	if roomLimit > 0 && len(joinedRooms) >= roomLimit {
		return JoinResult{}, ErrRoomLimitExceeded
	}

	joinedRooms[room] = struct{}{}
	members := e.rooms[room]
	if members == nil {
		members = make(map[string]struct{})
		e.rooms[room] = members
	}
	members[connID] = struct{}{}
	return JoinResult{
		Snapshot:           e.snapshotLocked(room),
		MembershipMutation: newMembershipMutation(MembershipMutationJoin, connID, room),
	}, nil
}

func (e *Engine) Leave(connID string, room string) LeaveResult {
	return e.leave(connID, room, PresenceEventOptions{}, false)
}

func (e *Engine) LeaveWithPresenceFanout(connID string, room string, options PresenceEventOptions) LeaveResult {
	return e.leave(connID, room, options, true)
}

func (e *Engine) leave(connID string, room string, options PresenceEventOptions, includeFanout bool) LeaveResult {
	e.mu.Lock()
	defer e.mu.Unlock()

	joinedRooms := e.connRooms[connID]
	if _, exists := joinedRooms[room]; !exists {
		return LeaveResult{}
	}
	delete(joinedRooms, room)
	if len(joinedRooms) == 0 {
		delete(e.connRooms, connID)
	}
	e.removeRoomMemberLocked(connID, room)
	e.removePresenceLocked(connID, room)
	result := LeaveResult{
		Left:               true,
		MembershipMutation: newMembershipMutation(MembershipMutationLeave, connID, room),
	}
	if includeFanout {
		fanout := PresenceFanout{
			Event:         NewOfflinePresenceEvent(connID, room, options),
			TargetConnIDs: e.memberIDsLocked(room, ""),
		}
		result.PresenceFanout = &fanout
	}
	return result
}

func (e *Engine) Disconnect(connID string) []string {
	rooms, _ := e.disconnect(connID, PresenceEventOptions{}, false)
	return rooms
}

func (e *Engine) DisconnectPresenceFanouts(connID string, options PresenceEventOptions) []PresenceFanout {
	_, fanouts := e.disconnect(connID, options, true)
	return fanouts
}

func (e *Engine) DisconnectSession(connID string, options PresenceEventOptions) DisconnectResult {
	rooms, fanouts := e.disconnect(connID, options, true)
	return DisconnectResult{
		Rooms:           rooms,
		PresenceFanouts: fanouts,
		Cleanup:         newConnectionCleanup(connID),
	}
}

func (e *Engine) disconnect(connID string, options PresenceEventOptions, includeFanouts bool) ([]string, []PresenceFanout) {
	e.mu.Lock()
	defer e.mu.Unlock()

	joinedRooms := e.connRooms[connID]
	rooms := make([]string, 0, len(joinedRooms))
	for room := range joinedRooms {
		rooms = append(rooms, room)
	}
	sort.Strings(rooms)

	fanouts := make([]PresenceFanout, 0, len(rooms))
	for _, room := range rooms {
		e.removeRoomMemberLocked(connID, room)
		e.removePresenceLocked(connID, room)
		if includeFanouts {
			fanouts = append(fanouts, PresenceFanout{
				Event:         NewOfflinePresenceEvent(connID, room, options),
				TargetConnIDs: e.memberIDsLocked(room, ""),
			})
		}
	}
	delete(e.connRooms, connID)
	delete(e.sessions, connID)
	return rooms, fanouts
}

func (e *Engine) SetPresence(connID string, room string, payload json.RawMessage) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.setPresenceLocked(connID, room, payload)
}

func (e *Engine) setPresenceLocked(connID string, room string, payload json.RawMessage) {
	roomPresence := e.presence[room]
	if roomPresence == nil {
		roomPresence = make(map[string]json.RawMessage)
		e.presence[room] = roomPresence
	}
	roomPresence[connID] = append(json.RawMessage(nil), payload...)
}

func (e *Engine) SetPresenceEvent(connID string, room string, payload json.RawMessage, options PresenceEventOptions) cluster.PresenceEvent {
	return e.SetPresenceFanout(connID, room, payload, options).Event
}

func (e *Engine) SetPresenceFanout(connID string, room string, payload json.RawMessage, options PresenceEventOptions) PresenceFanout {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.setPresenceLocked(connID, room, payload)
	return PresenceFanout{
		Event:         NewPresenceEvent(connID, room, payload, options),
		TargetConnIDs: e.memberIDsLocked(room, ""),
	}
}

func (e *Engine) PresenceFanout(event cluster.PresenceEvent) PresenceFanout {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return PresenceFanout{
		Event:         clonePresenceEvent(event),
		TargetConnIDs: e.memberIDsLocked(event.Room, ""),
	}
}

func (e *Engine) EventFanout(event cluster.PublishedEvent) EventFanout {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return EventFanout{
		Event:         clonePublishedEvent(event),
		TargetConnIDs: e.memberIDsLocked(event.Room, event.ExcludeSenderConnID),
	}
}

func NewPresenceEvent(connID string, room string, payload json.RawMessage, options PresenceEventOptions) cluster.PresenceEvent {
	return cluster.PresenceEvent{
		Room:       room,
		ConnID:     connID,
		State:      append(json.RawMessage(nil), payload...),
		OriginNode: options.OriginNode,
	}
}

func NewOfflinePresenceEvent(connID string, room string, options PresenceEventOptions) cluster.PresenceEvent {
	return cluster.PresenceEvent{
		Room:       room,
		ConnID:     connID,
		Offline:    true,
		OriginNode: options.OriginNode,
	}
}

func NewEvent(room string, eventName string, payload json.RawMessage, options EventOptions) cluster.PublishedEvent {
	return cluster.PublishedEvent{
		Room:                room,
		Event:               eventName,
		Payload:             append(json.RawMessage(nil), payload...),
		ExcludeSenderConnID: options.ExcludeSenderConnID,
		TraceID:             options.TraceID,
		OriginNode:          options.OriginNode,
	}
}

func NewStorageEvent(room string, update StorageMutation, options StorageEventOptions) (cluster.PublishedEvent, error) {
	payload, err := json.Marshal(cloneStorageMutation(update))
	if err != nil {
		return cluster.PublishedEvent{}, err
	}
	return cluster.PublishedEvent{
		Room:                room,
		Event:               cluster.EventStorageUpdate,
		Payload:             payload,
		ExcludeSenderConnID: options.ExcludeSenderConnID,
		OriginNode:          options.OriginNode,
	}, nil
}

func NewYJSEvent(room string, kind cluster.YJSEventKind, update []byte, options YJSEventOptions) cluster.YJSEvent {
	return cluster.YJSEvent{
		Room:         room,
		Kind:         kind,
		Update:       append([]byte(nil), update...),
		OriginNode:   options.OriginNode,
		OriginConnID: options.OriginConnID,
	}
}

func NewYJSEventPlan(room string, kind cluster.YJSEventKind, update []byte, options YJSEventOptions) (YJSEventPlan, bool) {
	if !validClientYJSEventKind(kind) {
		return YJSEventPlan{}, false
	}
	return YJSEventPlan{
		Event:           NewYJSEvent(room, kind, update, options),
		RequiresPublish: yjsEventRequiresPublish(kind),
		Durable:         durableYJSEventKind(kind),
	}, true
}

func (e *Engine) Snapshot(room string) Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.snapshotLocked(room)
}

func (result JoinResult) PageSnapshot(options SnapshotPageOptions) SnapshotPage {
	return PageSnapshot(result.Snapshot, options)
}

func NewJoinPlan(result JoinResult, options JoinPlanOptions) JoinPlan {
	return JoinPlan{
		result:          result,
		snapshotOptions: options.SnapshotPage,
		replayPlan:      NewJoinReplayPlan(options.Replay),
	}
}

func (plan JoinPlan) AlreadyJoined() bool {
	return plan.result.AlreadyJoined
}

func (plan JoinPlan) MembershipMutation() *MembershipMutation {
	if plan.result.MembershipMutation == nil {
		return nil
	}
	mutation := *plan.result.MembershipMutation
	return &mutation
}

func (plan JoinPlan) LocalSnapshotPage() SnapshotPage {
	return plan.SnapshotPage(plan.result.Snapshot)
}

func (plan JoinPlan) SnapshotPage(snapshot Snapshot) SnapshotPage {
	return PageSnapshot(snapshot, plan.snapshotOptions)
}

func (plan JoinPlan) ReplayLogRequest() (uint64, int, bool) {
	if !plan.replayPlan.Enabled() {
		return 0, 0, false
	}
	return plan.replayPlan.AfterSequence, plan.replayPlan.MaxEvents, true
}

func (plan JoinPlan) ReplayEvents(events []cluster.PublishedEvent) []cluster.PublishedEvent {
	return plan.replayPlan.ReplayEvents(events)
}

func NewJoinReplayPlan(options JoinReplayOptions) JoinReplayPlan {
	plan := JoinReplayPlan{
		AfterSequence: options.AfterSequence,
		MaxEvents:     options.MaxEvents,
		ExcludeConnID: options.ExcludeConnID,
	}
	if len(options.ExcludedEventNames) > 0 {
		plan.excludedEventNames = make(map[string]struct{}, len(options.ExcludedEventNames))
		for _, eventName := range options.ExcludedEventNames {
			plan.excludedEventNames[eventName] = struct{}{}
		}
	}
	return plan
}

func (plan JoinReplayPlan) Enabled() bool {
	return plan.AfterSequence > 0
}

func (plan JoinReplayPlan) ReplayEvents(events []cluster.PublishedEvent) []cluster.PublishedEvent {
	if len(events) == 0 {
		return nil
	}
	replayEvents := make([]cluster.PublishedEvent, 0, len(events))
	for _, event := range events {
		if plan.skipReplayEvent(event) {
			continue
		}
		replayEvents = append(replayEvents, clonePublishedEvent(event))
	}
	return replayEvents
}

func (plan JoinReplayPlan) skipReplayEvent(event cluster.PublishedEvent) bool {
	if plan.ExcludeConnID != "" && event.ExcludeSenderConnID == plan.ExcludeConnID {
		return true
	}
	_, excluded := plan.excludedEventNames[event.Event]
	return excluded
}

func PageSnapshot(snapshot Snapshot, options SnapshotPageOptions) SnapshotPage {
	sortedMembers := append([]string(nil), snapshot.Members...)
	sort.Strings(sortedMembers)

	start := 0
	if options.Cursor != "" {
		for index, member := range sortedMembers {
			if member == options.Cursor {
				start = index + 1
				break
			}
		}
	}

	limit := options.Limit
	if limit <= 0 || limit > len(sortedMembers) {
		limit = len(sortedMembers)
	}
	end := start + limit
	if end > len(sortedMembers) {
		end = len(sortedMembers)
	}

	members := append([]string(nil), sortedMembers[start:end]...)
	presence := make(map[string]json.RawMessage, len(members))
	for _, member := range members {
		if state, ok := snapshot.Presence[member]; ok {
			presence[member] = append(json.RawMessage(nil), state...)
		}
	}

	nextCursor := ""
	if end < len(sortedMembers) {
		nextCursor = sortedMembers[end-1]
	}

	return SnapshotPage{
		Members:    members,
		Presence:   presence,
		NextCursor: nextCursor,
	}
}

func (e *Engine) snapshotLocked(room string) Snapshot {
	members := make([]string, 0, len(e.rooms[room]))
	for connID := range e.rooms[room] {
		members = append(members, connID)
	}
	sort.Strings(members)

	presence := make(map[string]json.RawMessage, len(e.presence[room]))
	for connID, state := range e.presence[room] {
		presence[connID] = append(json.RawMessage(nil), state...)
	}

	return Snapshot{
		Members:  members,
		Presence: presence,
	}
}

func (e *Engine) MemberIDs(room string, excludeConnID string) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.memberIDsLocked(room, excludeConnID)
}

func (e *Engine) memberIDsLocked(room string, excludeConnID string) []string {
	members := make([]string, 0, len(e.rooms[room]))
	for connID := range e.rooms[room] {
		if excludeConnID != "" && connID == excludeConnID {
			continue
		}
		members = append(members, connID)
	}
	sort.Strings(members)
	return members
}

func (e *Engine) ActiveRoomCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.rooms)
}

func (e *Engine) JoinedRooms(connID string) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.joinedRoomsLocked(connID)
}

func (e *Engine) joinedRoomsLocked(connID string) []string {
	joinedRooms := e.connRooms[connID]
	rooms := make([]string, 0, len(joinedRooms))
	for room := range joinedRooms {
		rooms = append(rooms, room)
	}
	sort.Strings(rooms)
	return rooms
}

func (e *Engine) RegisterYJSConn(connID string, room string) {
	e.RegisterYJSSession(YJSSessionInfo{ConnID: connID, Room: room})
}

func (e *Engine) RegisterYJSSession(info YJSSessionInfo) {
	if info.ConnID == "" || info.Room == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if existing, ok := e.yjsSessions[info.ConnID]; ok && existing.Room != info.Room {
		e.removeYJSConnLocked(info.ConnID, existing.Room)
	}
	e.yjsSessions[info.ConnID] = info

	members := e.yjsRooms[info.Room]
	if members == nil {
		members = make(map[string]struct{})
		e.yjsRooms[info.Room] = members
	}
	members[info.ConnID] = struct{}{}
}

func (e *Engine) UnregisterYJSConn(connID string, room string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if existing, ok := e.yjsSessions[connID]; ok {
		if room == "" {
			room = existing.Room
		}
		if existing.Room != room {
			e.removeYJSConnLocked(connID, existing.Room)
		}
	}
	e.removeYJSConnLocked(connID, room)
	delete(e.yjsSessions, connID)
}

func (e *Engine) removeYJSConnLocked(connID string, room string) {
	if members := e.yjsRooms[room]; members != nil {
		delete(members, connID)
		if len(members) == 0 {
			delete(e.yjsRooms, room)
		}
	}
}

func (e *Engine) ConnectionsSnapshot() ConnectionsSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()

	connections := make([]ConnectionSnapshot, 0, len(e.sessions))
	for connID, session := range e.sessions {
		connections = append(connections, ConnectionSnapshot{
			ConnectionID: connID,
			Subject:      session.Subject,
			Tenant:       session.Tenant,
			Rooms:        e.joinedRoomsLocked(connID),
		})
	}
	sort.Slice(connections, func(i int, j int) bool {
		return connections[i].ConnectionID < connections[j].ConnectionID
	})

	yjsConnections := make([]YJSConnectionSnapshot, 0, len(e.yjsSessions))
	for connID, session := range e.yjsSessions {
		yjsConnections = append(yjsConnections, YJSConnectionSnapshot{
			ConnectionID: connID,
			Subject:      session.Subject,
			Tenant:       session.Tenant,
			Room:         session.Room,
		})
	}
	sort.Slice(yjsConnections, func(i int, j int) bool {
		return yjsConnections[i].ConnectionID < yjsConnections[j].ConnectionID
	})

	return ConnectionsSnapshot{
		Connections:     connections,
		YJSConnections:  yjsConnections,
		ActiveRoomCount: len(e.rooms),
	}
}

func (e *Engine) YJSTargetIDs(room string, excludeConnID string) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.yjsTargetIDsLocked(room, excludeConnID)
}

func (e *Engine) YJSFanout(event cluster.YJSEvent) YJSFanout {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return YJSFanout{
		Event:         cloneYJSEvent(event),
		TargetConnIDs: e.yjsTargetIDsLocked(event.Room, event.OriginConnID),
	}
}

func (e *Engine) NotificationFanout(event cluster.PublishedEvent, userID string) NotificationFanout {
	e.mu.RLock()
	defer e.mu.RUnlock()

	targets := make([]string, 0)
	if userID != "" {
		for connID, session := range e.sessions {
			if session.Subject == userID {
				targets = append(targets, connID)
			}
		}
	}
	sort.Strings(targets)

	return NotificationFanout{
		Event:         clonePublishedEvent(event),
		UserID:        userID,
		TargetConnIDs: targets,
	}
}

func (e *Engine) yjsTargetIDsLocked(room string, excludeConnID string) []string {
	members := make([]string, 0, len(e.yjsRooms[room]))
	for connID := range e.yjsRooms[room] {
		if excludeConnID != "" && connID == excludeConnID {
			continue
		}
		members = append(members, connID)
	}
	sort.Strings(members)
	return members
}

func (e *Engine) LoadYJSDocument(room string) cluster.YJSDocument {
	e.mu.RLock()
	defer e.mu.RUnlock()

	doc := e.yjsDocs[room]
	if doc == nil {
		return cluster.YJSDocument{}
	}

	out := cluster.YJSDocument{
		Snapshot:           append([]byte(nil), doc.Snapshot...),
		SnapshotCheckpoint: doc.SnapshotCheckpoint,
		Updates:            make([][]byte, 0, len(doc.Updates)),
		UpdateSequences:    append([]int64(nil), doc.UpdateSequences...),
		UpdateKinds:        append([]cluster.YJSEventKind(nil), doc.UpdateKinds...),
	}
	for _, update := range doc.Updates {
		out.Updates = append(out.Updates, append([]byte(nil), update...))
	}
	return out
}

func (e *Engine) StoreYJSEvent(event cluster.YJSEvent) cluster.YJSEvent {
	e.mu.Lock()
	defer e.mu.Unlock()

	doc := e.yjsDocs[event.Room]
	if doc == nil {
		doc = &memoryYJSDocument{}
		e.yjsDocs[event.Room] = doc
	}
	if event.Kind == cluster.YJSEventSnapshot {
		doc.Snapshot = append([]byte(nil), event.Update...)
		return event
	}

	doc.NextSequence++
	event.Sequence = doc.NextSequence
	doc.Updates = append(doc.Updates, append([]byte(nil), event.Update...))
	doc.UpdateSequences = append(doc.UpdateSequences, doc.NextSequence)
	doc.UpdateKinds = append(doc.UpdateKinds, event.Kind)
	return event
}

func (e *Engine) GetStorage(room string) (json.RawMessage, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	document := e.storage[room]
	if document == nil {
		return nil, cluster.ErrStorageNotFound
	}
	return append(json.RawMessage(nil), document...), nil
}

func (e *Engine) SetStorage(room string, document json.RawMessage, maxBytes int) (json.RawMessage, error) {
	compacted, err := compactJSON(document)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && len(compacted) > maxBytes {
		return nil, cluster.ErrStoragePatch
	}
	if err := cluster.ValidateStorageDocument(compacted); err != nil {
		return nil, err
	}

	e.mu.Lock()
	e.storage[room] = append(json.RawMessage(nil), compacted...)
	e.mu.Unlock()
	return append(json.RawMessage(nil), compacted...), nil
}

func (e *Engine) SetStorageMutation(room string, document json.RawMessage, options StorageMutationOptions) (StorageMutation, error) {
	stored, err := e.SetStorage(room, document, options.MaxBytes)
	if err != nil {
		return StorageMutation{}, err
	}
	return newStorageMutation(StorageMutationSet, stored, nil, options), nil
}

func (e *Engine) ApplyStoragePatch(room string, operations []cluster.JSONPatchOperation, maxBytes int) (json.RawMessage, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	current := e.storage[room]
	if current == nil {
		return nil, cluster.ErrStorageNotFound
	}
	patched, err := cluster.ApplyJSONPatch(current, operations)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && len(patched) > maxBytes {
		return nil, cluster.ErrStoragePatch
	}
	if err := cluster.ValidateStorageDocument(patched); err != nil {
		return nil, err
	}
	e.storage[room] = append(json.RawMessage(nil), patched...)
	return append(json.RawMessage(nil), patched...), nil
}

func (e *Engine) ApplyStoragePatchMutation(room string, operations []cluster.JSONPatchOperation, options StorageMutationOptions) (StorageMutation, error) {
	patched, err := e.ApplyStoragePatch(room, operations, options.MaxBytes)
	if err != nil {
		return StorageMutation{}, err
	}
	return newStorageMutation(StorageMutationPatch, patched, operations, options), nil
}

func (e *Engine) RecordStorageMutation(room string, kind string, document json.RawMessage, operations []cluster.JSONPatchOperation, options StorageMutationOptions) (StorageMutation, error) {
	if !validStorageMutationKind(kind) {
		return StorageMutation{}, ErrStorageMutationKind
	}
	stored, err := e.SetStorage(room, document, options.MaxBytes)
	if err != nil {
		return StorageMutation{}, err
	}
	return NewStorageMutation(kind, stored, operations, options)
}

func NewStorageMutation(kind string, document json.RawMessage, operations []cluster.JSONPatchOperation, options StorageMutationOptions) (StorageMutation, error) {
	if !validStorageMutationKind(kind) {
		return StorageMutation{}, ErrStorageMutationKind
	}
	if kind == StorageMutationSet {
		operations = nil
	}
	return newStorageMutation(kind, document, operations, options), nil
}

func (e *Engine) StorageFanout(room string, update StorageMutation, excludeConnID string) StorageFanout {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return StorageFanout{
		Room:          room,
		Update:        cloneStorageMutation(update),
		TargetConnIDs: e.memberIDsLocked(room, excludeConnID),
	}
}

func (e *Engine) removeRoomMemberLocked(connID string, room string) {
	if members := e.rooms[room]; members != nil {
		delete(members, connID)
		if len(members) == 0 {
			delete(e.rooms, room)
		}
	}
}

func validStorageMutationKind(kind string) bool {
	return kind == StorageMutationSet || kind == StorageMutationPatch
}

func validClientYJSEventKind(kind cluster.YJSEventKind) bool {
	return kind == cluster.YJSEventUpdate ||
		kind == cluster.YJSEventStateVectorRequest ||
		kind == cluster.YJSEventStateVectorDiff ||
		kind == cluster.YJSEventSubdocUpdate ||
		kind == cluster.YJSEventSubdocStateVector ||
		kind == cluster.YJSEventSubdocDiff
}

func yjsEventRequiresPublish(kind cluster.YJSEventKind) bool {
	return kind == cluster.YJSEventUpdate ||
		kind == cluster.YJSEventStateVectorDiff ||
		kind == cluster.YJSEventSubdocUpdate ||
		kind == cluster.YJSEventSubdocDiff
}

func durableYJSEventKind(kind cluster.YJSEventKind) bool {
	return kind == cluster.YJSEventUpdate || kind == cluster.YJSEventSubdocUpdate
}

func newStorageMutation(kind string, document json.RawMessage, operations []cluster.JSONPatchOperation, options StorageMutationOptions) StorageMutation {
	return StorageMutation{
		Kind:         kind,
		OpID:         options.OpID,
		OriginConnID: options.OriginConnID,
		Operations:   cloneStorageOperations(operations),
		Document:     append(json.RawMessage(nil), document...),
	}
}

func newMembershipMutation(kind string, connID string, room string) *MembershipMutation {
	return &MembershipMutation{
		Kind:   kind,
		ConnID: connID,
		Room:   room,
	}
}

func newConnectionCleanup(connID string) *ConnectionCleanup {
	return &ConnectionCleanup{ConnID: connID}
}

func newConnectionTouch(info SessionInfo) *ConnectionTouch {
	return &ConnectionTouch{
		ConnID:  info.ConnID,
		Subject: info.Subject,
		Tenant:  info.Tenant,
	}
}

func cloneStorageOperations(operations []cluster.JSONPatchOperation) []cluster.JSONPatchOperation {
	if len(operations) == 0 {
		return nil
	}
	cloned := make([]cluster.JSONPatchOperation, len(operations))
	for i, operation := range operations {
		cloned[i] = operation
		cloned[i].Value = append(json.RawMessage(nil), operation.Value...)
	}
	return cloned
}

func cloneStorageMutation(update StorageMutation) StorageMutation {
	return StorageMutation{
		Kind:         update.Kind,
		OpID:         update.OpID,
		OriginConnID: update.OriginConnID,
		Operations:   cloneStorageOperations(update.Operations),
		Document:     append(json.RawMessage(nil), update.Document...),
	}
}

func clonePresenceEvent(event cluster.PresenceEvent) cluster.PresenceEvent {
	event.State = append(json.RawMessage(nil), event.State...)
	return event
}

func clonePublishedEvent(event cluster.PublishedEvent) cluster.PublishedEvent {
	event.Payload = append(json.RawMessage(nil), event.Payload...)
	return event
}

func cloneYJSEvent(event cluster.YJSEvent) cluster.YJSEvent {
	event.Update = append([]byte(nil), event.Update...)
	return event
}

func compactJSON(raw json.RawMessage) (json.RawMessage, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), buf.Bytes()...), nil
}

func (e *Engine) removePresenceLocked(connID string, room string) {
	if roomPresence := e.presence[room]; roomPresence != nil {
		delete(roomPresence, connID)
		if len(roomPresence) == 0 {
			delete(e.presence, room)
		}
	}
}
