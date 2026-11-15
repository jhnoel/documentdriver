package main

import (
	"bytes"
	"testing"
)

func TestRunExitCodes(t *testing.T) {
	cases := []struct {
		args []string
		want int
	}{
		{nil, 2},
		{[]string{"help"}, 0},
		{[]string{"read"}, 2},
		{[]string{"help", "ops"}, 0},
		{[]string{"help", "object.add"}, 0},
		{[]string{"--version"}, 0},
		{[]string{"apply", "--help"}, 0},
		{[]string{"bogus"}, 2},
	}
	for _, c := range cases {
		var out, errb bytes.Buffer
		if got := run(c.args, &out, &errb); got != c.want {
			t.Errorf("run(%v) = %d, want %d", c.args, got, c.want)
		}
	}
}
