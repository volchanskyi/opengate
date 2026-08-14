package protocol

import "github.com/vmihailenco/msgpack/v5"

// controlFieldCount is the number of msgpack-encoded fields ControlMessage
// declares. controlFieldPresence and encodeControlField below are indexed
// positionally, so both must track the struct;
// TestEncodeControlMatchesReflectionPerField walks the struct by reflection and
// fails if any of the three ever drift.
const controlFieldCount = 99

// Each put* helper writes one map entry: the key, then the value through the
// same encoder method the reflection encoder picks for that Go type. The integer
// helpers are the width-preserving ones — compact ints are off by default — and
// that is what keeps the emitted bytes byte-identical to the reflection path.

func putString(enc *msgpack.Encoder, key, val string) error {
	if err := enc.EncodeString(key); err != nil {
		return err
	}
	return enc.EncodeString(val)
}

func putInt64(enc *msgpack.Encoder, key string, val int64) error {
	if err := enc.EncodeString(key); err != nil {
		return err
	}
	return enc.EncodeInt64(val)
}

func putUint16(enc *msgpack.Encoder, key string, val uint16) error {
	if err := enc.EncodeString(key); err != nil {
		return err
	}
	return enc.EncodeUint16(val)
}

func putUint32(enc *msgpack.Encoder, key string, val uint32) error {
	if err := enc.EncodeString(key); err != nil {
		return err
	}
	return enc.EncodeUint32(val)
}

func putUint64(enc *msgpack.Encoder, key string, val uint64) error {
	if err := enc.EncodeString(key); err != nil {
		return err
	}
	return enc.EncodeUint64(val)
}

func putFloat64(enc *msgpack.Encoder, key string, val float64) error {
	if err := enc.EncodeString(key); err != nil {
		return err
	}
	return enc.EncodeFloat64(val)
}

func putBool(enc *msgpack.Encoder, key string, val bool) error {
	if err := enc.EncodeString(key); err != nil {
		return err
	}
	return enc.EncodeBool(val)
}

func putBytes(enc *msgpack.Encoder, key string, val []byte) error {
	if err := enc.EncodeString(key); err != nil {
		return err
	}
	return enc.EncodeBytes(val)
}

// putValue covers the composite fields — slices of structs and struct pointers —
// whose payload the reflection encoder still handles. Those are fields a message
// actually carries, so that cost tracks real content rather than the union's
// declared width.
func putValue(enc *msgpack.Encoder, key string, val any) error {
	if err := enc.EncodeString(key); err != nil {
		return err
	}
	return enc.Encode(val)
}

// EncodeMsgpack writes the message as a msgpack map holding only the fields it
// actually carries.
//
// ControlMessage is a union: one flat struct covering every message type, so
// every field but Type is omitempty and all but a handful are zero on any given
// message. The reflection-based struct encoder decides emptiness by calling
// reflect.Value.Interface() on each omitempty field, which heap-boxes it — so
// encoding any message allocated once per declared field, and every field added
// to the union made every message on the wire more expensive. Testing emptiness
// with direct typed comparisons keeps the cost proportional to the fields a
// message actually populates.
//
// The emitted bytes match the reflection encoder's exactly: same field order
// (declaration order), same keys, same omitempty semantics, same integer widths.
// codec_wire_equivalence_test.go diffs the two encoders field by field.
func (m *ControlMessage) EncodeMsgpack(enc *msgpack.Encoder) error {
	present := m.controlFieldPresence()

	n := 0
	for _, ok := range present {
		if ok {
			n++
		}
	}
	if err := enc.EncodeMapLen(n); err != nil {
		return err
	}

	// Ascending index is declaration order, which is the order the reflection
	// encoder emits and therefore the order the bytes must keep.
	for i, ok := range present {
		if !ok {
			continue
		}
		if err := m.encodeControlField(enc, i); err != nil {
			return err
		}
	}
	return nil
}

// controlFieldPresence reports, per field position, whether the field carries a
// value msgpack would emit under omitempty. The result is a value array, so it
// stays on the stack and this pass allocates nothing.
func (m *ControlMessage) controlFieldPresence() [controlFieldCount]bool {
	var present [controlFieldCount]bool
	present[0] = true // type: no omitempty, always emitted
	present[1] = len(m.Capabilities) != 0
	present[2] = m.Hostname != ""
	present[3] = m.OS != ""
	present[4] = m.Arch != ""
	present[5] = m.Timestamp != 0
	present[6] = m.TS != 0
	present[7] = m.TenantID != ""
	present[8] = m.NodeAnomalyRate != 0
	present[9] = len(m.PerFamilyRates) != 0
	present[10] = len(m.RecentBitmask) != 0
	present[11] = m.SamplerVersion != ""
	present[12] = m.ModelVersion != ""
	present[13] = len(m.Dims) != 0
	present[14] = len(m.TopN) != 0
	present[15] = m.SinceTS != 0
	present[16] = m.Limit != 0
	present[17] = len(m.Summaries) != 0
	present[18] = len(m.Breaches) != 0
	present[19] = len(m.AlertRules) != 0
	present[20] = len(m.RuleCoverage) != 0
	present[21] = m.PendingSamples != 0
	present[22] = m.OldestTS != 0
	present[23] = m.Rate != 0
	present[24] = m.Deadline != 0
	present[25] = m.RetryAfter != 0
	present[26] = m.Tier != ""
	present[27] = len(m.BackfillSamples) != 0
	present[28] = m.Cursor != 0
	present[29] = m.Dim != ""
	present[30] = m.FromTS != 0
	present[31] = m.ToTS != 0
	present[32] = m.MaxPoints != 0
	present[33] = len(m.HistoryPoints) != 0
	present[34] = m.Truncated != nil
	present[35] = m.Token != ""
	present[36] = m.RelayURL != ""
	present[37] = m.Reason != ""
	present[38] = m.Permissions != nil
	present[39] = m.Version != ""
	present[40] = m.URL != ""
	present[41] = m.SHA256 != ""
	present[42] = m.Signature != ""
	present[43] = m.Success != nil
	present[44] = m.AckError != ""
	present[45] = m.SDPOffer != ""
	present[46] = m.Candidate != ""
	present[47] = m.Mid != ""
	present[48] = m.X != 0
	present[49] = m.Y != 0
	present[50] = m.Button != ""
	present[51] = m.Pressed != nil
	present[52] = m.Key != ""
	present[53] = m.Cols != 0
	present[54] = m.Rows != 0
	present[55] = m.Path != ""
	present[56] = len(m.Entries) != 0
	present[57] = m.TotalSize != 0
	present[58] = m.Text != ""
	present[59] = m.Sender != ""
	present[60] = m.CPUModel != ""
	present[61] = m.CPUCores != 0
	present[62] = m.RAMTotalMB != 0
	present[63] = m.DiskTotalMB != 0
	present[64] = m.DiskFreeMB != 0
	present[65] = len(m.NetworkInterfaces) != 0
	present[66] = m.SystemUUID != ""
	present[67] = m.AMTAvailable != nil
	present[68] = m.AMTVersion != ""
	present[69] = m.LogLevel != ""
	present[70] = m.TimeFrom != ""
	present[71] = m.TimeTo != ""
	present[72] = m.Search != ""
	present[73] = m.LogOffset != 0
	present[74] = m.LogLimit != 0
	present[75] = m.Source != ""
	present[76] = m.Unit != ""
	present[77] = len(m.LogEntries) != 0
	present[78] = m.TotalCount != 0
	present[79] = m.HasMore != nil
	present[80] = len(m.AvailableUnits) != 0
	present[81] = len(m.Ports) != 0
	present[82] = len(m.Services) != 0
	present[83] = len(m.DBEngines) != 0
	present[84] = len(m.Containers) != 0
	present[85] = len(m.Packages) != 0
	present[86] = m.Enabled != nil
	present[87] = m.AlertID != ""
	present[88] = m.RuleID != ""
	present[89] = m.RuleVersion != 0
	present[90] = m.Severity != nil
	present[91] = m.Metric != ""
	present[92] = m.Value != nil
	present[93] = m.WindowStartTS != 0
	present[94] = m.WindowEndTS != 0
	present[95] = m.ObservedTS != 0
	present[96] = m.Backfilled != nil
	present[97] = m.EvidenceCodec != ""
	present[98] = len(m.Evidence) != 0
	return present
}

// encodeControlField writes the field at position i. One case per struct field,
// in declaration order.
func (m *ControlMessage) encodeControlField(enc *msgpack.Encoder, i int) error {
	switch i {
	case 0:
		return putString(enc, "type", string(m.Type))
	case 1:
		return putValue(enc, "capabilities", m.Capabilities)
	case 2:
		return putString(enc, "hostname", string(m.Hostname))
	case 3:
		return putString(enc, "os", string(m.OS))
	case 4:
		return putString(enc, "arch", string(m.Arch))
	case 5:
		return putInt64(enc, "timestamp", m.Timestamp)
	case 6:
		return putInt64(enc, "ts", m.TS)
	case 7:
		return putString(enc, "tenant_id", string(m.TenantID))
	case 8:
		return putFloat64(enc, "node_anomaly_rate", m.NodeAnomalyRate)
	case 9:
		return putValue(enc, "per_family_rates", m.PerFamilyRates)
	case 10:
		return putBytes(enc, "recent_bitmask", m.RecentBitmask)
	case 11:
		return putString(enc, "sampler_ver", string(m.SamplerVersion))
	case 12:
		return putString(enc, "model_ver", string(m.ModelVersion))
	case 13:
		return putValue(enc, "dims", m.Dims)
	case 14:
		return putValue(enc, "top_n", m.TopN)
	case 15:
		return putInt64(enc, "since_ts", m.SinceTS)
	case 16:
		return putUint32(enc, "limit", m.Limit)
	case 17:
		return putValue(enc, "summaries", m.Summaries)
	case 18:
		return putValue(enc, "breaches", m.Breaches)
	case 19:
		return putValue(enc, "rules", m.AlertRules)
	case 20:
		return putValue(enc, "rule_coverage", m.RuleCoverage)
	case 21:
		return putUint64(enc, "pending_samples", m.PendingSamples)
	case 22:
		return putInt64(enc, "oldest_ts", m.OldestTS)
	case 23:
		return putUint32(enc, "rate", m.Rate)
	case 24:
		return putInt64(enc, "deadline", m.Deadline)
	case 25:
		return putUint32(enc, "retry_after", m.RetryAfter)
	case 26:
		return putString(enc, "tier", string(m.Tier))
	case 27:
		return putValue(enc, "samples", m.BackfillSamples)
	case 28:
		return putInt64(enc, "cursor", m.Cursor)
	case 29:
		return putString(enc, "dim", string(m.Dim))
	case 30:
		return putInt64(enc, "from_ts", m.FromTS)
	case 31:
		return putInt64(enc, "to_ts", m.ToTS)
	case 32:
		return putUint32(enc, "max_points", m.MaxPoints)
	case 33:
		return putValue(enc, "points", m.HistoryPoints)
	case 34:
		return putBool(enc, "truncated", *m.Truncated)
	case 35:
		return putString(enc, "token", string(m.Token))
	case 36:
		return putString(enc, "relay_url", string(m.RelayURL))
	case 37:
		return putString(enc, "reason", string(m.Reason))
	case 38:
		return putValue(enc, "permissions", m.Permissions)
	case 39:
		return putString(enc, "version", string(m.Version))
	case 40:
		return putString(enc, "url", string(m.URL))
	case 41:
		return putString(enc, "sha256", string(m.SHA256))
	case 42:
		return putString(enc, "signature", string(m.Signature))
	case 43:
		return putBool(enc, "success", *m.Success)
	case 44:
		return putString(enc, "error", string(m.AckError))
	case 45:
		return putString(enc, "sdp_offer", string(m.SDPOffer))
	case 46:
		return putString(enc, "candidate", string(m.Candidate))
	case 47:
		return putString(enc, "mid", string(m.Mid))
	case 48:
		return putUint16(enc, "x", m.X)
	case 49:
		return putUint16(enc, "y", m.Y)
	case 50:
		return putString(enc, "button", string(m.Button))
	case 51:
		return putBool(enc, "pressed", *m.Pressed)
	case 52:
		return putString(enc, "key", string(m.Key))
	case 53:
		return putUint16(enc, "cols", m.Cols)
	case 54:
		return putUint16(enc, "rows", m.Rows)
	case 55:
		return putString(enc, "path", string(m.Path))
	case 56:
		return putValue(enc, "entries", m.Entries)
	case 57:
		return putUint64(enc, "total_size", m.TotalSize)
	case 58:
		return putString(enc, "text", string(m.Text))
	case 59:
		return putString(enc, "sender", string(m.Sender))
	case 60:
		return putString(enc, "cpu_model", string(m.CPUModel))
	case 61:
		return putUint32(enc, "cpu_cores", m.CPUCores)
	case 62:
		return putUint64(enc, "ram_total_mb", m.RAMTotalMB)
	case 63:
		return putUint64(enc, "disk_total_mb", m.DiskTotalMB)
	case 64:
		return putUint64(enc, "disk_free_mb", m.DiskFreeMB)
	case 65:
		return putValue(enc, "network_interfaces", m.NetworkInterfaces)
	case 66:
		return putString(enc, "system_uuid", string(m.SystemUUID))
	case 67:
		return putBool(enc, "amt_available", *m.AMTAvailable)
	case 68:
		return putString(enc, "amt_version", string(m.AMTVersion))
	case 69:
		return putString(enc, "log_level", string(m.LogLevel))
	case 70:
		return putString(enc, "time_from", string(m.TimeFrom))
	case 71:
		return putString(enc, "time_to", string(m.TimeTo))
	case 72:
		return putString(enc, "search", string(m.Search))
	case 73:
		return putUint32(enc, "log_offset", m.LogOffset)
	case 74:
		return putUint32(enc, "log_limit", m.LogLimit)
	case 75:
		return putString(enc, "source", string(m.Source))
	case 76:
		return putString(enc, "unit", string(m.Unit))
	case 77:
		return putValue(enc, "log_entries", m.LogEntries)
	case 78:
		return putUint32(enc, "total_count", m.TotalCount)
	case 79:
		return putBool(enc, "has_more", *m.HasMore)
	case 80:
		return putValue(enc, "available_units", m.AvailableUnits)
	case 81:
		return putValue(enc, "ports", m.Ports)
	case 82:
		return putValue(enc, "services", m.Services)
	case 83:
		return putValue(enc, "db_engines", m.DBEngines)
	case 84:
		return putValue(enc, "containers", m.Containers)
	case 85:
		return putValue(enc, "packages", m.Packages)
	case 86:
		return putBool(enc, "enabled", *m.Enabled)
	case 87:
		return putString(enc, "alert_id", string(m.AlertID))
	case 88:
		return putString(enc, "rule_id", string(m.RuleID))
	case 89:
		return putUint32(enc, "rule_version", m.RuleVersion)
	case 90:
		return putString(enc, "severity", string(*m.Severity))
	case 91:
		return putString(enc, "metric", string(m.Metric))
	case 92:
		return putFloat64(enc, "value", *m.Value)
	case 93:
		return putInt64(enc, "window_start_ts", m.WindowStartTS)
	case 94:
		return putInt64(enc, "window_end_ts", m.WindowEndTS)
	case 95:
		return putInt64(enc, "observed_ts", m.ObservedTS)
	case 96:
		return putBool(enc, "backfilled", *m.Backfilled)
	case 97:
		return putString(enc, "evidence_codec", string(m.EvidenceCodec))
	case 98:
		return putBytes(enc, "evidence", m.Evidence)
	}
	return nil
}
