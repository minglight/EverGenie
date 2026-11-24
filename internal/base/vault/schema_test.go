package vault

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

var _ Schema = Spec{}

func assertInvalid(t *testing.T, err error, field string) {
	t.Helper()
	var e *platform.Err
	if !errors.As(err, &e) || e.Code != sdk.ErrorCodeInvalid || e.Field != field {
		t.Fatalf("want invalid %s, got %v", field, err)
	}
}

// Guards: REQ-1, REQ-4
func TestLinkFormat(t *testing.T) {
	sc := Spec{Fields: []Field{{Name: "link", Type: Link, LinkKind: "task"}}}
	for _, value := range []string{"[[R-1]]", "SEC-1", "[[]]", "[[SEC-0]]", "[[SEC-a]]"} {
		assertInvalid(t, sc.Validate(Record{Fields: map[string]any{"link": value}}), "link")
	}
	if err := sc.Validate(Record{Fields: map[string]any{"link": "[[SEC-1]]"}}); err != nil {
		t.Fatal(err)
	}
	sc.Fields[0].LinkKind = "note"
	if err := sc.Validate(Record{Fields: map[string]any{"link": "[[code/readme]]"}}); err != nil {
		t.Fatal(err)
	}
}

// Guards: REQ-1, REQ-4
func TestSchemaRejectsMissingRequired(t *testing.T) {
	sc := Spec{Fields: []Field{{Name: "title", Type: String, Required: true}}}
	for _, fields := range []map[string]any{nil, {"title": ""}, {"title": nil}} {
		assertInvalid(t, sc.Validate(Record{Fields: fields}), "title")
	}
}

// Guards: REQ-1, REQ-4
func TestSchemaRejectsBadEnum(t *testing.T) {
	sc := Spec{Fields: []Field{{Name: "status", Type: Enum, Enum: []string{"todo", "done"}}}}
	assertInvalid(t, sc.Validate(Record{Fields: map[string]any{"status": "other"}}), "status")
	if err := sc.Validate(Record{Fields: map[string]any{"status": "todo"}}); err != nil {
		t.Fatal(err)
	}
}

// Guards: REQ-1, REQ-4
func TestDateTimeNeedsZone(t *testing.T) {
	sc := Spec{Fields: []Field{{Name: "at", Type: DateTime}, {Name: "day", Type: Date}}}
	for _, value := range []string{"2026-10-23T08:00:00", "2026-10-23", "2026-02-30T08:00:00+08:00"} {
		assertInvalid(t, sc.Validate(Record{Fields: map[string]any{"at": value}}), "at")
	}
	if err := sc.Validate(Record{Fields: map[string]any{"at": "2026-10-23T08:00:00+08:00", "day": "2026-10-23"}}); err != nil {
		t.Fatal(err)
	}
	assertInvalid(t, sc.Validate(Record{Fields: map[string]any{"day": "2026-02-30"}}), "day")
}

// Guards: REQ-1, REQ-4
func TestRequiredIf(t *testing.T) {
	sc := Spec{Fields: []Field{{Name: "result", Type: String, RequiredIf: func(r Record) bool { return r.Fields["status"] == "done" }}}}
	if err := sc.Validate(Record{Fields: map[string]any{"status": "doing"}}); err != nil {
		t.Fatal(err)
	}
	assertInvalid(t, sc.Validate(Record{Fields: map[string]any{"status": "done"}}), "result")
	if err := sc.Validate(Record{Fields: map[string]any{"status": "done", "result": "finished"}}); err != nil {
		t.Fatal(err)
	}
}

// Guards: REQ-1, REQ-4
func TestSpecClosed(t *testing.T) {
	at := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	sc := Spec{Kind: "example", ClosedFunc: func(Record) (bool, time.Time) { return true, at }}
	if closed, when := sc.Closed(Record{}); !closed || !when.Equal(at) {
		t.Fatal("callback not used")
	}
	if closed, when := (Spec{}).Closed(Record{}); closed || !when.IsZero() {
		t.Fatal("nil callback not open")
	}
	m := New(Deps{})
	m.RegisterSchema(sc)
	if closed, _ := m.Closed(Record{Kind: "unknown"}); closed {
		t.Fatal("unknown kind closed")
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if closed, when := m.Closed(Record{Kind: "example"}); !closed || !when.Equal(at) {
				t.Error("registered callback not used")
			}
		}()
	}
	wg.Wait()
}

// Guards: REQ-1, REQ-4
func TestRegisterSchemaDuplicatePanics(t *testing.T) {
	for _, kind := range []string{"", "example"} {
		t.Run(kind, func(t *testing.T) {
			m := New(Deps{})
			m.RegisterSchema(Spec{Kind: "example"})
			defer func() {
				if recover() == nil {
					t.Error("programming error did not panic")
				}
			}()
			m.RegisterSchema(Spec{Kind: kind})
		})
	}
}

// Guards: REQ-1, REQ-4
func TestSchemaFieldTypes(t *testing.T) {
	for _, tc := range []struct {
		kind      FieldType
		good, bad any
	}{{String, "text", 3}, {Int, 3, 3.5}, {Bool, false, "false"}, {List, []any{"a"}, "a"}, {Map, map[string]any{"x": 1}, []any{1}}} {
		sc := Spec{Fields: []Field{{Name: "value", Type: tc.kind}}}
		if err := sc.Validate(Record{Fields: map[string]any{"value": tc.good}}); err != nil {
			t.Fatal(err)
		}
		assertInvalid(t, sc.Validate(Record{Fields: map[string]any{"value": tc.bad}}), "value")
	}
}
