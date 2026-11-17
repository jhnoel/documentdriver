package read

import "github.com/jhnoel/documentdriver/internal/vsdx/vsd"

// DefaultLimit is find's default --limit.
const DefaultLimit = 10000

// FindRequest is a find command.
type FindRequest struct {
	Query      string
	IgnoreCase bool // casefold.Simple on both sides
	Page       string
	Limit      int // 0: no limit
}

// FindResult is find's output.
type FindResult struct {
	Base      string   `json:"base"`
	Matches   []Match  `json:"matches"`
	Truncated *FindCut `json:"truncated,omitempty"`
	Notes     []Note   `json:"notes"`
}

// Match is one match: the shape (its text matched) or the shape data
// row (its value matched), and that text as read shows it.
type Match struct {
	At   string `json:"at"`
	Text string `json:"text"`
}

// FindCut names the last match returned when --limit cut the search.
type FindCut struct {
	After string `json:"after"`
}

// Find searches the shapes' effective text and shape data values of
// every page in pages.xml order (or one page), one page tree at a time.
// The empty query matches every shape with text and every row with a
// value. Chunk R3.
func Find(input []byte, base string, req FindRequest) (*FindResult, error) {
	_, _, _ = input, base, req
	return nil, vsd.ErrNotImplemented
}

// XML returns the exact source bytes of the addressed shape (page or
// master shape), its nested shapes included. Chunk R3.
func XML(input []byte, address string) ([]byte, error) {
	_, _ = input, address
	return nil, vsd.ErrNotImplemented
}
