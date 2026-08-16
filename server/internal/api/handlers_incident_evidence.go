package api

import (
	"context"
	"errors"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// Reading the frozen evidence behind one alert.
//
// It is a call of its own rather than part of the room, because evidence is tens
// of kilobytes per alert and a fleet event folds hundreds of them: a room that
// carried its evidence would move megabytes to render a page nobody has scrolled
// into yet.
//
// The decode happens here rather than in the browser for the same reason the
// codec is named on the row: the blob is DEFLATE around msgpack, a page has
// neither, and a reader that met a codec it did not know would have no way to
// say so. Three answers come back — the evidence, "there is none", and "this
// build cannot read what is stored" — and the third is what keeps a future codec
// an additive change rather than a page full of nonsense.
//
// Nothing is redacted on this path and nothing needs to be: log lines are
// redacted on the machine before they are sent, and this hands back the stored
// structure unchanged rather than re-deriving anything from elsewhere.

const (
	// msgAlertNotFound covers an alert that is not in the room it was asked for
	// through, one in another tenant, and one that carries no evidence. An alert
	// is only ever reachable through its room.
	msgAlertNotFound = "alert evidence not found"
	// msgEvidenceUnreadable is evidence this build cannot decode — a codec it
	// does not know, or bytes that do not read back as evidence.
	msgEvidenceUnreadable = "evidence cannot be read by this build"
)

// GetAlertEvidence implements StrictServerInterface.
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
		// Stored and unreadable is a fact about this build, not a missing row,
		// and saying which is the whole reason the codec rides on the alert.
		s.logger.WarnContext(ctx, "stored alert evidence could not be decoded",
			"alert_id", request.AlertId, "codec", codec, "error", err)
		return GetAlertEvidence422JSONResponse{Error: msgEvidenceUnreadable}, nil
	}
	return GetAlertEvidence200JSONResponse(evidenceToAPI(evidence)), nil
}

// evidenceToAPI renders what the machine knew. Every list is present even when
// it is empty: a missing field reads as "not collected", which is not the same
// answer as "collected, and there was nothing".
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
			Cpu:      process.CPU,
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
