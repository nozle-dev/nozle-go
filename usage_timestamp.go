package nozle

import (
	"sync"
	"time"
)

// usageTimestampMemo remembers the timestamp filled in for an idempotency key, so a retry of the same
// key on the same client sends the same request.
// shortcut: lives in this client's memory, so a retry from another process must pass Timestamp; keeps the last 1000 keys.
type usageTimestampMemo struct {
	mu    sync.Mutex
	known map[string]string
	order []string
}

func (m *usageTimestampMemo) get(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if timestamp, ok := m.known[key]; ok {
		return timestamp
	}
	if m.known == nil {
		m.known = map[string]string{}
	}
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	m.known[key] = timestamp
	m.order = append(m.order, key)
	if len(m.order) > 1000 {
		delete(m.known, m.order[0])
		m.order = m.order[1:]
	}
	return timestamp
}
