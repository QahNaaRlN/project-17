package deliverycache

import (
	"reflect"
	"testing"
)

// The binary entry round-trips; a truncated header, another format or an old JSON entry is a miss.
func TestEntryFormat(t *testing.T) {
	e := Entry{
		Body:          []byte(`{"page":"/"}`),
		ETag:          `"abc"`,
		SurrogateKeys: []string{"routes", "obj:1"},
		Tags:          map[string]string{"routes": "v1", "obj:1": ""},
	}
	raw := encode(e)
	got, ok := decode(raw)
	if !ok || !reflect.DeepEqual(got, e) {
		t.Fatalf("round trip: %v %+v", ok, got)
	}
	header := len(raw) - len(e.Body)
	for n := range header {
		if _, ok := decode(raw[:n]); ok {
			t.Fatalf("truncated header of %d bytes decoded", n)
		}
	}
	for _, bad := range [][]byte{
		[]byte(`{"b":"e30=","e":"x"}`),
		{format, 0xff},       // ETag length does not end
		{format, 0x05, 'a'},  // ETag longer than the entry
		{format, 0x00, 0x09}, // more keys than bytes
	} {
		if _, ok := decode(bad); ok {
			t.Fatalf("decoded %q", bad)
		}
	}
	empty, ok := decode(encode(Entry{}))
	if !ok || len(empty.Body) != 0 || len(empty.SurrogateKeys) != 0 {
		t.Fatalf("empty entry: %v %+v", ok, empty)
	}
}
