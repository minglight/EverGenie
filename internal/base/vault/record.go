package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

func revision(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func normalizedBody(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	return strings.TrimRight(body, "\n")
}
func decodedRecord(fields map[string]any, body string, data []byte) Record {
	r := Record{Fields: fields, Body: normalizedBody(body), Revision: revision(data)}
	r.ID, _ = fields["id"].(string)
	r.Kind, _ = fields["kind"].(string)
	if value, ok := fields["updated_at"].(string); ok {
		r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, value)
	}
	return r
}
