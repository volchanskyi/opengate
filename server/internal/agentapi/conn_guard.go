package agentapi

import (
	"errors"
	"fmt"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// controlField pairs a wire field name with the value bound for it, so a guard
// can name the offending field in its error.
type controlField struct {
	name  string
	value string
}

// requireNonEmptyFields refuses a control message whose load-bearing fields are
// not all populated. The encoder drops a zero-valued field from the wire map
// and the agent's decoder requires these fields, so the resulting frame is
// undecodable: it breaks the agent's control loop and forces a full reconnect.
// Keeping the failure on the server side means an agent already in the field
// never sees the frame. Fields are checked in argument order and the first
// empty one is reported.
func requireNonEmptyFields(msgType protocol.ControlMessageType, fields ...controlField) error {
	for _, f := range fields {
		if f.value == "" {
			return fmt.Errorf("%w: %s.%s is empty", ErrIncompleteControlMessage, msgType, f.name)
		}
	}
	return nil
}

// IsIncompleteMessageError reports whether err means the server refused to send
// a control variant because a load-bearing field was empty.
func IsIncompleteMessageError(err error) bool {
	return errors.Is(err, ErrIncompleteControlMessage)
}
