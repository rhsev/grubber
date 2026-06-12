package main

import (
	"fmt"
	"strings"
)

// mergeRecords merges JSONL-source records into scanned records that share
// the same identity on the given key fields (--merge-on).
//
// Motivation: a collection index (--from-jsonl) and its annotation files
// describe the same logical record in two layers. The union alone would show
// both; merging collapses them:
//
//   - Scanned records always pass through (they carry the richer fields and
//     the file the preview should open).
//   - A JSONL record whose key matches a scanned record is dropped after
//     back-filling any fields the scanned record lacks (nil, missing, or "").
//     Underscore fields (_note_file, _mtime) are never back-filled.
//   - JSONL records with no scanned counterpart pass through unchanged.
//   - Records lacking a value for the *first* key field are never merge
//     candidates and pass through unchanged.
func mergeRecords(scanned, jsonl []Record, keys []string) []Record {
	byKey := make(map[string][]Record)
	for _, rec := range scanned {
		if key, ok := mergeKey(rec, keys); ok {
			byKey[key] = append(byKey[key], rec)
		}
	}

	out := make([]Record, 0, len(scanned)+len(jsonl))
	out = append(out, scanned...)

	for _, jrec := range jsonl {
		key, ok := mergeKey(jrec, keys)
		if !ok {
			out = append(out, jrec)
			continue
		}
		targets := byKey[key]
		if len(targets) == 0 {
			out = append(out, jrec)
			continue
		}
		for _, target := range targets {
			backfill(target, jrec)
		}
	}
	return out
}

// mergeKey builds the identity key for a record. The first key field is the
// primary identity and must be present and non-empty; later fields default to
// "" when absent (e.g. a record that is in no binder).
func mergeKey(r Record, keys []string) (string, bool) {
	primary := r[keys[0]]
	if primary == nil || primary == "" {
		return "", false
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		if v := r[k]; v != nil {
			parts[i] = fmt.Sprint(v)
		}
	}
	return strings.Join(parts, "\x00"), true
}

// backfill copies fields from src into dst where dst has no value
// (missing, nil, or ""). Underscore fields stay untouched: the scanned
// record's _note_file/_mtime are its identity.
func backfill(dst, src Record) {
	for k, v := range src {
		if strings.HasPrefix(k, "_") || v == nil || v == "" {
			continue
		}
		if cur, ok := dst[k]; !ok || cur == nil || cur == "" {
			dst[k] = v
		}
	}
}
