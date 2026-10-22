package protocol

// Application error codes a QUIC connection between agent and server closes with.
const (
	// CloseBye is a normal close by either side.
	CloseBye uint64 = 0
	// CloseStreamAcceptFailed means the server could not accept the control stream.
	CloseStreamAcceptFailed uint64 = 1
	// CloseHandshakeRefused means the handshake was refused; on the fast path, a stale master-key
	// fingerprint.
	CloseHandshakeRefused uint64 = 2
	// CloseDeviceDeleted tells the agent its device was deleted, like an AgentDeregistered frame.
	CloseDeviceDeleted uint64 = 3
	// CloseRestrictedSessionEnded ends a restricted session after it renewed or its time ran out.
	CloseRestrictedSessionEnded uint64 = 4
	// CloseCertificateRefused refuses a certificate that is too old, superseded, unvouched, or was
	// never issued by this server.
	CloseCertificateRefused uint64 = 5
	// CloseReplacedByNewerConnection closes a connection a newer one for the same device replaced.
	CloseReplacedByNewerConnection uint64 = 6
)
