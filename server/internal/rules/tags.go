package rules

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Typed failures, so an API layer can answer an operator with what is wrong.
var (
	// ErrInvalidLabel means the label is outside its bounds.
	ErrInvalidLabel = errors.New("label is outside its bounds")
	// ErrLabelExists means the customer's list already offers this key and value.
	ErrLabelExists = errors.New("the customer's list already offers this label")
	// ErrLabelInUse means a rule is aimed at the label, so removing it would
	// silently widen a threshold across the machines that carried it.
	ErrLabelInUse = errors.New("a rule is aimed at this label")
	// ErrLabelForeign means the label and the machine belong to different
	// customers.
	ErrLabelForeign = errors.New("the label and the machine belong to different customers")
	// ErrLabelNotFound means no label in the customer's list has that id.
	ErrLabelNotFound = errors.New("no such label")
)

// Label is one entry in a customer's list: a key, a value, and the identity a machine is assigned.
type Label struct {
	ID uuid.UUID
	// OrganizationID is the customer whose list this belongs to.
	// A label is scoped to one customer even inside a tenant.
	OrganizationID uuid.UUID
	Key            string
	Value          string
	// CreatedBy names whoever added the label.
	CreatedBy string
}

// Selector renders the label as the predicate a binding aims with.
func (l Label) Selector() Selector { return Selector{l.Key: l.Value} }

// ValidateLabel bounds a label's key and value by the selector's own limits.
func ValidateLabel(l Label) error {
	if l.OrganizationID == uuid.Nil {
		return fmt.Errorf("%w: a label belongs to a customer", ErrInvalidLabel)
	}
	if l.Key == "" {
		return fmt.Errorf("%w: a label needs a key", ErrInvalidLabel)
	}
	if len(l.Key) > maxSelectorKeyLen {
		return fmt.Errorf("%w: key is longer than %d characters", ErrInvalidLabel, maxSelectorKeyLen)
	}
	if l.Value == "" {
		return fmt.Errorf("%w: a label needs a value", ErrInvalidLabel)
	}
	if len(l.Value) > maxSelectorValueLen {
		return fmt.Errorf("%w: value is longer than %d characters", ErrInvalidLabel, maxSelectorValueLen)
	}
	return nil
}
