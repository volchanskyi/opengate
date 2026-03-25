package api

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/db"
)

// CreateEnrollmentToken implements StrictServerInterface.
func (s *Server) CreateEnrollmentToken(ctx context.Context, request CreateEnrollmentTokenRequestObject) (CreateEnrollmentTokenResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, CreateEnrollmentToken403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
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
	if resp, denied := denyIfNotAdmin(ctx, ListEnrollmentTokens403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
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
	if resp, denied := denyIfNotAdmin(ctx, DeleteEnrollmentToken403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
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

	// Include the Ed25519 update signing key so agents can verify updates
	// without needing the --update-public-key CLI flag.
	if s.signing != nil {
		key := s.signing.PublicKeyHex()
		result.UpdateSigningKey = &key
	}

	// Sign agent CSR if provided; only count as a real enrollment when a
	// certificate is actually issued (the install script probes with an
	// empty csr_pem to validate the token without consuming a use).
	if request.Body != nil && request.Body.CsrPem != "" {
		certPEM, err := s.signCSR(request.Body.CsrPem)
		if err != nil {
			s.logger.Warn("CSR signing failed", "error", err)
			return Enroll400JSONResponse{Error: "invalid enrollment request"}, nil
		}
		result.CertPem = &certPEM

		if err := s.store.IncrementEnrollmentTokenUseCount(ctx, et.ID); err != nil {
			return nil, fmt.Errorf("increment token use count: %w", err)
		}
	}

	return result, nil
}

// validateEnrollmentToken fetches the token and checks expiry/usage limits.
// Returns (token, nil, nil) on success, or (nil, response, nil) for client errors.
func (s *Server) validateEnrollmentToken(ctx context.Context, token string) (*db.EnrollmentToken, EnrollResponseObject, error) {
	et, err := s.store.GetEnrollmentTokenByToken(ctx, token)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
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

// signCSR decodes a PEM-encoded CSR, signs it with the server CA,
// and returns the signed certificate as PEM.
func (s *Server) signCSR(csrPEM string) (string, error) {
	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return "", fmt.Errorf("invalid CSR PEM")
	}

	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse CSR: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return "", fmt.Errorf("CSR signature invalid: %w", err)
	}

	certDER, err := s.cert.SignAgentCSR(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("sign agent CSR: %w", err)
	}

	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})), nil
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
