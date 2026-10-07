package api

import (
	"context"
	"errors"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

const (
	// msgAlertNotFound covers an alert absent from the room or without evidence.
	msgAlertNotFound = "alert evidence not found"
	// msgEvidenceUnreadable covers an unknown codec or bytes that do not decode as evidence.
	msgEvidenceUnreadable = "evidence cannot be read by this build"
)

// GetAlertEvidence implements StrictServerInterface and decodes the stored blob server-side.
func (s *Server) GetAlertEvidence(ctx context.Context, request GetAlertEvidenceRequestObject) (GetAlertEvidenceResponseObject, error) {
	if _, err := s.requireIncidentInScope(ctx, request.Id, deref(request.Params.OrganizationId)); err != nil {
		if errors.Is(err, alerts.ErrIncidentNotFound) {
			return GetAlertEvidence404JSONResponse{Error: msgIncidentNotFound}, nil
		}
		return nil, err
	}

	blob, codec, err := s.investigations.Evidence(ctx, request.Id, request.AlertId)
	switch {
	case err == nil:
	case errors.Is(err, alerts.ErrAlertNotFound), errors.Is(err, alerts.ErrNoEvidence):
		return GetAlertEvidence404JSONResponse{Error: msgAlertNotFound}, nil
	default:
		return nil, err
	}

	evidence, err := protocol.DecodeAlertEvidence(blob, codec)
	if err != nil {
		// Stored but undecodable evidence answers 422, distinct from a missing alert's 404.
		s.logger.WarnContext(ctx, "stored alert evidence could not be decoded",
			"alert_id", request.AlertId, "codec", codec, "error", err)
		return GetAlertEvidence422JSONResponse{Error: msgEvidenceUnreadable}, nil
	}
	return GetAlertEvidence200JSONResponse(evidenceToAPI(evidence)), nil
}

// evidenceToAPI renders the stored evidence with every list present, so an empty list reads
// as collected-and-empty.
func evidenceToAPI(evidence protocol.AlertEvidence) AlertEvidence {
	ranked := make([]EvidenceRankedDim, 0, len(evidence.Ranked))
	for _, dim := range evidence.Ranked {
		ranked = append(ranked, EvidenceRankedDim{Dim: dim.Dim, Score: dim.Score})
	}

	series := make([]EvidenceSeries, 0, len(evidence.Series))
	for _, reading := range evidence.Series {
		points := make([]EvidencePoint, 0, len(reading.Points))
		for _, point := range reading.Points {
			points = append(points, EvidencePoint{Ts: point.TS, Value: point.Value})
		}
		series = append(series, EvidenceSeries{Dim: reading.Dim, Points: points})
	}

	processes := make([]EvidenceProcess, 0, len(evidence.Processes))
	for _, process := range evidence.Processes {
		processes = append(processes, EvidenceProcess{
			Rank:     int(process.Rank),
			Basename: process.Basename,
			Pid:      int(process.PID),
			Cpu:      process.CPUShare,
			Mem:      process.Mem,
		})
	}

	samples := evidence.LogSamples
	if samples == nil {
		samples = []string{}
	}
	return AlertEvidence{
		Ranked:     ranked,
		Series:     series,
		Processes:  processes,
		LogSamples: samples,
		Truncated:  evidence.Truncated,
	}
}
