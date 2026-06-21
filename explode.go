package main

// explodeRecords expands every record whose `field` holds an array into one
// record per element, carrying that element as a scalar in `field`. Records
// whose `field` is a scalar or absent pass through unchanged. An empty array
// yields a single record with `field` removed (a binderless row).
//
// Motivation: the fileregister index keeps one record per file with `binder`
// as an array (a set of memberships). Per-binder Markdown context blocks carry
// a single `binder`. Exploding the array before --merge-on lines the two up so
// `--merge-on id,binder` collapses each membership against its context block.
// All other fields, including the `_note_file`/`_mtime` provenance, are copied
// to every exploded row.
func explodeRecords(records []Record, field string) []Record {
	if field == "" {
		return records
	}
	out := make([]Record, 0, len(records))
	for _, r := range records {
		v, ok := r[field]
		if !ok {
			out = append(out, r)
			continue
		}
		arr, isArr := v.([]any)
		if !isArr {
			out = append(out, r)
			continue
		}
		if len(arr) == 0 {
			c := cloneRecord(r)
			delete(c, field)
			out = append(out, c)
			continue
		}
		for _, elem := range arr {
			c := cloneRecord(r)
			c[field] = elem
			out = append(out, c)
		}
	}
	return out
}

func cloneRecord(r Record) Record {
	c := make(Record, len(r))
	for k, v := range r {
		c[k] = v
	}
	return c
}
