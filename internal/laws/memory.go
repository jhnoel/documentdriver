package laws

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// The memory budget (make scaling): an operation run once on an input of
// MemoryUnits units may hold at most a set peak of live heap and allocate
// at most a set number of bytes. The budgets are today's numbers with a
// margin, lowered as the engine shrinks, so a change cannot make memory
// worse unnoticed. AGENTS.md (Performance and memory) gives the rule;
// docs/design.md the numbers.
//
// Peak live heap is measured at checkpoints: the operation calls the
// mark function it is given (an Options field of the command, such as
// apply.Options.Checkpoint) where its working set is largest, and each
// mark runs a full collection and reads the live heap. That is
// deterministic, unlike sampling a running program: the same build gives
// the same numbers on a loaded machine.

// MemoryUnits is the input size, in units, the memory budgets hold at.
const MemoryUnits = 4000

// Memory is what one run of an operation held and allocated.
type Memory struct {
	// PeakLive is the largest live heap at a checkpoint, less the live
	// heap before the run (what the caller already held), and 0 when the
	// heap is below that: an input held in memory that the run stops
	// using before a mark is freed there, and offsets what the run holds.
	// A run over an in-memory input keeps it alive past its last mark
	// (runtime.KeepAlive); so must anything a mark should count that is
	// only used after it in part (a string whose length alone is read).
	// At is the checkpoint it was found at.
	PeakLive uint64
	At       string
	// Alloc is the bytes the run allocated, in total.
	Alloc uint64
	// Marks lists each checkpoint and its live heap, for the log.
	Marks []string
}

// Budget is the most an operation may use: peak live heap and total
// allocation, in bytes.
type Budget struct {
	PeakLive, Alloc uint64
}

// MB is n mebibytes.
func MB(n float64) uint64 { return uint64(n * (1 << 20)) }

// MeasureMemory runs f once, giving it the checkpoint function, and
// returns its peak live heap over the checkpoints and its allocation.
func MeasureMemory(f func(mark func(string)) error) (Memory, error) {
	var m Memory
	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	base, before := ms.HeapAlloc, ms.TotalAlloc
	mark := func(name string) {
		runtime.GC()
		runtime.ReadMemStats(&ms)
		live := ms.HeapAlloc - min(base, ms.HeapAlloc)
		m.Marks = append(m.Marks, fmt.Sprintf("%s %s", name, mb(live)))
		if live > m.PeakLive || m.At == "" {
			m.PeakLive, m.At = live, name
		}
	}
	err := f(mark)
	runtime.ReadMemStats(&ms)
	if err != nil {
		return Memory{}, err
	}
	m.Alloc = ms.TotalAlloc - before
	return m, nil
}

// WithinMemory measures run (once warm) and fails t when it holds or
// allocates more than b, or marks no checkpoint. It is skipped under the
// race detector, whose shadow memory and allocations are not the
// program's.
func WithinMemory(t *testing.T, name string, b Budget, run func(mark func(string)) error) {
	t.Helper()
	if RaceEnabled {
		t.Skip("memory is not measured under the race detector")
	}
	if err := run(func(string) {}); err != nil { // warm: lazy tables, first-use caches
		t.Fatalf("%s: %v", name, err)
	}
	m, err := MeasureMemory(run)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(m.Marks) == 0 {
		t.Fatalf("%s marked no checkpoint", name)
	}
	t.Logf("%s at %d units: peak live %s at %s (budget %s), allocated %s (budget %s); checkpoints: %s", name, MemoryUnits,
		mb(m.PeakLive), m.At, mb(b.PeakLive), mb(m.Alloc), mb(b.Alloc), strings.Join(m.Marks, ", "))
	if m.PeakLive > b.PeakLive {
		t.Errorf("%s holds %s of live heap at %d units (at %s), over its budget of %s", name, mb(m.PeakLive), MemoryUnits, m.At, mb(b.PeakLive))
	}
	if m.Alloc > b.Alloc {
		t.Errorf("%s allocates %s at %d units, over its budget of %s", name, mb(m.Alloc), MemoryUnits, mb(b.Alloc))
	}
}
