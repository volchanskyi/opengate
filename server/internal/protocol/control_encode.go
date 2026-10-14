package protocol

import "github.com/vmihailenco/msgpack/v5"

// controlFieldCount is the number of msgpack-encoded fields ControlMessage declares; the
// positionally indexed controlFieldPresence and encodeControlField must track the struct.
const controlFieldCount = 102

// Each put* helper writes one map entry through the encoder method the reflection encoder picks
// for that Go type, keeping the integer widths and so the bytes identical.

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

// putValue writes the slice and struct-pointer fields through the reflection encoder.
func putValue(enc *msgpack.Encoder, key string, val any) error {
	if err := enc.EncodeString(key); err != nil {
		return err
	}
	return enc.Encode(val)
}

// EncodeMsgpack writes only the populated fields as a msgpack map, testing emptiness with typed
// comparisons so the cost tracks the fields set; the bytes match the reflection encoder's.
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

// controlFieldPresence reports, per field position, whether msgpack would emit the field under
// omitempty; the value array stays on the stack.
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
	present[14] = m.SinceTS != 0
	present[15] = m.Limit != 0
	present[16] = len(m.Summaries) != 0
	present[17] = len(m.Breaches) != 0
	present[18] = len(m.AlertRules) != 0
	present[19] = len(m.RuleCoverage) != 0
	present[20] = m.DeviceHourlyCeiling != 0
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
	present[39] = len(m.IceServers) != 0
	present[40] = m.Version != ""
	present[41] = m.URL != ""
	present[42] = m.SHA256 != ""
	present[43] = m.Signature != ""
	present[44] = m.Success != nil
	present[45] = m.AckError != ""
	present[46] = m.SDP != ""
	present[47] = m.Candidate != ""
	present[48] = m.Mid != ""
	present[49] = m.Received != 0
	present[50] = m.X != 0
	present[51] = m.Y != 0
	present[52] = m.Button != ""
	present[53] = m.Pressed != nil
	present[54] = m.Key != ""
	present[55] = m.Cols != 0
	present[56] = m.Rows != 0
	present[57] = m.Path != ""
	present[58] = len(m.Entries) != 0
	present[59] = m.More
	present[60] = m.TotalSize != 0
	present[61] = m.Text != ""
	present[62] = m.Sender != ""
	present[63] = m.CPUModel != ""
	present[64] = m.CPUCores != 0
	present[65] = m.RAMTotalMB != 0
	present[66] = m.DiskTotalMB != 0
	present[67] = m.DiskFreeMB != 0
	present[68] = len(m.NetworkInterfaces) != 0
	present[69] = m.SystemUUID != ""
	present[70] = m.AMTAvailable != nil
	present[71] = m.AMTVersion != ""
	present[72] = m.LogLevel != ""
	present[73] = m.TimeFrom != ""
	present[74] = m.TimeTo != ""
	present[75] = m.Search != ""
	present[76] = m.LogOffset != 0
	present[77] = m.LogLimit != 0
	present[78] = m.Source != ""
	present[79] = m.Unit != ""
	present[80] = len(m.LogEntries) != 0
	present[81] = m.TotalCount != 0
	present[82] = m.HasMore != nil
	present[83] = len(m.AvailableUnits) != 0
	present[84] = len(m.Ports) != 0
	present[85] = len(m.Services) != 0
	present[86] = len(m.DBEngines) != 0
	present[87] = len(m.Containers) != 0
	present[88] = len(m.Packages) != 0
	present[89] = m.Enabled != nil
	present[90] = m.AlertID != ""
	present[91] = m.RuleID != ""
	present[92] = m.RuleVersion != 0
	present[93] = m.Severity != nil
	present[94] = m.Metric != ""
	present[95] = m.Value != nil
	present[96] = m.WindowStartTS != 0
	present[97] = m.WindowEndTS != 0
	present[98] = m.ObservedTS != 0
	present[99] = m.Backfilled != nil
	present[100] = m.EvidenceCodec != ""
	present[101] = len(m.Evidence) != 0
	return present
}

// encodeControlField writes the field at position i through the group that owns it; the groups
// follow declaration order, so wire position matches declared position.
func (m *ControlMessage) encodeControlField(enc *msgpack.Encoder, i int) error {
	switch {
	case i <= 7:
		return m.encodeEnvelopeField(enc, i)
	case i <= 34:
		return m.encodeTelemetryField(enc, i)
	case i <= 45:
		return m.encodeSessionField(enc, i)
	case i <= 62:
		return m.encodeInteractionField(enc, i)
	case i <= 71:
		return m.encodeHardwareField(enc, i)
	case i <= 88:
		return m.encodeInventoryField(enc, i)
	case i <= 101:
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

// encodeTelemetryField writes one of fields 8–34: vitals, summaries, breaches and windows.
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
		return putInt64(enc, "since_ts", m.SinceTS)
	case 15:
		return putUint32(enc, "limit", m.Limit)
	case 16:
		return putValue(enc, "summaries", m.Summaries)
	case 17:
		return putValue(enc, "breaches", m.Breaches)
	case 18:
		return putValue(enc, "rules", m.AlertRules)
	case 19:
		return putValue(enc, "rule_coverage", m.RuleCoverage)
	case 20:
		return putUint32(enc, "device_hourly_ceiling", m.DeviceHourlyCeiling)
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
	}
	return nil
}

// encodeSessionField writes one of fields 35–45: enrolment, relay and update delivery.
func (m *ControlMessage) encodeSessionField(enc *msgpack.Encoder, i int) error {
	switch i {
	case 35:
		return putString(enc, "token", string(m.Token))
	case 36:
		return putString(enc, "relay_url", string(m.RelayURL))
	case 37:
		return putString(enc, "reason", string(m.Reason))
	case 38:
		return putValue(enc, "permissions", m.Permissions)
	case 39:
		return putValue(enc, "ice_servers", m.IceServers)
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

// encodeInteractionField writes one of fields 46–62: screen, input, terminal, files and chat.
func (m *ControlMessage) encodeInteractionField(enc *msgpack.Encoder, i int) error {
	switch i {
	case 46:
		return putString(enc, "sdp", string(m.SDP))
	case 47:
		return putString(enc, "candidate", string(m.Candidate))
	case 48:
		return putString(enc, "mid", string(m.Mid))
	case 49:
		return putUint64(enc, "received", m.Received)
	case 50:
		return putUint16(enc, "x", m.X)
	case 51:
		return putUint16(enc, "y", m.Y)
	case 52:
		return putString(enc, "button", string(m.Button))
	case 53:
		return putBool(enc, "pressed", *m.Pressed)
	case 54:
		return putString(enc, "key", string(m.Key))
	case 55:
		return putUint16(enc, "cols", m.Cols)
	case 56:
		return putUint16(enc, "rows", m.Rows)
	case 57:
		return putString(enc, "path", string(m.Path))
	case 58:
		return putValue(enc, "entries", m.Entries)
	case 59:
		return putBool(enc, "more", m.More)
	case 60:
		return putUint64(enc, "total_size", m.TotalSize)
	case 61:
		return putString(enc, "text", string(m.Text))
	case 62:
		return putString(enc, "sender", string(m.Sender))
	}
	return nil
}

// encodeHardwareField writes one of fields 63–71: what the machine is made of.
func (m *ControlMessage) encodeHardwareField(enc *msgpack.Encoder, i int) error {
	switch i {
	case 63:
		return putString(enc, "cpu_model", string(m.CPUModel))
	case 64:
		return putUint32(enc, "cpu_cores", m.CPUCores)
	case 65:
		return putUint64(enc, "ram_total_mb", m.RAMTotalMB)
	case 66:
		return putUint64(enc, "disk_total_mb", m.DiskTotalMB)
	case 67:
		return putUint64(enc, "disk_free_mb", m.DiskFreeMB)
	case 68:
		return putValue(enc, "network_interfaces", m.NetworkInterfaces)
	case 69:
		return putString(enc, "system_uuid", string(m.SystemUUID))
	case 70:
		return putBool(enc, "amt_available", *m.AMTAvailable)
	case 71:
		return putString(enc, "amt_version", string(m.AMTVersion))
	}
	return nil
}

// encodeInventoryField writes one of fields 72–88: logs and the machine's software inventory.
func (m *ControlMessage) encodeInventoryField(enc *msgpack.Encoder, i int) error {
	switch i {
	case 72:
		return putString(enc, "log_level", string(m.LogLevel))
	case 73:
		return putString(enc, "time_from", string(m.TimeFrom))
	case 74:
		return putString(enc, "time_to", string(m.TimeTo))
	case 75:
		return putString(enc, "search", string(m.Search))
	case 76:
		return putUint32(enc, "log_offset", m.LogOffset)
	case 77:
		return putUint32(enc, "log_limit", m.LogLimit)
	case 78:
		return putString(enc, "source", string(m.Source))
	case 79:
		return putString(enc, "unit", string(m.Unit))
	case 80:
		return putValue(enc, "log_entries", m.LogEntries)
	case 81:
		return putUint32(enc, "total_count", m.TotalCount)
	case 82:
		return putBool(enc, "has_more", *m.HasMore)
	case 83:
		return putValue(enc, "available_units", m.AvailableUnits)
	case 84:
		return putValue(enc, "ports", m.Ports)
	case 85:
		return putValue(enc, "services", m.Services)
	case 86:
		return putValue(enc, "db_engines", m.DBEngines)
	case 87:
		return putValue(enc, "containers", m.Containers)
	case 88:
		return putValue(enc, "packages", m.Packages)
	}
	return nil
}

// encodeAlertField writes one of fields 89–101: one alert and the evidence frozen with it.
func (m *ControlMessage) encodeAlertField(enc *msgpack.Encoder, i int) error {
	switch i {
	case 89:
		return putBool(enc, "enabled", *m.Enabled)
	case 90:
		return putString(enc, "alert_id", string(m.AlertID))
	case 91:
		return putString(enc, "rule_id", string(m.RuleID))
	case 92:
		return putUint32(enc, "rule_version", m.RuleVersion)
	case 93:
		return putString(enc, "severity", string(*m.Severity))
	case 94:
		return putString(enc, "metric", string(m.Metric))
	case 95:
		return putFloat64(enc, "value", *m.Value)
	case 96:
		return putInt64(enc, "window_start_ts", m.WindowStartTS)
	case 97:
		return putInt64(enc, "window_end_ts", m.WindowEndTS)
	case 98:
		return putInt64(enc, "observed_ts", m.ObservedTS)
	case 99:
		return putBool(enc, "backfilled", *m.Backfilled)
	case 100:
		return putString(enc, "evidence_codec", string(m.EvidenceCodec))
	case 101:
		return putBytes(enc, "evidence", m.Evidence)
	}
	return nil
}
