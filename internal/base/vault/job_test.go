package vault

import "testing"

// Guards: REQ-1
func TestJobPathRecognized(t *testing.T) {
	for _, tc := range []struct{ id, path string }{{"ping", "jobs/ping.md"}, {"code/ping", "agents/code/jobs/ping.md"}} {
		kind, id, archived := inferIdentity(tc.path, Record{})
		if kind != "job" || id != tc.id || archived {
			t.Fatalf("%s: %s %s %v", tc.path, kind, id, archived)
		}
		if got := defaultPath("job", tc.id); got != "vault/"+tc.path {
			t.Fatalf("path=%s", got)
		}
	}
}
