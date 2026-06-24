package redact

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

const (
	placeholderOpen    = "⟦RG:"
	placeholderClose   = "⟧"
	placeholderHashLen = 8
)

// PlaceholderMaxLen is the byte length of a placeholder token
// (e.g. "⟦RG:abcd1234⟧"). Streaming writers must buffer at least
// this many bytes across chunk boundaries to avoid splitting a token.
var PlaceholderMaxLen = len(placeholderOpen) + placeholderHashLen + len(placeholderClose)

// Store maps short hashes to the original sensitive values they replaced.
// It lives only in memory for the lifetime of the daemon process and is
// never persisted to disk.
type Store struct {
	mu     sync.RWMutex
	values map[string]string // hash -> original value
}

// NewStore creates an empty mapping store.
func NewStore() *Store {
	return &Store{values: make(map[string]string)}
}

// PlaceholderFor returns a stable placeholder token for value, recording the
// mapping so it can later be restored. The same value always maps to the
// same placeholder.
func (s *Store) PlaceholderFor(value string) string {
	h := hashValue(value)
	placeholder := placeholderOpen + h + placeholderClose
	s.mu.RLock()
	_, ok := s.values[h]
	s.mu.RUnlock()
	if ok {
		return placeholder
	}
	s.mu.Lock()
	s.values[h] = value
	s.mu.Unlock()
	return placeholder
}

// Lookup returns the original value for a given placeholder hash, if known.
func (s *Store) Lookup(hash string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.values[hash]
	return v, ok
}

func hashValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:placeholderHashLen]
}
