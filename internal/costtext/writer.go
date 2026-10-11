package costtext

import (
	"bytes"
	"encoding/json"
	"io"
)

// DisplayKey is the field a token-mode JSON document gains so a reader can
// tell the dollar fields were left out on purpose, not lost.
const DisplayKey = "cost_display"

// Writer renders a CLI report under the developer cost display. In dollar
// mode it is a pass-through. In token mode it decides on the first
// non-blank byte:
//
//   - a JSON document ('{' or '['): buffered to Close, then every
//     dollar-keyed field is dropped (StripDollarFields) and a top-level object
//     gains "cost_display": "tokens". Output that turns out not to be one JSON
//     value is tried as JSON lines, and failing that masked as text.
//   - anything else: streamed line by line with MaskDollars, so a long-running
//     command still prints as it goes.
//
// Close flushes what is buffered; it must be called once the report is done.
type Writer struct {
	dst     io.Writer
	tokens  bool
	decided bool
	isJSON  bool
	buf     bytes.Buffer
}

// NewWriter wraps dst for mode.
func NewWriter(dst io.Writer, mode string) *Writer {
	return &Writer{dst: dst, tokens: Tokens(mode)}
}

// Write implements io.Writer.
func (w *Writer) Write(p []byte) (int, error) {
	if !w.tokens {
		return w.dst.Write(p)
	}
	w.buf.Write(p)
	if !w.decided {
		trimmed := bytes.TrimLeft(w.buf.Bytes(), " \t\r\n")
		if len(trimmed) == 0 {
			return len(p), nil
		}
		w.decided = true
		w.isJSON = trimmed[0] == '{' || trimmed[0] == '['
	}
	if w.isJSON {
		return len(p), nil
	}
	for {
		i := bytes.IndexByte(w.buf.Bytes(), '\n')
		if i < 0 {
			break
		}
		line := string(w.buf.Next(i + 1))
		if _, err := io.WriteString(w.dst, maskAligned(line)); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

// Close flushes the buffered remainder.
func (w *Writer) Close() error {
	if !w.tokens || w.buf.Len() == 0 {
		return nil
	}
	rest := w.buf.Bytes()
	if w.isJSON {
		if out, ok := ShapeJSON(rest); ok {
			_, err := w.dst.Write(out)
			w.buf.Reset()
			return err
		}
		if out, ok := shapeJSONLines(rest); ok {
			_, err := w.dst.Write(out)
			w.buf.Reset()
			return err
		}
	}
	_, err := io.WriteString(w.dst, maskAligned(string(rest)))
	w.buf.Reset()
	return err
}

// ShapeJSON reshapes one JSON document for token mode: dollar fields dropped,
// strings masked, and a top-level object marked with DisplayKey. The
// indentation follows the input (indented in, indented out). ok is false when
// raw is not exactly one JSON value.
func ShapeJSON(raw []byte) ([]byte, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	shaped := StripDollarFields(v)
	if m, isObj := shaped.(map[string]any); isObj {
		m[DisplayKey] = ModeTokens
	}
	var out []byte
	var err error
	if bytes.Contains(trimmed, []byte("\n")) {
		out, err = json.MarshalIndent(shaped, "", "  ")
	} else {
		out, err = json.Marshal(shaped)
	}
	if err != nil {
		return nil, false
	}
	if bytes.HasSuffix(raw, []byte("\n")) {
		out = append(out, '\n')
	}
	return out, true
}

// shapeJSONLines reshapes newline-delimited JSON one line at a time. ok is
// false when any non-blank line is not one JSON value.
func shapeJSONLines(raw []byte) ([]byte, bool) {
	var out bytes.Buffer
	for _, line := range bytes.SplitAfter(raw, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			out.Write(line)
			continue
		}
		shaped, ok := ShapeJSON(line)
		if !ok {
			return nil, false
		}
		out.Write(shaped)
	}
	return out.Bytes(), true
}
