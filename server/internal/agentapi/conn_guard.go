package agentapi

import (
	"errors"
	"fmt"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// controlField pairs a wire field name with its value so a guard can name the offending field.
type controlField struct {
	name  string
	value string
}

// requireNonEmptyFields reports the first empty load-bearing field, since the encoder drops zero
// values and the agent's decoder rejects the frame.
func requireNonEmptyFields(msgType protocol.ControlMessageType, fields ...controlField) error {
	for _, f := range fields {
		if f.value == "" {
			return fmt.Errorf("%w: %s.%s is empty", ErrIncompleteControlMessage, msgType, f.name)
		}
	}
	return nil
}

// IsIncompleteMessageError reports whether err means a control message was refused for an empty
// load-bearing field.
func IsIncompleteMessageError(err error) bool {
	return errors.Is(err, ErrIncompleteControlMessage)
}
