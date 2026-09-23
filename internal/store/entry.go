package store

// Entry is what is persisted per payload_id: the original text (for round-trip
// unmasking) and, in token mode, the token → original-value map.
type Entry struct {
	Original string
	Tokens   map[string]string
}