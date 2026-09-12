package main

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"sort"
	"strings"
	"testing"
)

// The options in json.go are load-bearing, and each of these tests fails if
// one of them is dropped. v2 defaults would randomize key order, stop
// escaping HTML, and reject the two input defects that extract is meant to
// survive rather than abort on.

func TestMarshalKeyOrderIsDeterministic(t *testing.T) {
	rec := Record{}
	want := []string{}
	for _, k := range []string{"zulu", "alpha", "mike", "bravo", "yankee", "delta", "oscar", "kilo", "echo", "tango"} {
		rec[k] = 1
		want = append(want, k)
	}
	sort.Strings(want)

	first, err := jsonv2.Marshal(rec, marshalOptions)
	if err != nil {
		t.Fatal(err)
	}
	// Go randomizes map iteration, so a missing Deterministic option shows up
	// within a few dozen runs.
	for i := 0; i < 200; i++ {
		got, err := jsonv2.Marshal(rec, marshalOptions)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, first) {
			t.Fatalf("key order is not stable:\n run 0: %s\n run %d: %s", first, i, got)
		}
	}

	// Stable is not enough; the order must be sorted, as v1 emitted it.
	var keys []string
	for _, part := range strings.Split(strings.Trim(string(first), "{}"), ",") {
		keys = append(keys, strings.Trim(strings.SplitN(part, ":", 2)[0], `"`))
	}
	if !sort.StringsAreSorted(keys) {
		t.Errorf("keys are not sorted: %v", keys)
	}
}

func TestMarshalEscapesHTML(t *testing.T) {
	out, err := jsonv2.Marshal(Record{"v": "a & b <tag>"}, marshalOptions)
	if err != nil {
		t.Fatal(err)
	}
	// v1 escaped these as &, < and >; keeping that means
	// extracts cached from older versions still diff clean. Asserted as a
	// property so the expectation does not need escaped literals of its own.
	got := string(out)
	for _, raw := range []string{"&", "<", ">"} {
		if strings.Contains(got, raw) {
			t.Errorf("%q should have been escaped, got %s", raw, got)
		}
	}
	for _, esc := range []string{"u0026", "u003c", "u003e"} {
		if !strings.Contains(got, esc) {
			t.Errorf("expected %s in the output, got %s", esc, got)
		}
	}
}

func TestMarshalSurvivesInvalidUTF8(t *testing.T) {
	// doctor reports such a byte; extract must still produce output.
	out, err := jsonv2.Marshal(Record{"v": "vor\xffnach"}, marshalOptions)
	if err != nil {
		t.Fatalf("invalid UTF-8 must not fail the marshal: %v", err)
	}
	if !strings.Contains(string(out), "�") {
		t.Errorf("expected the replacement character, got %s", out)
	}
}

func TestUnmarshalToleratesSourceDefects(t *testing.T) {
	// A collection index belongs to the tool that writes it. Both defects are
	// reported by doctor, never a reason to drop the record here.
	var rec map[string]any
	if err := jsonv2.Unmarshal([]byte(`{"k":"erst","k":"zweit"}`), &rec, unmarshalOptions); err != nil {
		t.Fatalf("duplicate key must be accepted: %v", err)
	}
	if rec["k"] != "zweit" {
		t.Errorf("last value should win, got %v", rec["k"])
	}

	rec = nil
	if err := jsonv2.Unmarshal([]byte("{\"k\":\"vor\xffnach\"}"), &rec, unmarshalOptions); err != nil {
		t.Fatalf("invalid UTF-8 must be accepted: %v", err)
	}
}

func TestRecordEncoderWritesOneLinePerRecord(t *testing.T) {
	var buf bytes.Buffer
	enc := newRecordEncoder(&buf)
	for _, r := range []Record{{"a": 1}, {"b": 2}} {
		if err := enc.Encode(r); err != nil {
			t.Fatal(err)
		}
	}
	if want := "{\"a\":1}\n{\"b\":2}\n"; buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}
