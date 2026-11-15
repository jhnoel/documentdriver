package laws

import (
	"math"
	"os"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	harness "github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// The corpus suites run deck by deck in parallel (internal/corpustest):
// each deck tallies into counts of its own, and the suite sums them in
// deck order afterwards. The five corpus suites run side by side
// (t.Parallel), sharing corpustest's file slots, so that one suite's
// slowest deck overlaps the others' decks; they share no written state,
// and the fixture suites, which are sequential, finish first.

// nativeFiles returns the PowerPoint-native manifest decks that are on
// disk, named relative to PPTX_CORPUS, and how many are missing.
func nativeFiles(t *testing.T) ([]corpustest.File, int) {
	t.Helper()
	var on []string
	missing := 0
	for _, d := range testdeck.Native(t) {
		if _, err := os.Stat(d); err != nil {
			missing++
			continue
		}
		on = append(on, d)
	}
	return corpustest.Files(on, os.Getenv("PPTX_CORPUS")), missing
}

func addInts(dst, src map[string]int) {
	for k, v := range src {
		dst[k] += v
	}
}

func (c *counts) add(o counts) {
	c.slides += o.slides
	c.objects += o.objects
	c.noBox += o.noBox
	c.notWritable += o.notWritable
	c.readOnly += o.readOnly
	c.setOK += o.setOK
	c.setInexpressible += o.setInexpressible
}

func addReports(dst, src map[string]*harness.Report) {
	for n, r := range src {
		if dst[n] == nil {
			dst[n] = &harness.Report{}
		}
		dst[n].Add(*r)
	}
}

func (c *m3counts) add(o *m3counts) {
	c.decks += o.decks
	c.objects += o.objects
	c.notTargetable += o.notTargetable
	addInts(c.paired, o.paired)
	c.identityFields += o.identityFields
	c.inverseOK += o.inverseOK
	c.inverseInexpressible += o.inverseInexpressible
	c.inverseBranch += o.inverseBranch
	addInts(c.inverseByCodec, o.inverseByCodec)
	addInts(c.opPlans, o.opPlans)
	c.opInexpressible += o.opInexpressible
	c.dupDeleteRename += o.dupDeleteRename
	c.addDelete += o.addDelete
	addInts(c.textRefused, o.textRefused)
	c.textReadBack += o.textReadBack
	c.textAtomic += o.textAtomic
}

// add sums o into c; untargetedBudget is per deck and not summed.
func (c *m4counts) add(o *m4counts) {
	c.decks += o.decks
	c.custom += o.custom
	c.rebuildPaths += o.rebuildPaths
	c.rebuildSame += o.rebuildSame
	c.freshSame += o.freshSame
	addInts(c.rebuildSkip, o.rebuildSkip)
	addInts(c.rebuildDiff, o.rebuildDiff)
	c.inverseOK += o.inverseOK
	c.inverseInexpressible += o.inverseInexpressible
	addInts(c.inverseRefused, o.inverseRefused)
	c.switchOK += o.switchOK
	c.untargeted += o.untargeted
	c.atomic += o.atomic
	c.pairs += o.pairs
	c.pairsIdentity += o.pairsIdentity
	c.pairsBytes += o.pairsBytes
	addInts(c.pairRefused, o.pairRefused)
	addInts(c.pairDiff, o.pairDiff)
	c.groups += o.groups
	c.ungroupOK += o.ungroupOK
	c.ungroupExact += o.ungroupExact
	addInts(c.ungroupRefused, o.ungroupRefused)
	c.fitOK += o.fitOK
	c.fitChanged += o.fitChanged
	c.fitExact += o.fitExact
	addInts(c.fitRefused, o.fitRefused)
	c.maxMoved = math.Max(c.maxMoved, o.maxMoved)
}

func (c *picCounts) add(o picCounts) {
	c.decks += o.decks
	c.pictures += o.pictures
	c.paired += o.paired
	c.identity += o.identity
	c.inverse += o.inverse
	c.inexpressible += o.inexpressible
	c.replaced += o.replaced
	c.shared += o.shared
	c.collected += o.collected
	c.kept += o.kept
	c.added += o.added
	c.atomic += o.atomic
	c.deleted += o.deleted
	c.delCollected += o.delCollected
	c.delKept += o.delKept
	c.addDeleteNoop += o.addDeleteNoop
}
