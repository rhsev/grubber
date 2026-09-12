package main

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"io"
)

// grubber states its JSON semantics here rather than inheriting whichever
// defaults the toolchain happens to carry. encoding/json is by now formally
// the v1 package, implemented as v2 plus a set of options that preserve its
// historical behavior; those options are the ones written out below, so what
// used to be an implicit inheritance is now a decision the code makes.
//
// Every one of them is load-bearing:
//
//   - Deterministic: v2 does not sort map keys, and Record is a map. Without
//     this the field order of every record changes from run to run, which is
//     the churn deterministic record order was introduced to end.
//   - EscapeForHTML: v1 escapes <, > and & as <, >, &. Keeping
//     it means extracts cached from older versions still diff clean.
//   - AllowInvalidUTF8: a note can hold a byte that is not valid UTF-8
//     (doctor reports it as invalid-utf8). Extract degrades rather than
//     fails, so the byte travels out as U+FFFD instead of aborting the run.
//   - AllowDuplicateNames: a JSONL source may repeat a key. v1 took the last
//     one silently; rejecting the record instead would drop data that the
//     tool owning the index is responsible for.
var (
	// marshalOptions is the write path: extract output in every format.
	marshalOptions = jsonv2.JoinOptions(
		jsonv2.Deterministic(true),
		jsontext.EscapeForHTML(true),
		jsontext.AllowInvalidUTF8(true),
	)

	// marshalIndentOptions is the collected --format json output.
	marshalIndentOptions = jsonv2.JoinOptions(
		marshalOptions,
		jsontext.WithIndent("  "),
	)

	// unmarshalOptions is the read path: --from-jsonl sources.
	unmarshalOptions = jsonv2.JoinOptions(
		jsontext.AllowDuplicateNames(true),
		jsontext.AllowInvalidUTF8(true),
	)
)

// recordEncoder writes one JSON value per line. v2 marshals the value alone,
// so the newline that v1's Encoder.Encode appended is written here.
type recordEncoder struct {
	w io.Writer
}

func newRecordEncoder(w io.Writer) recordEncoder {
	return recordEncoder{w: w}
}

func (e recordEncoder) Encode(v any) error {
	if err := jsonv2.MarshalWrite(e.w, v, marshalOptions); err != nil {
		return err
	}
	_, err := io.WriteString(e.w, "\n")
	return err
}
