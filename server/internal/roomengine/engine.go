package roomengine

import (
	"encoding/json"
	"errors"
	"sort"
	"sync"

	"github.com/openrtc/openrtc/server/internal/cluster"
)

var ErrRoomLimitExceeded = errors.New("room limit exceeded")

type Engine struct {
	mu        sync.RWMutex
	rooms     map[string]map[string]struct{}
	connRooms map[string]map[string]struct{}
	presence  map[string]map[string]json.RawMessage
	yjsRooms  map[string]map[string]struct{}
	yjsDocs   map[string]*memoryYJSDocument
}

type memoryYJSDocument struct {
	Snapshot           []byte
	SnapshotCheckpoint int64
	Updates            [][]byte
	UpdateSequences    []int64
	NextSequence       int64
}

type JoinResult struct {
	AlreadyJoined bool
}

type LeaveResult struct {
	Left bool
}

type Snapshot struct {
	Members  []string
	Presence map[string]json.RawMessage
}

func New() *Engine {
	return &Engine{
		rooms:     make(map[string]map[string]struct{}),
		connRooms: make(map[string]map[string]struct{}),
		presence:  make(map[string]map[string]json.RawMessage),
		yjsRooms:  make(map[string]map[string]struct{}),
		yjsDocs:   make(map[string]*memoryYJSDocument),
	}
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
		return JoinResult{AlreadyJoined: true}, nil
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
	return JoinResult{}, nil
}

func (e *Engine) Leave(connID string, room string) LeaveResult {
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
	return LeaveResult{Left: true}
}

func (e *Engine) Disconnect(connID string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()

	joinedRooms := e.connRooms[connID]
	rooms := make([]string, 0, len(joinedRooms))
	for room := range joinedRooms {
		rooms = append(rooms, room)
		e.removeRoomMemberLocked(connID, room)
		e.removePresenceLocked(connID, room)
	}
	delete(e.connRooms, connID)
	sort.Strings(rooms)
	return rooms
}

func (e *Engine) SetPresence(connID string, room string, payload json.RawMessage) {
	e.mu.Lock()
	defer e.mu.Unlock()

	roomPresence := e.presence[room]
	if roomPresence == nil {
		roomPresence = make(map[string]json.RawMessage)
		e.presence[room] = roomPresence
	}
	roomPresence[connID] = append(json.RawMessage(nil), payload...)
}

func (e *Engine) Snapshot(room string) Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()

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

	joinedRooms := e.connRooms[connID]
	rooms := make([]string, 0, len(joinedRooms))
	for room := range joinedRooms {
		rooms = append(rooms, room)
	}
	sort.Strings(rooms)
	return rooms
}

func (e *Engine) RegisterYJSConn(connID string, room string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	members := e.yjsRooms[room]
	if members == nil {
		members = make(map[string]struct{})
		e.yjsRooms[room] = members
	}
	members[connID] = struct{}{}
}

func (e *Engine) UnregisterYJSConn(connID string, room string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if members := e.yjsRooms[room]; members != nil {
		delete(members, connID)
		if len(members) == 0 {
			delete(e.yjsRooms, room)
		}
	}
}

func (e *Engine) YJSTargetIDs(room string, excludeConnID string) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

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
	return event
}

func (e *Engine) removeRoomMemberLocked(connID string, room string) {
	if members := e.rooms[room]; members != nil {
		delete(members, connID)
		if len(members) == 0 {
			delete(e.rooms, room)
		}
	}
}

func (e *Engine) removePresenceLocked(connID string, room string) {
	if roomPresence := e.presence[room]; roomPresence != nil {
		delete(roomPresence, connID)
		if len(roomPresence) == 0 {
			delete(e.presence, room)
		}
	}
}
