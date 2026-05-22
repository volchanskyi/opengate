package api

import (
	"context"

	"github.com/volchanskyi/opengate/server/internal/notifications"
)

// SubscribePush implements StrictServerInterface.
func (s *Server) SubscribePush(ctx context.Context, request SubscribePushRequestObject) (SubscribePushResponseObject, error) {
	userID := ContextUserID(ctx)

	sub := &notifications.WebPushSubscription{
		Endpoint: request.Body.Endpoint,
		UserID:   userID,
		P256dh:   request.Body.P256dh,
		Auth:     request.Body.Auth,
	}
	if err := s.webPush.Upsert(ctx, sub); err != nil {
		return nil, err
	}

	return SubscribePush204Response{}, nil
}

// UnsubscribePush implements StrictServerInterface.
func (s *Server) UnsubscribePush(ctx context.Context, request UnsubscribePushRequestObject) (UnsubscribePushResponseObject, error) {
	if err := s.webPush.Delete(ctx, request.Body.Endpoint); err != nil {
		return nil, err
	}

	return UnsubscribePush204Response{}, nil
}

// GetVapidKey implements StrictServerInterface.
func (s *Server) GetVapidKey(ctx context.Context, _ GetVapidKeyRequestObject) (GetVapidKeyResponseObject, error) {
	return GetVapidKey200JSONResponse{
		PublicKey: s.notifier.VAPIDPublicKey(),
	}, nil
}
