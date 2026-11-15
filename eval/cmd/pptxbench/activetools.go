package main

import (
	"encoding/json"
	"strings"
)

// activeTools reads the tool set the extension reported on stderr
// ("pptxbench-tools: [...]"); nil when it did not.
func activeTools(stderr string) []string {
	for _, l := range strings.Split(stderr, "\n") {
		if rest, ok := strings.CutPrefix(l, "pptxbench-tools: "); ok {
			var names []string
			if json.Unmarshal([]byte(rest), &names) == nil {
				return names
			}
		}
	}
	return nil
}
