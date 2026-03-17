package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/db"
)

// CreateEnrollmentToken implements StrictServerInterface.
func (s *Server) CreateEnrollmentToken(ctx context.Context, request CreateEnrollmentTokenRequestObject) (CreateEnrollmentTokenResponseObject, error) {
	if !isAdmin(ctx) {
		return CreateEnrollmentToken403JSONResponse{Error: "admin access required"}, nil
	}

	// Generate crypto-random token (32 bytes = 64 hex chars).
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}
	tokenStr := hex.EncodeToString(tokenBytes)

	label := ""
	if request.Body.Label != nil {
		label = *request.Body.Label
	}
	maxUses := 0
	if request.Body.MaxUses != nil {
		maxUses = *request.Body.MaxUses
	}
	expiresInHours := 24
	if request.Body.ExpiresInHours != nil {
		expiresInHours = *request.Body.ExpiresInHours
	}

	et := &db.EnrollmentToken{
		ID:        uuid.New(),
		Token:     tokenStr,
		Label:     label,
		CreatedBy: ContextUserID(ctx),
		MaxUses:   maxUses,
		UseCount:  0,
		ExpiresAt: time.Now().UTC().Add(time.Duration(expiresInHours) * time.Hour),
	}

	if err := s.store.CreateEnrollmentToken(ctx, et); err != nil {
		return nil, fmt.Errorf("create enrollment token: %w", err)
	}

	s.auditLog(ContextUserID(ctx), "enrollment.create", et.ID.String(),
		fmt.Sprintf("label=%s max_uses=%d expires_in=%dh", label, maxUses, expiresInHours))

	return CreateEnrollmentToken201JSONResponse(enrollmentTokenToAPI(et)), nil
}

// ListEnrollmentTokens implements StrictServerInterface.
func (s *Server) ListEnrollmentTokens(ctx context.Context, _ ListEnrollmentTokensRequestObject) (ListEnrollmentTokensResponseObject, error) {
	if !isAdmin(ctx) {
		return ListEnrollmentTokens403JSONResponse{Error: "admin access required"}, nil
	}

	tokens, err := s.store.ListEnrollmentTokens(ctx, ContextUserID(ctx))
	if err != nil {
		return nil, fmt.Errorf("list enrollment tokens: %w", err)
	}

	result := make([]EnrollmentToken, 0, len(tokens))
	for _, t := range tokens {
		result = append(result, enrollmentTokenToAPI(t))
	}
	return ListEnrollmentTokens200JSONResponse(result), nil
}

// DeleteEnrollmentToken implements StrictServerInterface.
func (s *Server) DeleteEnrollmentToken(ctx context.Context, request DeleteEnrollmentTokenRequestObject) (DeleteEnrollmentTokenResponseObject, error) {
	if !isAdmin(ctx) {
		return DeleteEnrollmentToken403JSONResponse{Error: "admin access required"}, nil
	}

	if err := s.store.DeleteEnrollmentToken(ctx, request.Id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return DeleteEnrollmentToken404JSONResponse{Error: "token not found"}, nil
		}
		return nil, fmt.Errorf("delete enrollment token: %w", err)
	}

	s.auditLog(ContextUserID(ctx), "enrollment.delete", request.Id.String(), "")
	return DeleteEnrollmentToken204Response{}, nil
}

// Enroll implements StrictServerInterface.
// This is a public endpoint — no auth required.
func (s *Server) Enroll(ctx context.Context, request EnrollRequestObject) (EnrollResponseObject, error) {
	if s.cert == nil {
		return nil, fmt.Errorf("cert provider not configured")
	}

	et, err := s.store.GetEnrollmentTokenByToken(ctx, request.Token)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return Enroll404JSONResponse{Error: "invalid enrollment token"}, nil
		}
		return nil, fmt.Errorf("get enrollment token: %w", err)
	}

	// Check expiry.
	if time.Now().UTC().After(et.ExpiresAt) {
		return Enroll410JSONResponse{Error: "enrollment token expired"}, nil
	}

	// Check usage limit (0 = unlimited).
	if et.MaxUses > 0 && et.UseCount >= et.MaxUses {
		return Enroll410JSONResponse{Error: "enrollment token exhausted"}, nil
	}

	// Increment use count.
	if err := s.store.IncrementEnrollmentTokenUseCount(ctx, et.ID); err != nil {
		return nil, fmt.Errorf("increment token use count: %w", err)
	}

	// Derive server address from the HTTP request host.
	httpReq := httpRequestFromContext(ctx)
	host := httpReq.Host
	// Strip port if present to get just the hostname.
	if idx := len(host) - 1; idx >= 0 {
		for i := idx; i >= 0; i-- {
			if host[i] == ':' {
				host = host[:i]
				break
			}
			if host[i] == ']' {
				break // IPv6 bracket, no port
			}
		}
	}

	quicHost := host
	if s.quicHost != "" {
		quicHost = s.quicHost
	}

	return Enroll200JSONResponse{
		CaPem:        string(s.cert.CACertPEM()),
		ServerAddr:   quicHost + ":9090",
		ServerDomain: host,
	}, nil
}

// GetServerCA implements StrictServerInterface.
func (s *Server) GetServerCA(ctx context.Context, _ GetServerCARequestObject) (GetServerCAResponseObject, error) {
	if s.cert == nil {
		return nil, fmt.Errorf("cert provider not configured")
	}
	return GetServerCA200JSONResponse{Pem: string(s.cert.CACertPEM())}, nil
}

func enrollmentTokenToAPI(t *db.EnrollmentToken) EnrollmentToken {
	return EnrollmentToken{
		Id:        t.ID,
		Token:     t.Token,
		Label:     t.Label,
		CreatedBy: t.CreatedBy,
		MaxUses:   t.MaxUses,
		UseCount:  t.UseCount,
		ExpiresAt: t.ExpiresAt,
		CreatedAt: t.CreatedAt,
	}
}
