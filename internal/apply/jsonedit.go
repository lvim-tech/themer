package apply

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Editing a JSON document in place, one key at a time.
//
// json-set used to read the file into a map[string]any and marshal it back.
// That set the key and silently took everything else with it: comments are not
// representable in a Go map, key order is lost, every number came back through
// float64, and the pretty-printed file somebody maintained by hand collapsed
// onto one line on the first switch. On zed — the program the dotted-key
// support was written for — it did not get that far at all: zed's settings.json
// is JSONC, encoding/json rejects the // comments its own default file ships
// with, so the operation failed every single time.
//
// So the document is edited as TEXT. The scanner below walks it only as far as
// it must to find where one key's value sits, and only those bytes are
// replaced. Everything it does not need to understand it steps over untouched,
// which is what makes comments, trailing commas, indentation, key order and
// number spelling survive by construction rather than by being reproduced.

// setJSONKey returns src with the value at the dotted path replaced by encoded,
// which must already be valid JSON.
//
// A path the document does not have yet is created: the missing part is
// inserted at the top of the deepest object that does exist, indented the way
// that object's first member is.
func setJSONKey(src []byte, path []string, encoded []byte) ([]byte, error) {
	spot, err := findJSONKey(src, path)
	if err != nil {
		return nil, err
	}

	if spot.found {
		return splice(src, spot.start, spot.end, encoded), nil
	}

	member := jsonMember(spot.rest, encoded)
	if !spot.empty {
		member = append(member, ',')
	}
	// The whitespace run after the '{' is what separates that brace from the
	// first member, so reusing it puts the new member on its own line at the
	// object's own indentation — without having to work out what that is.
	gap := leadingSpace(src[spot.insertAt:])
	text := append(append([]byte{}, gap...), member...)
	return splice(src, spot.insertAt, spot.insertAt, text), nil
}

func splice(src []byte, start, end int, with []byte) []byte {
	out := make([]byte, 0, len(src)-(end-start)+len(with))
	out = append(out, src[:start]...)
	out = append(out, with...)
	return append(out, src[end:]...)
}

// jsonMember writes `"a": {"b": encoded}` for the path parts still missing.
func jsonMember(rest []string, encoded []byte) []byte {
	var b bytes.Buffer
	for i, p := range rest {
		key, _ := json.Marshal(p)
		b.Write(key)
		b.WriteString(": ")
		if i < len(rest)-1 {
			b.WriteString("{")
		}
	}
	b.Write(encoded)
	for range rest[1:] {
		b.WriteString("}")
	}
	return b.Bytes()
}

func leadingSpace(b []byte) []byte {
	for i, ch := range b {
		if ch != ' ' && ch != '\t' && ch != '\n' && ch != '\r' {
			return b[:i]
		}
	}
	return b
}

// jsonSpot is where a dotted key came out.
type jsonSpot struct {
	// found says the whole path resolved; start and end then bracket the bytes
	// of its value.
	found      bool
	start, end int
	// When it did not, insertAt is just inside the deepest object that did
	// resolve, rest is what of the path remains to be created there, and empty
	// says that object has no members yet — so the new one needs no comma.
	insertAt int
	rest     []string
	empty    bool
}

// findJSONKey walks src looking for path.
func findJSONKey(src []byte, path []string) (jsonSpot, error) {
	c := &jsonCursor{src: src}
	c.space()
	if c.at() != '{' {
		return jsonSpot{}, fmt.Errorf("the document is not a JSON object")
	}
	return c.object(path)
}

// jsonCursor reads a JSON document, tolerating the two things JSONC adds:
// comments, and a comma after the last member.
type jsonCursor struct {
	src []byte
	i   int
}

// at is the byte under the cursor, or 0 at the end of the document — which no
// document contains, so it needs no separate test.
func (c *jsonCursor) at() byte {
	if c.i >= len(c.src) {
		return 0
	}
	return c.src[c.i]
}

// space steps over whitespace and comments alike: to everything above, a
// comment is just a longer gap between two tokens.
func (c *jsonCursor) space() {
	for c.i < len(c.src) {
		switch ch := c.src[c.i]; {
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			c.i++
		case ch == '/' && c.i+1 < len(c.src) && c.src[c.i+1] == '/':
			for c.i < len(c.src) && c.src[c.i] != '\n' {
				c.i++
			}
		case ch == '/' && c.i+1 < len(c.src) && c.src[c.i+1] == '*':
			if end := bytes.Index(c.src[c.i+2:], []byte("*/")); end >= 0 {
				c.i += 2 + end + 2
			} else {
				c.i = len(c.src) // unterminated: whatever follows is reported by the caller
			}
		default:
			return
		}
	}
}

// object walks the object at the cursor looking for path[0], descending when
// the path continues past it.
func (c *jsonCursor) object(path []string) (jsonSpot, error) {
	open := c.i
	c.i++ // the '{'

	out := jsonSpot{insertAt: open + 1, rest: path, empty: true}
	for {
		c.space()
		switch c.at() {
		case '}':
			c.i++
			return out, nil // the key is not in this object
		case ',':
			c.i++ // between members, and after the last one in JSONC
			continue
		case '"':
		default:
			return jsonSpot{}, fmt.Errorf("expected a key at byte %d, found %q", c.i, printable(c.at()))
		}
		out.empty = false

		key, err := c.str()
		if err != nil {
			return jsonSpot{}, err
		}
		c.space()
		if c.at() != ':' {
			return jsonSpot{}, fmt.Errorf("expected %q after the key %q at byte %d", ":", key, c.i)
		}
		c.i++
		c.space()

		if key != path[0] {
			if err := c.value(); err != nil {
				return jsonSpot{}, err
			}
			continue
		}
		if len(path) == 1 {
			start := c.i
			if err := c.value(); err != nil {
				return jsonSpot{}, err
			}
			return jsonSpot{found: true, start: start, end: c.i}, nil
		}
		// A scalar where the path expects an object means the document is not
		// shaped the way the definition believes; overwriting it would destroy
		// a setting.
		if c.at() != '{' {
			return jsonSpot{}, fmt.Errorf("%q is not an object", key)
		}
		return c.object(path[1:])
	}
}

// value steps over one value of any kind, leaving the cursor just past it.
func (c *jsonCursor) value() error {
	c.space()
	switch ch := c.at(); {
	case ch == '"':
		_, err := c.str()
		return err
	case ch == '{' || ch == '[':
		return c.container()
	case ch == 0:
		return fmt.Errorf("the document ends where a value was expected")
	default:
		// A number, true, false or null: it runs until something that can only
		// come after it.
		start := c.i
		for c.i < len(c.src) && !endsValue(c.src[c.i]) {
			c.i++
		}
		if c.i == start {
			return fmt.Errorf("expected a value at byte %d, found %q", c.i, printable(ch))
		}
		return nil
	}
}

// container steps over a balanced object or array without caring what is in it.
func (c *jsonCursor) container() error {
	open := c.at()
	shut := byte('}')
	if open == '[' {
		shut = ']'
	}
	c.i++
	for {
		c.space()
		switch ch := c.at(); {
		case ch == shut:
			c.i++
			return nil
		case ch == 0:
			return fmt.Errorf("the document ends inside a %q", printable(open))
		case ch == '"':
			if _, err := c.str(); err != nil {
				return err
			}
		case ch == '{' || ch == '[':
			if err := c.container(); err != nil {
				return err
			}
		case ch == ',' || ch == ':':
			c.i++
		default:
			start := c.i
			for c.i < len(c.src) && !endsValue(c.src[c.i]) {
				c.i++
			}
			if c.i == start {
				return fmt.Errorf("unexpected %q at byte %d", printable(ch), c.i)
			}
		}
	}
}

// str reads the string at the cursor and returns what it means, so a key
// written with escapes is compared by its value rather than by its spelling.
func (c *jsonCursor) str() (string, error) {
	start := c.i
	c.i++ // the opening quote
	for c.i < len(c.src) {
		switch c.src[c.i] {
		case '\\':
			c.i += 2
		case '"':
			c.i++
			var s string
			if err := json.Unmarshal(c.src[start:c.i], &s); err != nil {
				return "", fmt.Errorf("the string at byte %d: %w", start, err)
			}
			return s, nil
		default:
			c.i++
		}
	}
	return "", fmt.Errorf("the string opened at byte %d is never closed", start)
}

func endsValue(ch byte) bool {
	switch ch {
	case ',', '}', ']', ' ', '\t', '\n', '\r', '/':
		return true
	}
	return false
}

// printable keeps an error message readable when the byte it names is not.
func printable(ch byte) string {
	if ch == 0 {
		return "end of document"
	}
	return string(ch)
}
