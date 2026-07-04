package roomengine

import (
	"context"
	"errors"
	"testing"

	"github.com/openrtc/openrtc/server/internal/auth"
	"github.com/openrtc/openrtc/server/internal/cluster"
)

func TestAllowsRoomActionUsesTokenScopeBeforeRoomStore(t *testing.T) {
	store := &fakeRoomGetter{err: errors.New("should not load room")}
	claims := &auth.Claims{
		Tenant: "tenant-a",
		Scope:  "storage:read:tenant-a:*",
	}

	if !AllowsRoomAction(context.Background(), claims, store, "storage:read", "tenant-a:room-1", RoomAuthorizationOptions{EnforceTenantPrefix: true, TenantSeparator: ":"}) {
		t.Fatalf("expected token scope to allow storage read")
	}
	if store.calls != 0 {
		t.Fatalf("expected token scope to allow without room lookup, got %d lookups", store.calls)
	}
}

func TestAllowsRoomActionUsesRoomGrants(t *testing.T) {
	store := &fakeRoomGetter{
		record: cluster.RoomRecord{
			ID:              "tenant-a:room-1",
			DefaultAccesses: []string{cluster.PermissionStorageRead},
			UsersAccesses: map[string][]string{
				"storage-writer": {cluster.PermissionStorageWrite},
			},
			GroupsAccesses: map[string][]string{
				"commenters": {cluster.PermissionCommentsWrite},
			},
		},
	}

	tests := []struct {
		name   string
		claims *auth.Claims
		action string
		want   bool
	}{
		{
			name:   "default access",
			claims: testRoomClaims("tenant-a", "reader"),
			action: "storage:read",
			want:   true,
		},
		{
			name:   "user access",
			claims: testRoomClaims("tenant-a", "storage-writer"),
			action: "storage:write",
			want:   true,
		},
		{
			name:   "group access",
			claims: testRoomClaims("tenant-a", "commenter", "commenters"),
			action: "comments:read",
			want:   true,
		},
		{
			name:   "denied",
			claims: testRoomClaims("tenant-a", "reader"),
			action: "comments:write",
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AllowsRoomAction(context.Background(), tc.claims, store, tc.action, "tenant-a:room-1", RoomAuthorizationOptions{EnforceTenantPrefix: true, TenantSeparator: ":"})
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestAllowsRoomActionEnforcesTenantPrefixBeforeRoomGrants(t *testing.T) {
	store := &fakeRoomGetter{
		record: cluster.RoomRecord{
			ID: "tenant-a:room-1",
			UsersAccesses: map[string][]string{
				"publisher": {cluster.PermissionRoomWrite},
			},
		},
	}
	claims := testRoomClaims("tenant-b", "publisher")

	if AllowsRoomAction(context.Background(), claims, store, "publish", "tenant-a:room-1", RoomAuthorizationOptions{EnforceTenantPrefix: true, TenantSeparator: ":"}) {
		t.Fatalf("expected cross-tenant room grant to be denied")
	}
	if store.calls != 0 {
		t.Fatalf("expected tenant prefix denial before room lookup, got %d lookups", store.calls)
	}
}

func TestAllowsRoomActionDeniesMissingClaimsOrRoomLookupFailure(t *testing.T) {
	if AllowsRoomAction(context.Background(), nil, nil, "storage:read", "tenant-a:room-1", RoomAuthorizationOptions{}) {
		t.Fatalf("expected nil claims to be denied")
	}

	claims := testRoomClaims("tenant-a", "reader")
	if AllowsRoomAction(context.Background(), claims, nil, "storage:read", "tenant-a:room-1", RoomAuthorizationOptions{EnforceTenantPrefix: true, TenantSeparator: ":"}) {
		t.Fatalf("expected missing store to be denied")
	}
	if AllowsRoomAction(context.Background(), claims, &fakeRoomGetter{err: errors.New("load failed")}, "storage:read", "tenant-a:room-1", RoomAuthorizationOptions{EnforceTenantPrefix: true, TenantSeparator: ":"}) {
		t.Fatalf("expected room lookup failure to be denied")
	}
}

type fakeRoomGetter struct {
	record cluster.RoomRecord
	err    error
	calls  int
}

func testRoomClaims(tenant string, subject string, groupIDs ...string) *auth.Claims {
	claims := &auth.Claims{Tenant: tenant, GroupIDs: groupIDs}
	claims.Subject = subject
	return claims
}

func (s *fakeRoomGetter) GetRoom(context.Context, string) (cluster.RoomRecord, error) {
	s.calls++
	if s.err != nil {
		return cluster.RoomRecord{}, s.err
	}
	return s.record, nil
}
