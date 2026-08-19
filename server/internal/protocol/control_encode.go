package protocol

import "github.com/vmihailenco/msgpack/v5"

// controlFieldCount is the number of msgpack-encoded fields ControlMessage
// declares. controlFieldPresence and encodeControlField below are indexed
// positionally, so both must track the struct;
// TestEncodeControlMatchesReflectionPerField walks the struct by reflection and
// fails if any of the three ever drift.
const controlFieldCount = 100

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
	present[21] = m.DeviceHourlyCeiling != 0
	present[22] = m.PendingSamples != 0
	present[23] = m.OldestTS != 0
	present[24] = m.Rate != 0
	present[25] = m.Deadline != 0
	present[26] = m.RetryAfter != 0
	present[27] = m.Tier != ""
	present[28] = len(m.BackfillSamples) != 0
	present[29] = m.Cursor != 0
	present[30] = m.Dim != ""
	present[31] = m.FromTS != 0
	present[32] = m.ToTS != 0
	present[33] = m.MaxPoints != 0
	present[34] = len(m.HistoryPoints) != 0
	present[35] = m.Truncated != nil
	present[36] = m.Token != ""
	present[37] = m.RelayURL != ""
	present[38] = m.Reason != ""
	present[39] = m.Permissions != nil
	present[40] = m.Version != ""
	present[41] = m.URL != ""
	present[42] = m.SHA256 != ""
	present[43] = m.Signature != ""
	present[44] = m.Success != nil
	present[45] = m.AckError != ""
	present[46] = m.SDPOffer != ""
	present[47] = m.Candidate != ""
	present[48] = m.Mid != ""
	present[49] = m.X != 0
	present[50] = m.Y != 0
	present[51] = m.Button != ""
	present[52] = m.Pressed != nil
	present[53] = m.Key != ""
	present[54] = m.Cols != 0
	present[55] = m.Rows != 0
	present[56] = m.Path != ""
	present[57] = len(m.Entries) != 0
	present[58] = m.TotalSize != 0
	present[59] = m.Text != ""
	present[60] = m.Sender != ""
	present[61] = m.CPUModel != ""
	present[62] = m.CPUCores != 0
	present[63] = m.RAMTotalMB != 0
	present[64] = m.DiskTotalMB != 0
	present[65] = m.DiskFreeMB != 0
	present[66] = len(m.NetworkInterfaces) != 0
	present[67] = m.SystemUUID != ""
	present[68] = m.AMTAvailable != nil
	present[69] = m.AMTVersion != ""
	present[70] = m.LogLevel != ""
	present[71] = m.TimeFrom != ""
	present[72] = m.TimeTo != ""
	present[73] = m.Search != ""
	present[74] = m.LogOffset != 0
	present[75] = m.LogLimit != 0
	present[76] = m.Source != ""
	present[77] = m.Unit != ""
	present[78] = len(m.LogEntries) != 0
	present[79] = m.TotalCount != 0
	present[80] = m.HasMore != nil
	present[81] = len(m.AvailableUnits) != 0
	present[82] = len(m.Ports) != 0
	present[83] = len(m.Services) != 0
	present[84] = len(m.DBEngines) != 0
	present[85] = len(m.Containers) != 0
	present[86] = len(m.Packages) != 0
	present[87] = m.Enabled != nil
	present[88] = m.AlertID != ""
	present[89] = m.RuleID != ""
	present[90] = m.RuleVersion != 0
	present[91] = m.Severity != nil
	present[92] = m.Metric != ""
	present[93] = m.Value != nil
	present[94] = m.WindowStartTS != 0
	present[95] = m.WindowEndTS != 0
	present[96] = m.ObservedTS != 0
	present[97] = m.Backfilled != nil
	present[98] = m.EvidenceCodec != ""
	present[99] = len(m.Evidence) != 0
	return present
}

// encodeControlField writes the field at position i, handing off to the group
// that owns it. The groups follow the struct's own declaration order, so a
// field's position on the wire is still the position it is declared at.
func (m *ControlMessage) encodeControlField(enc *msgpack.Encoder, i int) error {
	switch {
	case i <= 7:
		return m.encodeEnvelopeField(enc, i)
	case i <= 35:
		return m.encodeTelemetryField(enc, i)
	case i <= 45:
		return m.encodeSessionField(enc, i)
	case i <= 60:
		return m.encodeInteractionField(enc, i)
	case i <= 69:
		return m.encodeHardwareField(enc, i)
	case i <= 86:
		return m.encodeInventoryField(enc, i)
	case i <= 99:
		return m.encodeAlertField(enc, i)
	}
	return nil
}

// encodeEnvelopeField writes one of fields 0–7: who is speaking, when, and for which tenant.
func (m *ControlMessage) encodeEnvelopeField(enc *msgpack.Encoder, i int) error {
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
	}
	return nil
}

// encodeTelemetryField writes one of fields 8–35: vitals, summaries, breaches and the windows they cover.
func (m *ControlMessage) encodeTelemetryField(enc *msgpack.Encoder, i int) error {
	switch i {
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
		return putUint32(enc, "device_hourly_ceiling", m.DeviceHourlyCeiling)
	case 22:
		return putUint64(enc, "pending_samples", m.PendingSamples)
	case 23:
		return putInt64(enc, "oldest_ts", m.OldestTS)
	case 24:
		return putUint32(enc, "rate", m.Rate)
	case 25:
		return putInt64(enc, "deadline", m.Deadline)
	case 26:
		return putUint32(enc, "retry_after", m.RetryAfter)
	case 27:
		return putString(enc, "tier", string(m.Tier))
	case 28:
		return putValue(enc, "samples", m.BackfillSamples)
	case 29:
		return putInt64(enc, "cursor", m.Cursor)
	case 30:
		return putString(enc, "dim", string(m.Dim))
	case 31:
		return putInt64(enc, "from_ts", m.FromTS)
	case 32:
		return putInt64(enc, "to_ts", m.ToTS)
	case 33:
		return putUint32(enc, "max_points", m.MaxPoints)
	case 34:
		return putValue(enc, "points", m.HistoryPoints)
	case 35:
		return putBool(enc, "truncated", *m.Truncated)
	}
	return nil
}

// encodeSessionField writes one of fields 36–45: enrolment, relay and update delivery.
func (m *ControlMessage) encodeSessionField(enc *msgpack.Encoder, i int) error {
	switch i {
	case 36:
		return putString(enc, "token", string(m.Token))
	case 37:
		return putString(enc, "relay_url", string(m.RelayURL))
	case 38:
		return putString(enc, "reason", string(m.Reason))
	case 39:
		return putValue(enc, "permissions", m.Permissions)
	case 40:
		return putString(enc, "version", string(m.Version))
	case 41:
		return putString(enc, "url", string(m.URL))
	case 42:
		return putString(enc, "sha256", string(m.SHA256))
	case 43:
		return putString(enc, "signature", string(m.Signature))
	case 44:
		return putBool(enc, "success", *m.Success)
	case 45:
		return putString(enc, "error", string(m.AckError))
	}
	return nil
}

// encodeInteractionField writes one of fields 46–60: screen, input, terminal, files and chat.
func (m *ControlMessage) encodeInteractionField(enc *msgpack.Encoder, i int) error {
	switch i {
	case 46:
		return putString(enc, "sdp_offer", string(m.SDPOffer))
	case 47:
		return putString(enc, "candidate", string(m.Candidate))
	case 48:
		return putString(enc, "mid", string(m.Mid))
	case 49:
		return putUint16(enc, "x", m.X)
	case 50:
		return putUint16(enc, "y", m.Y)
	case 51:
		return putString(enc, "button", string(m.Button))
	case 52:
		return putBool(enc, "pressed", *m.Pressed)
	case 53:
		return putString(enc, "key", string(m.Key))
	case 54:
		return putUint16(enc, "cols", m.Cols)
	case 55:
		return putUint16(enc, "rows", m.Rows)
	case 56:
		return putString(enc, "path", string(m.Path))
	case 57:
		return putValue(enc, "entries", m.Entries)
	case 58:
		return putUint64(enc, "total_size", m.TotalSize)
	case 59:
		return putString(enc, "text", string(m.Text))
	case 60:
		return putString(enc, "sender", string(m.Sender))
	}
	return nil
}

// encodeHardwareField writes one of fields 61–69: what the machine is made of.
func (m *ControlMessage) encodeHardwareField(enc *msgpack.Encoder, i int) error {
	switch i {
	case 61:
		return putString(enc, "cpu_model", string(m.CPUModel))
	case 62:
		return putUint32(enc, "cpu_cores", m.CPUCores)
	case 63:
		return putUint64(enc, "ram_total_mb", m.RAMTotalMB)
	case 64:
		return putUint64(enc, "disk_total_mb", m.DiskTotalMB)
	case 65:
		return putUint64(enc, "disk_free_mb", m.DiskFreeMB)
	case 66:
		return putValue(enc, "network_interfaces", m.NetworkInterfaces)
	case 67:
		return putString(enc, "system_uuid", string(m.SystemUUID))
	case 68:
		return putBool(enc, "amt_available", *m.AMTAvailable)
	case 69:
		return putString(enc, "amt_version", string(m.AMTVersion))
	}
	return nil
}

// encodeInventoryField writes one of fields 70–86: logs and the software inventory read off the machine.
func (m *ControlMessage) encodeInventoryField(enc *msgpack.Encoder, i int) error {
	switch i {
	case 70:
		return putString(enc, "log_level", string(m.LogLevel))
	case 71:
		return putString(enc, "time_from", string(m.TimeFrom))
	case 72:
		return putString(enc, "time_to", string(m.TimeTo))
	case 73:
		return putString(enc, "search", string(m.Search))
	case 74:
		return putUint32(enc, "log_offset", m.LogOffset)
	case 75:
		return putUint32(enc, "log_limit", m.LogLimit)
	case 76:
		return putString(enc, "source", string(m.Source))
	case 77:
		return putString(enc, "unit", string(m.Unit))
	case 78:
		return putValue(enc, "log_entries", m.LogEntries)
	case 79:
		return putUint32(enc, "total_count", m.TotalCount)
	case 80:
		return putBool(enc, "has_more", *m.HasMore)
	case 81:
		return putValue(enc, "available_units", m.AvailableUnits)
	case 82:
		return putValue(enc, "ports", m.Ports)
	case 83:
		return putValue(enc, "services", m.Services)
	case 84:
		return putValue(enc, "db_engines", m.DBEngines)
	case 85:
		return putValue(enc, "containers", m.Containers)
	case 86:
		return putValue(enc, "packages", m.Packages)
	}
	return nil
}

// encodeAlertField writes one of fields 87–99: one alert and the evidence frozen with it.
func (m *ControlMessage) encodeAlertField(enc *msgpack.Encoder, i int) error {
	switch i {
	case 87:
		return putBool(enc, "enabled", *m.Enabled)
	case 88:
		return putString(enc, "alert_id", string(m.AlertID))
	case 89:
		return putString(enc, "rule_id", string(m.RuleID))
	case 90:
		return putUint32(enc, "rule_version", m.RuleVersion)
	case 91:
		return putString(enc, "severity", string(*m.Severity))
	case 92:
		return putString(enc, "metric", string(m.Metric))
	case 93:
		return putFloat64(enc, "value", *m.Value)
	case 94:
		return putInt64(enc, "window_start_ts", m.WindowStartTS)
	case 95:
		return putInt64(enc, "window_end_ts", m.WindowEndTS)
	case 96:
		return putInt64(enc, "observed_ts", m.ObservedTS)
	case 97:
		return putBool(enc, "backfilled", *m.Backfilled)
	case 98:
		return putString(enc, "evidence_codec", string(m.EvidenceCodec))
	case 99:
		return putBytes(enc, "evidence", m.Evidence)
	}
	return nil
}
