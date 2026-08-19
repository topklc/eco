package transcript

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The record: one text file per scenario holding everything the core told the
// UI, step by step, and the state it ended in. It is the reference the dual-core
// gate used to compute live from the v1 Session - pinned to disk instead, so it
// survives the core that produced it.
//
// -update rewrites the records instead of comparing against them:
//
//	go test ./internal/transcript -update
//
// It is not a way to make a red test green. A diff here is a behaviour change;
// re-recording one without reading it is how a suite stops meaning anything.
var updateRecords = flag.Bool("update", false, "rewrite the recorded transcripts in testdata")

// record renders a run as the text that gets checked in. Every fact is one
// line, so a behaviour change shows up as a line diff a reviewer can read
// without running anything.
func record(name string, run coreRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# scenario %s: what the core told the UI, and where it ended up.\n", name)
	b.WriteString("# Recorded from internal/irc + internal/state. Regenerate with\n")
	b.WriteString("# `go test ./internal/transcript -update`, and read doc.go before you do.\n")
	for _, g := range run.groups {
		fmt.Fprintf(&b, "\nstream %s\n", escape(g.label))
		for _, f := range g.facts {
			fmt.Fprintf(&b, "\t%s\n", escape(f.String()))
		}
	}
	for _, s := range run.state {
		fmt.Fprintf(&b, "\nstate %s\n", escape(s.header()))
		for _, f := range s.Lines {
			fmt.Fprintf(&b, "\t%s\n", escape(f.String()))
		}
	}
	return b.String()
}

// escape keeps a record readable. The step labels are raw protocol lines, and a
// CTCP \x01 written straight into a checked-in file is a byte nobody reviewing
// the diff can see. Fact text is already quoted by Fact.String.
func escape(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			fmt.Fprintf(&b, "\\x%02x", r)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// checkRecord compares a run against its record, or rewrites it under -update.
func checkRecord(t *testing.T, name string, run coreRun) {
	t.Helper()
	got := record(name, run)
	path := filepath.Join("testdata", name+".txt")
	if *updateRecords {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no recorded transcript for scenario %q (%v).\n"+
			"A new scenario is recorded with:\n\tgo test ./internal/transcript -update\n"+
			"and the recording is then reviewed line by line, because it is what the "+
			"suite will defend from here on.", name, err)
	}
	if string(want) != got {
		t.Fatal("\n" + diffRecords(name, string(want), got))
	}
}

// maxDiffLines bounds a reported diff. A record is a couple of hundred lines,
// and a core that broke early would otherwise print all of them.
const maxDiffLines = 40

// diffRecords says where the run stopped matching its record, line by line.
//
// The alignment matters more than it looks: a single dropped fact shifts
// everything after it, and a report that called the whole remainder "changed"
// would bury the one line that is the regression. So the two sides are trimmed
// to their common prefix and suffix and then aligned on their longest common
// subsequence - the changed lines, and only those, are what gets printed.
func diffRecords(name, want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	head := 0
	for head < len(w) && head < len(g) && w[head] == g[head] {
		head++
	}
	tail := 0
	for tail < len(w)-head && tail < len(g)-head && w[len(w)-1-tail] == g[len(g)-1-tail] {
		tail++
	}

	var b strings.Builder
	fmt.Fprintf(&b, "scenario %q no longer behaves the way testdata/%s.txt records\n",
		name, name)
	fmt.Fprintf(&b, "  (- recorded, + now; line numbers are the record's)\n")
	for i := max(0, head-2); i < head; i++ {
		fmt.Fprintf(&b, "    %4d   %s\n", i+1, w[i])
	}
	shown := 0
	for _, e := range alignLines(w[head:len(w)-tail], g[head:len(g)-tail]) {
		if shown == maxDiffLines {
			fmt.Fprintf(&b, "    ...    (more differences follow)\n")
			break
		}
		shown++
		fmt.Fprintf(&b, "    %4d %s %s\n", head+e.at+1, e.sign, e.line)
	}
	if shown == 0 {
		b.WriteString("    (the records differ only in whitespace)\n")
	}
	b.WriteString("\nEither the core changed by accident - which is the regression this record " +
		"exists\nto catch - or the change is deliberate, in which case re-record with " +
		"`-update`,\nread every line of the diff, and say why in the blueprint.\n")
	return b.String()
}

// edit is one differing line: '-' for the record's, '+' for this run's.
type edit struct {
	sign string
	at   int // index within the changed region, for a line number
	line string
}

// alignLines diffs two line slices through their longest common subsequence.
// The regions handed to it are what is left after the common prefix and suffix
// are trimmed, so the table stays small.
func alignLines(a, b []string) []edit {
	// lcs[i][j] = length of the longest common subsequence of a[i:] and b[j:]
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
				continue
			}
			lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
		}
	}
	var out []edit
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, edit{"-", i, a[i]})
			i++
		default:
			out = append(out, edit{"+", i, b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		out = append(out, edit{"-", i, a[i]})
	}
	for ; j < len(b); j++ {
		out = append(out, edit{"+", i, b[j]})
	}
	return out
}
