// Package toon implements an encoder for TOON (Token-Oriented Object
// Notation, github.com/toon-format/toon) — a compact, LLM-friendly
// serialization covering spec sections 1–4:
//
//   - objects              → `key: value` lines, 2-space indent, json tags honored
//   - uniform struct slices → tabular `key[N]{f1,f2,…}:` + CSV rows
//   - primitive slices      → `key[N]: v1,v2,…`
//   - non-uniform slices    → `- ` item lines
//
// Strings are emitted plain when safe and JSON-style quoted+escaped when they
// contain a newline, quote, comma, colon or braces. The output is fully
// deterministic: struct field order follows declaration order and map keys are
// sorted. There is intentionally no decoder — providers answer JSON.
package toon

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Marshal encodes v as TOON. Top-level structs emit one `key: value` line per
// exported field; other top-level values encode inline.
func Marshal(v any) (string, error) {
	var b strings.Builder
	rv := deref(reflect.ValueOf(v))
	if !rv.IsValid() {
		return "", nil
	}
	if rv.Kind() == reflect.Struct {
		if err := encodeObject(&b, rv, 0); err != nil {
			return "", err
		}
		return b.String(), nil
	}
	s, err := encodeInline(rv)
	if err != nil {
		return "", err
	}
	b.WriteString(s)
	b.WriteString("\n")
	return b.String(), nil
}

func pad(indent int) string {
	return strings.Repeat("  ", indent)
}

func deref(v reflect.Value) reflect.Value {
	for v.IsValid() && (v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

// encodeObject writes every exported field of rv at the given indent level.
func encodeObject(b *strings.Builder, rv reflect.Value, indent int) error {
	t := rv.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue // unexported
		}
		if jsonTagOmitted(field) {
			continue
		}
		if jsonOmitEmpty(field) && isEmptyValue(rv.Field(i)) {
			continue // mirrors encoding/json: drop empty tagged fields
		}
		if err := encodeField(b, jsonFieldName(field), rv.Field(i), indent); err != nil {
			return err
		}
	}
	return nil
}

func jsonTagOmitted(f reflect.StructField) bool {
	tag, ok := f.Tag.Lookup("json")
	if !ok {
		return false
	}
	name := strings.Split(tag, ",")[0]
	return name == "-"
}

func jsonFieldName(f reflect.StructField) string {
	tag, ok := f.Tag.Lookup("json")
	if !ok {
		return f.Name
	}
	name := strings.Split(tag, ",")[0]
	if name == "" {
		return f.Name
	}
	return name
}

// jsonOmitEmpty reports whether the field carries ",omitempty".
func jsonOmitEmpty(f reflect.StructField) bool {
	tag, ok := f.Tag.Lookup("json")
	if !ok {
		return false
	}
	for _, part := range strings.Split(tag, ",")[1:] {
		if part == "omitempty" {
			return true
		}
	}
	return false
}

// isEmptyValue mirrors encoding/json's omitempty notion of emptiness for the
// value kinds TOON emits (empty strings/slices/maps, nil pointers, false).
func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return v.Len() == 0
	case reflect.Slice, reflect.Map:
		return v.Len() == 0
	case reflect.Ptr, reflect.Interface:
		return v.IsNil()
	case reflect.Bool:
		return !v.Bool()
	default:
		return false
	}
}

// encodeField writes one `name: value` (or nested) line for a struct field.
func encodeField(b *strings.Builder, name string, val reflect.Value, indent int) error {
	header := pad(indent) + name
	rv := deref(val)
	if !rv.IsValid() {
		b.WriteString(header + ":\n")
		return nil
	}

	switch rv.Kind() {
	case reflect.Struct:
		b.WriteString(header + ":\n")
		return encodeObject(b, rv, indent+1)
	case reflect.Map:
		b.WriteString(header + ":\n")
		return encodeMap(b, rv, indent+1)
	case reflect.Slice, reflect.Array:
		return encodeSliceField(b, header, rv, indent)
	default:
		s, err := encodeInline(rv)
		if err != nil {
			return err
		}
		b.WriteString(header + ":")
		if s != "" {
			b.WriteString(" " + s)
		}
		b.WriteString("\n")
		return nil
	}
}

// encodeSliceField picks the compact representation for a slice-valued field.
func encodeSliceField(b *strings.Builder, header string, rv reflect.Value, indent int) error {
	length := rv.Len()
	// Deref pointer elements so *[N]T behaves like []T.
	if length > 0 && rv.Index(0).Kind() == reflect.Ptr {
		elems := make([]reflect.Value, 0, length)
		for i := 0; i < length; i++ {
			elems = append(elems, rv.Index(i))
		}
		return encodeElems(b, header, elems, indent)
	}

	if length == 0 {
		b.WriteString(header + "[0]:\n")
		return nil
	}

	if isScalarType(rv.Index(0).Type()) {
		b.WriteString(header + "[" + strconv.Itoa(length) + "]: ")
		vals := make([]string, 0, length)
		for i := 0; i < length; i++ {
			s, err := encodeInline(rv.Index(i))
			if err != nil {
				return err
			}
			vals = append(vals, s)
		}
		b.WriteString(strings.Join(vals, ",") + "\n")
		return nil
	}

	if elemType := rv.Index(0).Type(); elemType.Kind() == reflect.Struct {
		if fields := tabularFieldsFor(rv); len(fields) > 0 {
			return encodeTabular(b, header, rv, fields, indent)
		}
	}

	return encodeListFallback(b, header, rv, indent)
}

// encodeElems handles slices of pointers after they were dereferenced.
func encodeElems(b *strings.Builder, header string, elems []reflect.Value, indent int) error {
	if len(elems) == 0 {
		b.WriteString(header + "[0]:\n")
		return nil
	}
	if isScalarType(elems[0].Type()) {
		b.WriteString(header + "[" + strconv.Itoa(len(elems)) + "]: ")
		vals := make([]string, 0, len(elems))
		for _, e := range elems {
			s, err := encodeInline(deref(e))
			if err != nil {
				return err
			}
			vals = append(vals, s)
		}
		b.WriteString(strings.Join(vals, ",") + "\n")
		return nil
	}
	for _, e := range elems {
		if err := encodeListItem(b, header, e, indent); err != nil {
			return err
		}
	}
	return nil
}

// encodeTabular writes the `key[N]{f1,f2,…}:` form followed by one CSV row
// per element. This is where TOON saves the most tokens.
func encodeTabular(b *strings.Builder, header string, rv reflect.Value, fields []int, indent int) error {
	elemType := rv.Type().Elem()
	b.WriteString(header + "[" + strconv.Itoa(rv.Len()) + "]{")
	names := make([]string, 0, len(fields))
	for _, idx := range fields {
		names = append(names, jsonFieldName(elemType.Field(idx)))
	}
	b.WriteString(strings.Join(names, ",") + "}:\n")

	rowPad := pad(indent + 1)
	for i := 0; i < rv.Len(); i++ {
		elem := rv.Index(i)
		cells := make([]string, 0, len(fields))
		for _, idx := range fields {
			fv := deref(elem.Field(idx))
			if fv.IsValid() && (fv.Kind() == reflect.Slice || fv.Kind() == reflect.Array) {
				// Omitempty slice columns verified empty by tabularFieldsFor.
				cells = append(cells, "")
				continue
			}
			s, err := encodeInline(fv)
			if err != nil {
				return err
			}
			cells = append(cells, s)
		}
		b.WriteString(rowPad + strings.Join(cells, ",") + "\n")
	}
	return nil
}

// encodeListFallback writes the non-uniform `- ` item form.
func encodeListFallback(b *strings.Builder, header string, rv reflect.Value, indent int) error {
	b.WriteString(header + "[" + strconv.Itoa(rv.Len()) + "]:\n")
	for i := 0; i < rv.Len(); i++ {
		if err := encodeListItem(b, header, rv.Index(i), indent); err != nil {
			return err
		}
	}
	return nil
}

// encodeListItem writes one `- ` item; struct items put their first field on
// the dash line and the rest under it.
func encodeListItem(b *strings.Builder, header string, elem reflect.Value, indent int) error {
	itemPad := pad(indent + 1)
	e := deref(elem)
	if !e.IsValid() {
		b.WriteString(itemPad + "-\n")
		return nil
	}
	if e.Kind() != reflect.Struct {
		s, err := encodeInline(e)
		if err != nil {
			return err
		}
		b.WriteString(itemPad + "- " + s + "\n")
		return nil
	}

	t := e.Type()
	first := true
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" || jsonTagOmitted(field) {
			continue
		}
		if jsonOmitEmpty(field) && isEmptyValue(e.Field(i)) {
			continue
		}
		name := jsonFieldName(field)
		fv := deref(e.Field(i))
		if !fv.IsValid() {
			if first {
				b.WriteString(itemPad + "- " + name + ":\n")
			} else {
				b.WriteString(itemPad + "  " + name + ":\n")
			}
			first = false
			continue
		}
		if fv.Kind() == reflect.Struct || fv.Kind() == reflect.Slice ||
			fv.Kind() == reflect.Array || fv.Kind() == reflect.Map {
			if first {
				b.WriteString(itemPad + "- " + name + ":\n")
				if err := encodeNested(b, fv, indent+2); err != nil {
					return err
				}
			} else {
				b.WriteString(itemPad + "  " + name + ":\n")
				if err := encodeNested(b, fv, indent+2); err != nil {
					return err
				}
			}
			first = false
			continue
		}
		s, err := encodeInline(fv)
		if err != nil {
			return err
		}
		if first {
			b.WriteString(itemPad + "- " + name + ": " + s + "\n")
		} else {
			b.WriteString(itemPad + "  " + name + ": " + s + "\n")
		}
		first = false
	}
	return nil
}

// encodeNested writes a nested object/slice/map value under an already
// emitted header line.
func encodeNested(b *strings.Builder, rv reflect.Value, indent int) error {
	switch rv.Kind() {
	case reflect.Struct:
		return encodeObject(b, rv, indent)
	case reflect.Map:
		return encodeMap(b, rv, indent)
	case reflect.Slice, reflect.Array:
		if rv.Len() == 0 {
			b.WriteString(pad(indent) + "[0]:\n")
			return nil
		}
		header := ""
		if isScalarType(rv.Index(0).Type()) {
			vals := make([]string, 0, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				s, err := encodeInline(rv.Index(i))
				if err != nil {
					return err
				}
				vals = append(vals, s)
			}
			b.WriteString(pad(indent) + "[" + strconv.Itoa(rv.Len()) + "]: " + strings.Join(vals, ",") + "\n")
			return nil
		}
		return encodeListFallback(b, header, rv, indent)
	default:
		s, err := encodeInline(rv)
		if err != nil {
			return err
		}
		b.WriteString(pad(indent) + s + "\n")
		return nil
	}
}

// encodeMap writes a map as a nested object with deterministically sorted keys.
func encodeMap(b *strings.Builder, rv reflect.Value, indent int) error {
	keys := make([]string, 0, rv.Len())
	byKey := make(map[string]reflect.Value, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		k := fmt.Sprintf("%v", iter.Key().Interface())
		keys = append(keys, k)
		byKey[k] = iter.Value()
	}
	sort.Strings(keys)

	for _, k := range keys {
		if err := encodeField(b, escapeString(k), byKey[k], indent); err != nil {
			return err
		}
	}
	return nil
}

// encodeInline renders a scalar (string, number, bool) on one line.
func encodeInline(v reflect.Value) (string, error) {
	if !v.IsValid() {
		return "", nil
	}
	switch v.Kind() {
	case reflect.String:
		return escapeString(v.String()), nil
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10), nil
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64), nil
	default:
		return "", fmt.Errorf("toon: cannot inline value of kind %s", v.Kind())
	}
}

// isScalarType reports whether t encodes inline (string/number/bool).
func isScalarType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

// tabularFieldsFor returns the field indices usable as fixed tabular columns
// for THIS slice value, or nil when the tabular form does not apply.
//
// A column qualifies when it is scalar-encodable, or when it is an omitempty
// slice-of-scalars whose value is empty in EVERY row (e.g. CodeElement.Calls);
// such columns are emitted as empty cells so the row shape stays uniform.
func tabularFieldsFor(rv reflect.Value) []int {
	if rv.Len() == 0 {
		return nil
	}
	t := rv.Type().Elem()
	if t.Kind() != reflect.Struct {
		return nil
	}

	idx := make([]int, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" || jsonTagOmitted(f) {
			continue
		}
		ft := f.Type
		for ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		if isScalarType(ft) {
			idx = append(idx, i)
			continue
		}
		// Tolerate omitempty slice-of-scalar columns when empty everywhere.
		if jsonOmitEmpty(f) && ft.Kind() == reflect.Slice && isScalarType(ft.Elem()) {
			for row := 0; row < rv.Len(); row++ {
				if rv.Index(row).Field(i).Len() > 0 {
					return nil // a row carries calls: tabular is not safe
				}
			}
			idx = append(idx, i)
			continue
		}
		return nil
	}
	if len(idx) == 0 {
		return nil
	}
	return idx
}

// escapeString emits plain text when safe, JSON-quoted text when the value
// contains a newline, quote, comma, colon, brace, carriage return or has
// leading/trailing whitespace.
func escapeString(s string) string {
	if !needsQuoting(s) {
		return s
	}
	data, err := json.Marshal(s)
	if err != nil {
		return strconv.Quote(s)
	}
	return string(data)
}

func needsQuoting(s string) bool {
	if s == "" {
		return false
	}
	if strings.TrimSpace(s) != s {
		return true
	}
	return strings.ContainsAny(s, "\n\r\"{},:")
}
