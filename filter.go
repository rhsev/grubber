package main

import (
	"fmt"
	"regexp"
	"strings"
)

// "!=" is accepted as an alias for "!"; m[2][0] yields '!' for both.
var filterRe = regexp.MustCompile(`^([^=~^!]+)(!=|[=~^!])(.*)$`)

type condition struct {
	field string
	op    byte
	value string
}

type Filter struct {
	conditions []condition
}

func NewFilter(filters []string) (*Filter, error) {
	conds := make([]condition, 0, len(filters))
	for _, f := range filters {
		c, err := parseCondition(f)
		if err != nil {
			return nil, err
		}
		conds = append(conds, c)
	}
	return &Filter{conditions: conds}, nil
}

func parseCondition(s string) (condition, error) {
	m := filterRe.FindStringSubmatch(s)
	if m == nil {
		return condition{}, fmt.Errorf("invalid filter syntax: %s", s)
	}
	return condition{
		field: strings.TrimSpace(m[1]),
		op:    m[2][0],
		value: strings.ToLower(strings.TrimSpace(m[3])),
	}, nil
}

func (f *Filter) Match(r Record) bool {
	for _, c := range f.conditions {
		if !matchCondition(r, c) {
			return false
		}
	}
	return true
}

func matchCondition(r Record, c condition) bool {
	v, ok := r[c.field]
	if !ok || v == nil {
		return c.op == '!'
	}

	values := fieldValues(v)
	lower := make([]string, len(values))
	for i, s := range values {
		lower[i] = strings.ToLower(s)
	}

	switch c.op {
	case '=':
		for _, s := range lower {
			if s == c.value {
				return true
			}
		}
	case '~':
		for _, s := range lower {
			if strings.Contains(s, c.value) {
				return true
			}
		}
	case '^':
		for _, s := range lower {
			if strings.HasPrefix(s, c.value) {
				return true
			}
		}
	case '!':
		for _, s := range lower {
			if s == c.value {
				return false
			}
		}
		return true
	}
	return false
}

func fieldValues(v any) []string {
	if arr, ok := v.([]any); ok {
		result := make([]string, len(arr))
		for i, item := range arr {
			result[i] = fmt.Sprint(item)
		}
		return result
	}
	return []string{fmt.Sprint(v)}
}
