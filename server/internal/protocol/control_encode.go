package protocol

import "github.com/vmihailenco/msgpack/v5"

// controlFieldCount is the number of msgpack-encoded fields ControlMessage
// declares. controlFieldPresence and encodeControlField below are indexed
// positionally, so both must track the struct;
// TestEncodeControlMatchesReflectionPerField walks the struct by reflection and
// fails if any of the three ever drift.
const controlFieldCount = 83

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
	present[7] = m.OrgID != ""
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
	present[20] = m.PendingSamples != 0
	present[21] = m.OldestTS != 0
	present[22] = m.Rate != 0
	present[23] = m.Deadline != 0
	present[24] = m.RetryAfter != 0
	present[25] = m.Tier != ""
	present[26] = len(m.BackfillSamples) != 0
	present[27] = m.Cursor != 0
	present[28] = m.Dim != ""
	present[29] = m.FromTS != 0
	present[30] = m.ToTS != 0
	present[31] = m.MaxPoints != 0
	present[32] = len(m.HistoryPoints) != 0
	present[33] = m.Truncated != nil
	present[34] = m.Token != ""
	present[35] = m.RelayURL != ""
	present[36] = m.Reason != ""
	present[37] = m.Permissions != nil
	present[38] = m.Version != ""
	present[39] = m.URL != ""
	present[40] = m.SHA256 != ""
	present[41] = m.Signature != ""
	present[42] = m.Success != nil
	present[43] = m.AckError != ""
	present[44] = m.SDPOffer != ""
	present[45] = m.Candidate != ""
	present[46] = m.Mid != ""
	present[47] = m.X != 0
	present[48] = m.Y != 0
	present[49] = m.Button != ""
	present[50] = m.Pressed != nil
	present[51] = m.Key != ""
	present[52] = m.Cols != 0
	present[53] = m.Rows != 0
	present[54] = m.Path != ""
	present[55] = len(m.Entries) != 0
	present[56] = m.TotalSize != 0
	present[57] = m.Text != ""
	present[58] = m.Sender != ""
	present[59] = m.CPUModel != ""
	present[60] = m.CPUCores != 0
	present[61] = m.RAMTotalMB != 0
	present[62] = m.DiskTotalMB != 0
	present[63] = m.DiskFreeMB != 0
	present[64] = len(m.NetworkInterfaces) != 0
	present[65] = m.LogLevel != ""
	present[66] = m.TimeFrom != ""
	present[67] = m.TimeTo != ""
	present[68] = m.Search != ""
	present[69] = m.LogOffset != 0
	present[70] = m.LogLimit != 0
	present[71] = m.Source != ""
	present[72] = m.Unit != ""
	present[73] = len(m.LogEntries) != 0
	present[74] = m.TotalCount != 0
	present[75] = m.HasMore != nil
	present[76] = len(m.AvailableUnits) != 0
	present[77] = len(m.Ports) != 0
	present[78] = len(m.Services) != 0
	present[79] = len(m.DBEngines) != 0
	present[80] = len(m.Containers) != 0
	present[81] = len(m.Packages) != 0
	present[82] = m.Enabled != nil
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
		return putString(enc, "org_id", string(m.OrgID))
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
		return putUint64(enc, "pending_samples", m.PendingSamples)
	case 21:
		return putInt64(enc, "oldest_ts", m.OldestTS)
	case 22:
		return putUint32(enc, "rate", m.Rate)
	case 23:
		return putInt64(enc, "deadline", m.Deadline)
	case 24:
		return putUint32(enc, "retry_after", m.RetryAfter)
	case 25:
		return putString(enc, "tier", string(m.Tier))
	case 26:
		return putValue(enc, "samples", m.BackfillSamples)
	case 27:
		return putInt64(enc, "cursor", m.Cursor)
	case 28:
		return putString(enc, "dim", string(m.Dim))
	case 29:
		return putInt64(enc, "from_ts", m.FromTS)
	case 30:
		return putInt64(enc, "to_ts", m.ToTS)
	case 31:
		return putUint32(enc, "max_points", m.MaxPoints)
	case 32:
		return putValue(enc, "points", m.HistoryPoints)
	case 33:
		return putBool(enc, "truncated", *m.Truncated)
	case 34:
		return putString(enc, "token", string(m.Token))
	case 35:
		return putString(enc, "relay_url", string(m.RelayURL))
	case 36:
		return putString(enc, "reason", string(m.Reason))
	case 37:
		return putValue(enc, "permissions", m.Permissions)
	case 38:
		return putString(enc, "version", string(m.Version))
	case 39:
		return putString(enc, "url", string(m.URL))
	case 40:
		return putString(enc, "sha256", string(m.SHA256))
	case 41:
		return putString(enc, "signature", string(m.Signature))
	case 42:
		return putBool(enc, "success", *m.Success)
	case 43:
		return putString(enc, "error", string(m.AckError))
	case 44:
		return putString(enc, "sdp_offer", string(m.SDPOffer))
	case 45:
		return putString(enc, "candidate", string(m.Candidate))
	case 46:
		return putString(enc, "mid", string(m.Mid))
	case 47:
		return putUint16(enc, "x", m.X)
	case 48:
		return putUint16(enc, "y", m.Y)
	case 49:
		return putString(enc, "button", string(m.Button))
	case 50:
		return putBool(enc, "pressed", *m.Pressed)
	case 51:
		return putString(enc, "key", string(m.Key))
	case 52:
		return putUint16(enc, "cols", m.Cols)
	case 53:
		return putUint16(enc, "rows", m.Rows)
	case 54:
		return putString(enc, "path", string(m.Path))
	case 55:
		return putValue(enc, "entries", m.Entries)
	case 56:
		return putUint64(enc, "total_size", m.TotalSize)
	case 57:
		return putString(enc, "text", string(m.Text))
	case 58:
		return putString(enc, "sender", string(m.Sender))
	case 59:
		return putString(enc, "cpu_model", string(m.CPUModel))
	case 60:
		return putUint32(enc, "cpu_cores", m.CPUCores)
	case 61:
		return putUint64(enc, "ram_total_mb", m.RAMTotalMB)
	case 62:
		return putUint64(enc, "disk_total_mb", m.DiskTotalMB)
	case 63:
		return putUint64(enc, "disk_free_mb", m.DiskFreeMB)
	case 64:
		return putValue(enc, "network_interfaces", m.NetworkInterfaces)
	case 65:
		return putString(enc, "log_level", string(m.LogLevel))
	case 66:
		return putString(enc, "time_from", string(m.TimeFrom))
	case 67:
		return putString(enc, "time_to", string(m.TimeTo))
	case 68:
		return putString(enc, "search", string(m.Search))
	case 69:
		return putUint32(enc, "log_offset", m.LogOffset)
	case 70:
		return putUint32(enc, "log_limit", m.LogLimit)
	case 71:
		return putString(enc, "source", string(m.Source))
	case 72:
		return putString(enc, "unit", string(m.Unit))
	case 73:
		return putValue(enc, "log_entries", m.LogEntries)
	case 74:
		return putUint32(enc, "total_count", m.TotalCount)
	case 75:
		return putBool(enc, "has_more", *m.HasMore)
	case 76:
		return putValue(enc, "available_units", m.AvailableUnits)
	case 77:
		return putValue(enc, "ports", m.Ports)
	case 78:
		return putValue(enc, "services", m.Services)
	case 79:
		return putValue(enc, "db_engines", m.DBEngines)
	case 80:
		return putValue(enc, "containers", m.Containers)
	case 81:
		return putValue(enc, "packages", m.Packages)
	case 82:
		return putBool(enc, "enabled", *m.Enabled)
	}
	return nil
}
