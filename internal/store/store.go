package store

import "context"

// Store persists entries keyed by payload_id for unmasking.
type Store interface {
	Save(ctx context.Context, id string, e Entry) error
	Get(ctx context.Context, id string) (Entry, bool, error)
}