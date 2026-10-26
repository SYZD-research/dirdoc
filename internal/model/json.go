package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// Marshal encodes v as compact JSON without HTML escaping (documents are full of <, > and &).
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// VersionError reports a stored document written for a schema version this dirdoc does not know.
type VersionError struct{ Version int }

func (e *VersionError) Error() string {
	return fmt.Sprintf("unsupported schema version %d (this dirdoc supports version %d); update dirdoc", e.Version, Version)
}

// Decode parses a stored document and returns either a DSD or a PFD.
func Decode(js []byte) (Kind, *DSD, *PFD, error) {
	var head struct {
		Format  string `json:"format"`
		Version *int   `json:"version"`
	}
	if err := json.Unmarshal(js, &head); err != nil {
		return 0, nil, nil, err
	}
	var kind Kind
	switch head.Format {
	case FormatDSD:
		kind = KindDSD
	case FormatPFD:
		kind = KindPFD
	default:
		return 0, nil, nil, fmt.Errorf("unknown format %q", head.Format)
	}
	if head.Version == nil {
		return 0, nil, nil, errors.New("missing version")
	}
	if *head.Version != Version {
		return 0, nil, nil, &VersionError{Version: *head.Version}
	}
	if kind == KindDSD {
		d := new(DSD)
		if err := json.Unmarshal(js, d); err != nil {
			return 0, nil, nil, err
		}
		return kind, d, nil, nil
	}
	p := new(PFD)
	if err := json.Unmarshal(js, p); err != nil {
		return 0, nil, nil, err
	}
	return kind, nil, p, nil
}

// object writes a JSON object with keys in the order they are added.
type object struct {
	buf bytes.Buffer
	err error
}

func (o *object) field(key string, v any) {
	if o.err != nil {
		return
	}
	b, err := Marshal(v)
	if err != nil {
		o.err = fmt.Errorf("%s: %w", key, err)
		return
	}
	o.raw(key, b)
}

func (o *object) raw(key string, b []byte) {
	if o.buf.Len() == 0 {
		o.buf.WriteByte('{')
	} else {
		o.buf.WriteByte(',')
	}
	k, _ := Marshal(key)
	o.buf.Write(k)
	o.buf.WriteByte(':')
	o.buf.Write(b)
}

func (o *object) extra(e Extra) {
	keys := make([]string, 0, len(e))
	for k := range e {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		o.raw(k, e[k])
	}
}

func (o *object) bytes() ([]byte, error) {
	if o.err != nil {
		return nil, o.err
	}
	if o.buf.Len() == 0 {
		return []byte("{}"), nil
	}
	o.buf.WriteByte('}')
	return o.buf.Bytes(), nil
}

// fields is a decoded JSON object whose known keys are taken out one by one; the rest becomes Extra.
type fields map[string]json.RawMessage

func readObject(b []byte) (fields, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.New("expected an object, got null")
	}
	return m, nil
}

func (f fields) take(key string, dst any) error {
	raw, ok := f[key]
	if !ok {
		return nil
	}
	delete(f, key)
	if string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	return nil
}

func (f fields) drop(keys ...string) {
	for _, k := range keys {
		delete(f, k)
	}
}

func (f fields) rest() Extra {
	if len(f) == 0 {
		return nil
	}
	return Extra(f)
}

func (n TreeNode) MarshalJSON() ([]byte, error) {
	var o object
	o.field("name", n.Name)
	o.field("type", n.Type)
	if n.DSD != "" {
		o.field("dsd", n.DSD)
	}
	if n.Note != "" {
		o.field("note", n.Note)
	}
	if n.More {
		o.field("more", true)
	}
	if len(n.Children) > 0 {
		o.field("children", n.Children)
	}
	o.extra(n.Extra)
	return o.bytes()
}

func (n *TreeNode) UnmarshalJSON(b []byte) error {
	f, err := readObject(b)
	if err != nil {
		return err
	}
	*n = TreeNode{}
	err = errors.Join(
		f.take("name", &n.Name),
		f.take("type", &n.Type),
		f.take("dsd", &n.DSD),
		f.take("note", &n.Note),
		f.take("more", &n.More),
		f.take("children", &n.Children),
	)
	for _, c := range n.Children {
		if c == nil {
			err = errors.Join(err, errors.New("children: null node"))
			break
		}
	}
	n.Extra = f.rest()
	return err
}

func (e DSDEntry) MarshalJSON() ([]byte, error) {
	var o object
	o.field("id", e.ID)
	if e.Name != "" {
		o.field("name", e.Name)
	}
	if e.Document != "" {
		o.field("document", e.Document)
	}
	o.extra(e.Extra)
	return o.bytes()
}

func (e *DSDEntry) UnmarshalJSON(b []byte) error {
	f, err := readObject(b)
	if err != nil {
		return err
	}
	*e = DSDEntry{}
	err = errors.Join(f.take("id", &e.ID), f.take("name", &e.Name), f.take("document", &e.Document))
	e.Extra = f.rest()
	return err
}

func (d DSD) MarshalJSON() ([]byte, error) {
	var o object
	o.field("format", FormatDSD)
	o.field("version", Version)
	if d.Summary != "" {
		o.field("summary", d.Summary)
	}
	if d.Tree != nil {
		o.field("tree", d.Tree)
	}
	if len(d.Entries) > 0 {
		o.field("dsd", d.Entries)
	}
	o.extra(d.Extra)
	return o.bytes()
}

func (d *DSD) UnmarshalJSON(b []byte) error {
	f, err := readObject(b)
	if err != nil {
		return err
	}
	*d = DSD{}
	f.drop("format", "version")
	err = errors.Join(f.take("summary", &d.Summary), f.take("tree", &d.Tree), f.take("dsd", &d.Entries))
	d.Extra = f.rest()
	return err
}

func (e Entry) MarshalJSON() ([]byte, error) {
	var o object
	o.field("id", e.ID)
	o.field("name", e.Name)
	o.field("summary", e.Summary)
	if e.Document != "" {
		o.field("document", e.Document)
	}
	o.extra(e.Extra)
	return o.bytes()
}

func (e *Entry) UnmarshalJSON(b []byte) error {
	f, err := readObject(b)
	if err != nil {
		return err
	}
	*e = Entry{}
	err = errors.Join(
		f.take("id", &e.ID),
		f.take("name", &e.Name),
		f.take("summary", &e.Summary),
		f.take("document", &e.Document),
	)
	e.Extra = f.rest()
	return err
}

func nonNil(items []OverviewItem) []OverviewItem {
	if items == nil {
		return []OverviewItem{}
	}
	return items
}

// MarshalJSON writes undocumented only when the file was analyzed, so that [] (analyzed, all
// documented) stays apart from nil (not analyzed).
func (o Overview) MarshalJSON() ([]byte, error) {
	var w object
	w.field("types", nonNil(o.Types))
	w.field("functions", nonNil(o.Functions))
	w.field("variables", nonNil(o.Variables))
	if o.Undocumented != nil {
		w.field("undocumented", o.Undocumented)
	}
	return w.bytes()
}

func (o *Overview) UnmarshalJSON(b []byte) error {
	f, err := readObject(b)
	if err != nil {
		return err
	}
	*o = Overview{}
	return errors.Join(
		f.take("types", &o.Types),
		f.take("functions", &o.Functions),
		f.take("variables", &o.Variables),
		f.take("undocumented", &o.Undocumented),
	)
}

func (p PFD) MarshalJSON() ([]byte, error) { return p.marshal(false) }

// MarshalSource encodes p as a JSON source: the stored structure without the fields compile generates.
func (p PFD) MarshalSource() ([]byte, error) { return p.marshal(true) }

func (p PFD) marshal(source bool) ([]byte, error) {
	var o object
	o.field("format", FormatPFD)
	o.field("version", Version)
	o.field("target", p.Target)
	if p.TargetHash != "" && !source {
		o.field("target_hash", p.TargetHash)
	}
	if p.Summary != "" {
		o.field("summary", p.Summary)
	}
	if !source {
		o.field("overview", p.Overview)
	}
	for _, sec := range PFDSections {
		if entries := p.Section(sec); len(entries) > 0 {
			o.field(sec, entries)
		}
	}
	o.extra(p.Extra)
	return o.bytes()
}

func (p *PFD) UnmarshalJSON(b []byte) error {
	f, err := readObject(b)
	if err != nil {
		return err
	}
	*p = PFD{}
	f.drop("format", "version")
	err = errors.Join(
		f.take("target", &p.Target),
		f.take("target_hash", &p.TargetHash),
		f.take("summary", &p.Summary),
		f.take("overview", &p.Overview),
		f.take("types", &p.Types),
		f.take("functions", &p.Functions),
		f.take("variables", &p.Variables),
	)
	p.Extra = f.rest()
	return err
}

// CloneDSD returns a deep copy of d.
func CloneDSD(d *DSD) (*DSD, error) {
	b, err := Marshal(d)
	if err != nil {
		return nil, err
	}
	out := new(DSD)
	return out, json.Unmarshal(b, out)
}

// ClonePFD returns a deep copy of p.
func ClonePFD(p *PFD) (*PFD, error) {
	b, err := Marshal(p)
	if err != nil {
		return nil, err
	}
	out := new(PFD)
	return out, json.Unmarshal(b, out)
}
