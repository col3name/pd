package store

// Entry is what is persisted per payload_id: the original text (for round-trip
// unmasking), the masked result we returned (for idempotent retries of the
// direct check) and, in token mode, the token → original-value map.
type Entry struct {
	Original string
	Masked   string
	Tokens   map[string]string
}