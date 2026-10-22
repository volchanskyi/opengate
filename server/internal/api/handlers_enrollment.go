package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/devicecert"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

// CreateEnrollmentToken implements StrictServerInterface.
func (s *Server) CreateEnrollmentToken(ctx context.Context, request CreateEnrollmentTokenRequestObject) (CreateEnrollmentTokenResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, CreateEnrollmentToken403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}
	tokenStr := hex.EncodeToString(tokenBytes)

	label := ""
	if request.Body.Label != nil {
		label = sanitizeText(*request.Body.Label, maxLabelLen)
	}
	maxUses := 0
	if request.Body.MaxUses != nil {
		maxUses = *request.Body.MaxUses
	}
	expiresInHours := 24
	if request.Body.ExpiresInHours != nil {
		expiresInHours = *request.Body.ExpiresInHours
	}

	et := &updater.EnrollmentToken{
		ID:        uuid.New(),
		Token:     tokenStr,
		Label:     label,
		CreatedBy: ContextUserID(ctx),
		MaxUses:   maxUses,
		UseCount:  0,
		ExpiresAt: time.Now().UTC().Add(time.Duration(expiresInHours) * time.Hour),
	}

	if err := s.enrollment.Create(ctx, et); err != nil {
		return nil, fmt.Errorf("create enrollment token: %w", err)
	}

	s.auditLog(ctx, ContextUserID(ctx), "enrollment.create", et.ID.String(),
		fmt.Sprintf("label=%s max_uses=%d expires_in=%dh", label, maxUses, expiresInHours))

	return CreateEnrollmentToken201JSONResponse(enrollmentTokenToAPI(et)), nil
}

// ListEnrollmentTokens implements StrictServerInterface.
func (s *Server) ListEnrollmentTokens(ctx context.Context, _ ListEnrollmentTokensRequestObject) (ListEnrollmentTokensResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, ListEnrollmentTokens403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}

	tokens, err := s.enrollment.List(ctx, ContextUserID(ctx))
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
	if resp, denied := denyIfNotAdmin(ctx, DeleteEnrollmentToken403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}

	if err := s.enrollment.Delete(ctx, request.Id); err != nil {
		if errors.Is(err, updater.ErrNotFound) {
			return DeleteEnrollmentToken404JSONResponse{Error: "token not found"}, nil
		}
		return nil, fmt.Errorf("delete enrollment token: %w", err)
	}

	s.auditLog(ctx, ContextUserID(ctx), "enrollment.delete", request.Id.String(), "")
	return DeleteEnrollmentToken204Response{}, nil
}

// Enroll implements StrictServerInterface on a public endpoint that needs no authentication.
func (s *Server) Enroll(ctx context.Context, request EnrollRequestObject) (EnrollResponseObject, error) {
	if s.cert == nil {
		return nil, fmt.Errorf("cert provider not configured")
	}

	et, resp, err := s.validateEnrollmentToken(ctx, request.Token)
	if err != nil {
		return nil, err
	}
	if resp != nil {
		return resp, nil
	}

	host, quicHost := s.deriveEnrollHosts(ctx)

	result := Enroll200JSONResponse{
		CaPem:        string(s.cert.CACertPEM()),
		ServerAddr:   quicHost + ":9090",
		ServerDomain: host,
	}

	if s.signing != nil {
		key := s.signing.PublicKeyHex()
		result.UpdateSigningKey = &key
	}

	// An empty csr_pem is the install script's probe: it validates the token without a use.
	if request.Body != nil && request.Body.CsrPem != "" {
		deviceID, certPEM, err := s.issueEnrolment(ctx, request.Body.CsrPem)
		if errors.Is(err, devicecert.ErrInvalidRequest) {
			s.logger.Warn("enrollment request refused", "error", err)
			return Enroll400JSONResponse{Error: "invalid enrollment request"}, nil
		}
		if err != nil {
			return nil, fmt.Errorf("issue enrollment certificate: %w", err)
		}
		result.CertPem = &certPEM
		result.DeviceId = &deviceID

		if err := s.enrollment.IncrementUseCount(ctx, et.ID); err != nil {
			return nil, fmt.Errorf("increment token use count: %w", err)
		}
	}

	return result, nil
}

// validateEnrollmentToken fetches the token and checks expiry/usage limits.
// Returns (token, nil, nil) on success, or (nil, response, nil) for client errors.
func (s *Server) validateEnrollmentToken(ctx context.Context, token string) (*updater.EnrollmentToken, EnrollResponseObject, error) {
	et, err := s.enrollment.GetByToken(ctx, token)
	if err != nil {
		if errors.Is(err, updater.ErrNotFound) {
			return nil, Enroll404JSONResponse{Error: "invalid enrollment token"}, nil
		}
		return nil, nil, fmt.Errorf("get enrollment token: %w", err)
	}

	if time.Now().UTC().After(et.ExpiresAt) {
		return nil, Enroll410JSONResponse{Error: "enrollment token expired"}, nil
	}

	if et.MaxUses > 0 && et.UseCount >= et.MaxUses {
		return nil, Enroll410JSONResponse{Error: "enrollment token exhausted"}, nil
	}

	return et, nil, nil
}

// deriveEnrollHosts extracts the hostname and QUIC address from the request context.
func (s *Server) deriveEnrollHosts(ctx context.Context) (host, quicHost string) {
	httpReq := httpRequestFromContext(ctx)
	host = httpReq.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	quicHost = host
	if s.quicHost != "" {
		quicHost = s.quicHost
	}
	return host, quicHost
}

// issueEnrolment decodes a PEM certificate request and has it signed under a device number the
// server chooses, returning that number and the certificate as PEM.
func (s *Server) issueEnrolment(ctx context.Context, csrPEM string) (uuid.UUID, string, error) {
	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return uuid.Nil, "", fmt.Errorf("%w: not a PEM certificate request", devicecert.ErrInvalidRequest)
	}
	deviceID, certDER, err := s.cert.Enroll(ctx, block.Bytes)
	if err != nil {
		return uuid.Nil, "", err
	}
	return deviceID, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})), nil
}

// GetServerCA implements StrictServerInterface.
func (s *Server) GetServerCA(ctx context.Context, _ GetServerCARequestObject) (GetServerCAResponseObject, error) {
	if s.cert == nil {
		return nil, fmt.Errorf("cert provider not configured")
	}
	return GetServerCA200JSONResponse{Pem: string(s.cert.CACertPEM())}, nil
}

func enrollmentTokenToAPI(t *updater.EnrollmentToken) EnrollmentToken {
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
