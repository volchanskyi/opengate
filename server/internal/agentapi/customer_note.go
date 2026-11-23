package agentapi

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// customerNote remembers each machine's customer so the sign-in line seats a returning machine in
// its customer's turn before its sign-in reads the customer. It orders the line and nothing else.
type customerNote struct {
	mu sync.RWMutex
	of map[protocol.DeviceID]uuid.UUID
}

// customerOf is the machine's customer as last noted, the zero ID for one never noted.
func (n *customerNote) customerOf(id protocol.DeviceID) uuid.UUID {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.of[id]
}

// remember notes the machine's customer.
func (n *customerNote) remember(id protocol.DeviceID, customer uuid.UUID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.of == nil {
		n.of = make(map[protocol.DeviceID]uuid.UUID)
	}
	n.of[id] = customer
}

// forget drops the machine from the note.
func (n *customerNote) forget(id protocol.DeviceID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.of, id)
}

// WarmCustomerNote notes every tenant's machines with their customers, each tenant read inside its
// own boundary, before the server takes machines. Without a customer directory it notes nothing.
func (s *AgentServer) WarmCustomerNote(ctx context.Context) error {
	if s.customers == nil {
		return nil
	}
	customers, err := s.customers.AllCustomers(dbtx.WithDefaultTenant(ctx, true))
	if err != nil {
		return fmt.Errorf("list customers: %w", err)
	}
	read := make(map[uuid.UUID]bool)
	for _, c := range customers {
		if read[c.TenantID] {
			continue
		}
		read[c.TenantID] = true
		listed, err := s.devices.CustomerOfEach(dbtx.WithTenant(ctx, c.TenantID, false))
		if err != nil {
			return fmt.Errorf("list the machines of tenant %s: %w", c.TenantID, err)
		}
		for id, customer := range listed {
			s.customerNote.remember(id, customer)
		}
	}
	return nil
}
