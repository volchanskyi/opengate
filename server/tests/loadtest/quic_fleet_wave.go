package main

import "time"

// waveEntry is one machine's account for its wave: whether its turn came, and how it finished.
type waveEntry struct {
	began  bool
	online time.Duration
	// arrived marks the machine's first registration, and ended the end of its life.
	arrived bool
	ended   bool
}

// noteTurn marks a machine whose turn came, so its wave counts it as dialled.
func (f *QUICFleet) noteTurn(index int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.waves[index].began = true
}

// noteEnd closes the account of a machine wound down before its turn came.
func (f *QUICFleet) noteEnd(index int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.waves[index].ended = true
}

// Asked is how many machines the fleet has been asked for, the mark a wave is read from.
func (f *QUICFleet) Asked() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.next
}

// Wave is what became of the machines asked for from the first mark up to the second.
func (f *QUICFleet) Wave(from, to int) Wave {
	f.mu.Lock()
	defer f.mu.Unlock()
	var wave Wave
	for _, entry := range f.waves[max(from, 0):min(to, len(f.waves))] {
		if entry.began {
			wave.Dialled++
		}
		switch {
		case entry.arrived:
			wave.Online = append(wave.Online, entry.online)
		case !entry.ended:
			wave.Pending++
		}
	}
	return wave
}
