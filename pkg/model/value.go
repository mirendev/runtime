package model

import (
	"encoding/base64"
	"fmt"
	"time"
	"unicode/utf8"

	"miren.dev/runtime/pkg/entity"
)

// ellipsis marks an elided value. Paired with Field.Size so the reader always
// knows how much was left out.
const ellipsis = "…"

// renderBytes is the one place that decides how a byte value displays. Both the
// schema-aware path and the raw attribute path go through it, so a field cannot
// render one way under its facet and another way as a raw attribute.
//
// yaml.v3 has no []byte case on encode and falls through to generic slice
// handling, which is what turned a saga listing into millions of one-decimal
// lines. Base64 keeps it to one line and matches how the schema path has always
// rendered bytes.
func (o Options) renderBytes(b []byte) (string, bool, int) {
	enc := base64.StdEncoding.EncodeToString(b)

	if o.MaxValueLen > 0 && len(enc) > o.MaxValueLen {
		// base64 is ASCII, so a byte cut is already a rune cut here.
		return enc[:o.MaxValueLen] + ellipsis, true, len(b)
	}

	return enc, false, len(b)
}

// renderString elides over-long strings on the same terms as bytes. A multi-
// megabyte string attribute wrecks a listing just as thoroughly as a blob.
func (o Options) renderString(s string) (string, bool, int) {
	if o.MaxValueLen > 0 && len(s) > o.MaxValueLen {
		// Back off to a rune boundary. Cutting mid-rune leaves invalid UTF-8,
		// which json.Marshal turns into U+FFFD, so the reader sees a mojibake
		// character sitting in front of the ellipsis.
		cut := s[:o.MaxValueLen]
		for len(cut) > 0 && !utf8.ValidString(cut) {
			cut = cut[:len(cut)-1]
		}

		return cut + ellipsis, true, len(s)
	}

	return s, false, len(s)
}

// renderValue converts a raw attribute value into its natural JSON form.
//
// It switches on the value's own kind and only calls the matching accessor. The
// entity.Value accessors panic on a mismatch, and a debug listing is exactly
// where you meet malformed data, so this must never hand one the wrong kind.
func (o Options) renderValue(v entity.Value) (any, bool, int) {
	switch v.Kind() {
	case entity.KindString:
		return o.renderString(v.String())

	case entity.KindBytes:
		return o.renderBytes(v.Bytes())

	case entity.KindInt64:
		return v.Int64(), false, 0

	case entity.KindUint64:
		return v.Uint64(), false, 0

	case entity.KindFloat64:
		return v.Float64(), false, 0

	case entity.KindBool:
		return v.Bool(), false, 0

	case entity.KindTime:
		return v.Time().Format(time.RFC3339Nano), false, 0

	case entity.KindDuration:
		return v.Duration().String(), false, 0

	case entity.KindId:
		return string(v.Id()), false, 0

	case entity.KindKeyword:
		return string(v.Keyword()), false, 0

	case entity.KindLabel:
		lbl := v.Label()
		return fmt.Sprintf("%s=%s", lbl.Key, lbl.Value), false, 0

	case entity.KindArray:
		var (
			out       []any
			truncated bool
		)

		for _, elem := range v.Array() {
			ev, et, _ := o.renderValue(elem)
			truncated = truncated || et
			out = append(out, ev)
		}

		return out, truncated, 0

	case entity.KindComponent:
		comp := v.Component()
		if comp == nil {
			return nil, false, 0
		}

		return o.renderAttrsMap(comp.Attrs())

	case entity.KindAny:
		return fmt.Sprintf("%v", v.Any()), false, 0

	default:
		// A value kind added after this switch was written. Print it rather
		// than dropping it; an unrecognised value is still evidence.
		return fmt.Sprintf("%v", v.Any()), false, 0
	}
}

// renderAttrsMap renders a component's attributes without schema help, keyed by
// attribute id. Components carry their own nested attributes and the enclosing
// schema may not describe them.
func (o Options) renderAttrsMap(attrs []entity.Attr) (map[string]any, bool, int) {
	m := make(map[string]any, len(attrs))

	var (
		truncated bool
		largest   int
	)

	for _, a := range attrs {
		val, t, size := o.renderValue(a.Value)
		if t {
			truncated = true
			if size > largest {
				largest = size
			}
		}

		// Attributes are a multimap, so a repeated id collects into a slice
		// rather than overwriting.
		if prev, ok := m[string(a.ID)]; ok {
			if list, ok := prev.([]any); ok {
				m[string(a.ID)] = append(list, val)
				continue
			}

			m[string(a.ID)] = []any{prev, val}
			continue
		}

		m[string(a.ID)] = val
	}

	return m, truncated, largest
}
