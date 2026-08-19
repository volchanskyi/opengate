package rules

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// The labels a customer's machines are picked out by.
//
// A label is a flat key and value — `role=file-server`, `env=production` — and
// it cuts across the tenancy ladder rather than sitting on a rung of it. That is
// the whole point of it: a disk threshold is meant for the file servers, and the
// file servers are in four offices. No rung names that set, and inventing a
// rung for it would make every machine's place in the fleet depend on what
// somebody once wanted to tune.
//
// The values come from a list each customer maintains rather than being typed
// in, because a targeting dimension with free-text values is a dimension where
// `production`, `Production` and `prod` are three different estates and a
// threshold reaches a third of the machines it was meant for.

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

// Label is one entry in a customer's list: a key, a value, and the identity a
// machine is assigned it by.
type Label struct {
	ID uuid.UUID
	// OrganizationID is the customer whose list this belongs to. A label is
	// never shared between customers even inside one tenant — two of them using
	// the word `production` are describing two estates.
	OrganizationID uuid.UUID
	Key            string
	Value          string
	// CreatedBy names whoever added it, so a targeting dimension nobody
	// remembers agreeing to can still be traced.
	CreatedBy string
}

// Selector renders the label as the predicate a binding aims with, which is what
// makes "is this label in use" a question about stored selectors.
func (l Label) Selector() Selector { return Selector{l.Key: l.Value} }

// ValidateLabel bounds a label. Both halves travel to every agent that carries
// the label and are matched against a selector, so both carry the selector's own
// limits rather than limits of their own.
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
