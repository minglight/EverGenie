package vault

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func codecSpec() Spec {
	return Spec{Kind: "test", Fields: []Field{{Name: "kind"}, {Name: "id"}, {Name: "title"}, {Name: "link"}, {Name: "tags"}, {Name: "extra"}}}
}
func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Guards: REQ-1, REQ-4
func TestRoundTripBytesIdentical(t *testing.T) {
	files, err := filepath.Glob("testdata/codec/canonical/*.md")
	if err != nil || len(files) < 3 {
		t.Fatal("missing canonical fixtures")
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			input := readFixture(t, path)
			r, order, err := Decode(input)
			if err != nil {
				t.Fatal(err)
			}
			got := Encode(r, order, codecSpec())
			if !bytes.Equal(got, input) {
				t.Fatalf("canonical bytes changed:\n%s", got)
			}
			if r.Revision != fmt.Sprintf("%x", sha256.Sum256(input)) {
				t.Fatal("revision is not original file hash")
			}
		})
	}
}

// Guards: REQ-1, REQ-4
func TestRoundTripNormalizes(t *testing.T) {
	files, err := filepath.Glob("testdata/codec/noncanonical/*.md")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, path := range files {
		if strings.HasSuffix(path, ".want.md") {
			continue
		}
		count++
		t.Run(filepath.Base(path), func(t *testing.T) {
			input := readFixture(t, path)
			if filepath.Base(path) == "crlf.md" && !bytes.Contains(input, []byte("\r\n")) {
				t.Fatal("CRLF fixture lost its original line endings")
			}
			want := readFixture(t, strings.TrimSuffix(path, ".md")+".want.md")
			r, order, err := Decode(input)
			if err != nil {
				t.Fatal(err)
			}
			got := Encode(r, order, codecSpec())
			if !bytes.Equal(got, want) {
				t.Fatalf("wrong canonical output:\n%s", got)
			}
			normalized, norder, err := Decode(want)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(r.Fields, normalized.Fields) || !reflect.DeepEqual(r.Body, normalized.Body) {
				t.Fatal("normalization changed meaning")
			}
			if !bytes.Equal(Encode(normalized, norder, codecSpec()), want) {
				t.Fatal("normalization not idempotent")
			}
		})
	}
	if count < 5 {
		t.Fatal("missing normalization cases")
	}
}

// Guards: REQ-1, REQ-4
func TestUnknownFieldsKept(t *testing.T) {
	input := []byte("---\nunknown_z: [one, two]\ntitle: hello\nunknown_a:\n  value: true\n---\n\nbody\n")
	r, order, err := Decode(input)
	if err != nil {
		t.Fatal(err)
	}
	encoded := Encode(r, order, codecSpec())
	again, aorder, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Fields, again.Fields) || r.Body != again.Body {
		t.Fatal("unknown field lost")
	}
	if !reflect.DeepEqual(aorder, []string{"title", "unknown_z", "unknown_a"}) {
		t.Fatalf("unknown order changed: %v", aorder)
	}
}

// Guards: REQ-1, REQ-4
func TestLinkQuoted(t *testing.T) {
	data := Encode(Record{Fields: map[string]any{"link": "[[SEC-1]]", "tags": []string{"[[G-1]]"}}}, nil, codecSpec())
	if !bytes.Contains(data, []byte(`link: "[[SEC-1]]"`)) || !bytes.Contains(data, []byte(`- "[[G-1]]"`)) {
		t.Fatalf("links not quoted: %s", data)
	}
}

// Guards: REQ-1, REQ-4
func TestDecodeLegacyToml(t *testing.T) {
	input := []byte("+++\ntitle='old'\ntags=['one','two']\n+++\n\nold body\n")
	r, order, err := DecodeLegacy(input)
	if err != nil {
		t.Fatal(err)
	}
	if r.Fields["title"] != "old" || r.Body != "\nold body" || !reflect.DeepEqual(order, []string{"title", "tags"}) {
		t.Fatal("legacy content lost")
	}
	again, _, err := Decode(Encode(r, order, codecSpec()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Fields, again.Fields) || r.Body != again.Body {
		t.Fatal("legacy conversion changed meaning")
	}
}

// Guards: REQ-1, REQ-4
func TestDecodeRejectsMalformed(t *testing.T) {
	for _, input := range []string{"no header", "---\na: 1\n", "---\na: 1\na: 2\n---\n", "---\n- list\n---\n", "---\na: &a [*a]\n---\n"} {
		if _, _, err := Decode([]byte(input)); err == nil {
			t.Fatal("bad header accepted")
		}
	}
}
