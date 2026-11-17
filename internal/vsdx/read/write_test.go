package read

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// TestWritePageGoldens checks the RM3 golden rows: WritePage into a
// buffer equals byte for byte v1-page0.json (V1 default),
// v1-page0-full.json (V1 full), v1-page0-in.json (V1 in),
// v2-page0.json, v4-page0.json and v5-page0.json, the same six rows
// TestPageGoldens checks through Page.
func TestWritePageGoldens(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fixture string
		golden  string
		opts    Options
	}{
		{"v1 default", "v1-minimal", "v1-page0.json", Options{}},
		{"v1 full", "v1-minimal", "v1-page0-full.json", Options{Detail: "full"}},
		{"v1 in", "v1-minimal", "v1-page0-in.json", Options{Units: "in"}},
		{"v2", "v2-master", "v2-page0.json", Options{}},
		{"v4", "v4-groups", "v4-page0.json", Options{}},
		{"v5", "v5-glue", "v5-page0.json", Options{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WritePage(&buf, fixtureBytes(t, tc.fixture), "BASE", "page0", tc.opts); err != nil {
				t.Fatalf("WritePage(%s): %v", tc.fixture, err)
			}
			if want := goldenBytes(t, tc.golden); buf.String() != string(want) {
				t.Errorf("WritePage = %s, want %s", buf.String(), want)
			}
		})
	}
}

// TestScalingWritePage checks the RM3 scaling row: WritePage on
// Generated(1000) and Generated(4000) (N = 1000, 4N = 4000 shapes),
// streaming to io.Discard so no output buffer is held, allocates at
// most 5.5 times as much (laws.Scales) and the laws.TimeScales time
// ratio is at most 8.
func TestScalingWritePage(t *testing.T) {
	setup := func(n int) (func() error, error) {
		data, err := os.ReadFile(vsdxtest.Generated(t, n))
		if err != nil {
			return nil, err
		}
		return func() error {
			if err := WritePage(io.Discard, data, "B", "page0", Options{}); err != nil {
				return err
			}
			return nil
		}, nil
	}
	// Sanity: the streamed bytes are the Page bytes, so the scaling
	// run measures the same result it scales.
	data, err := os.ReadFile(vsdxtest.Generated(t, 1000))
	if err != nil {
		t.Fatalf("read Generated(1000): %v", err)
	}
	var buf bytes.Buffer
	if err := WritePage(&buf, data, "B", "page0", Options{}); err != nil {
		t.Fatalf("WritePage(Generated(1000)): %v", err)
	}
	want, err := Page(data, "B", "page0", Options{})
	if err != nil {
		t.Fatalf("Page(Generated(1000)): %v", err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("WritePage(Generated(1000)) = %d bytes, Page = %d bytes, want equal", buf.Len(), len(want))
	}
	if last := fmt.Sprintf(`"page0/s%d"`, 1000+500); !bytes.Contains(buf.Bytes(), []byte(last)) {
		t.Fatalf("WritePage(Generated(1000)): output has no %s", last)
	}
	laws.Scales(t, "vsdx WritePage shapes", setup)
	laws.TimeScales(t, "vsdx WritePage shapes", setup)
}
