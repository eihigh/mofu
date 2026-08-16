package mofufmt

import (
	"bytes"
	"testing"
)

// FuzzDecode drives arbitrary bytes through the decoder. The decoder must
// never panic, hang, or allocate past what the input's real size justifies,
// and anything it accepts must survive a re-encode/re-decode round trip.
func FuzzDecode(f *testing.F) {
	// Seed with real encodings, both compressed and raw, plus assorted
	// near-valid prefixes.
	for _, uncompressed := range []bool{false, true} {
		var buf bytes.Buffer
		if err := Encode(&buf, sample(), EncodeOptions{Uncompressed: uncompressed}); err != nil {
			f.Fatal(err)
		}
		f.Add(buf.Bytes())
		f.Add(buf.Bytes()[:buf.Len()/2])
	}
	var empty bytes.Buffer
	if err := Encode(&empty, &File{}, EncodeOptions{Uncompressed: true}); err != nil {
		f.Fatal(err)
	}
	f.Add(empty.Bytes())
	f.Add([]byte("MOFU"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, err := Decode(bytes.NewReader(data))
		if err != nil {
			return
		}
		// Whatever decodes must be encodable again...
		var out bytes.Buffer
		if err := Encode(&out, decoded, EncodeOptions{Uncompressed: true}); err != nil {
			t.Fatalf("re-encode of a decoded file failed: %v", err)
		}
		// ...and the re-encoding must decode to the same shape.
		again, err := Decode(bytes.NewReader(out.Bytes()))
		if err != nil {
			t.Fatalf("re-decode failed: %v", err)
		}
		if len(again.Meshes) != len(decoded.Meshes) ||
			len(again.Animations) != len(decoded.Animations) ||
			len(again.Textures) != len(decoded.Textures) {
			t.Fatal("re-decode changed the file's shape")
		}
	})
}
