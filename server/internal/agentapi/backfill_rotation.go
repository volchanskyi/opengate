package agentapi

import (
	"container/list"

	"github.com/google/uuid"
)

// customerTurns is one waiting customer's place in the rotation.
type customerTurns struct {
	// machines counts the customer's grants; held its bytes in flight.
	machines int
	held     int
	credit   int
	// asked holds the customer's machines refused while throttled, in the order they asked.
	asked    *list.List
	askedPos map[uuid.UUID]*list.Element
	// seq numbers each machine as it joins asked, which spreads their retries by position.
	seq     map[uuid.UUID]int
	nextSeq int
}

// join puts a machine at the back of its customer's rotation unless it is already waiting.
func (c *customerTurns) join(machine uuid.UUID) {
	if _, waiting := c.askedPos[machine]; waiting {
		return
	}
	c.askedPos[machine] = c.asked.PushBack(machine)
	c.seq[machine] = c.nextSeq
	c.nextSeq++
}

// leave removes a machine from its customer's rotation.
func (c *customerTurns) leave(machine uuid.UUID) {
	if at, waiting := c.askedPos[machine]; waiting {
		c.asked.Remove(at)
		delete(c.askedPos, machine)
		delete(c.seq, machine)
	}
}

// next reports whether it is this machine's turn: nobody waits, or it waited longest.
func (c *customerTurns) next(machine uuid.UUID) bool {
	front := c.asked.Front()
	return front == nil || front.Value.(uuid.UUID) == machine
}

// position is a waiting machine's place in its customer's rotation and how many wait.
func (c *customerTurns) position(machine uuid.UUID) (pos, waiting int) {
	front := c.asked.Front()
	if front == nil {
		return 0, 0
	}
	head := c.seq[front.Value.(uuid.UUID)]
	return c.seq[machine] - head, c.nextSeq - head
}
