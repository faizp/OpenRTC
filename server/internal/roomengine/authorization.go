package roomengine

import (
	"context"
	"strings"

	"github.com/openrtc/openrtc/server/internal/auth"
	"github.com/openrtc/openrtc/server/internal/cluster"
)

type RoomGetter interface {
	GetRoom(context.Context, string) (cluster.RoomRecord, error)
}

type RoomAuthorizationOptions struct {
	EnforceTenantPrefix bool
	TenantSeparator     string
}

func AllowsRoomAction(ctx context.Context, claims *auth.Claims, store RoomGetter, action string, room string, options RoomAuthorizationOptions) bool {
	if claims == nil {
		return false
	}

	separator := options.TenantSeparator
	if separator == "" {
		separator = ":"
	}
	if claims.Allows(action, room, options.EnforceTenantPrefix, separator) {
		return true
	}
	if options.EnforceTenantPrefix {
		if claims.Tenant == "" || !strings.HasPrefix(room, claims.Tenant+separator) {
			return false
		}
	}
	if store == nil {
		return false
	}
	record, err := store.GetRoom(ctx, room)
	if err != nil {
		return false
	}
	return record.Allows(claims.Subject, claims.RoomGroupIDs(), action)
}
