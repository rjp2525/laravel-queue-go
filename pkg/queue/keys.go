package queue

import "strings"

// Keys generates Redis key names matching Laravel's convention.
// Uses string concatenation instead of fmt.Sprintf for hot-path performance.
type Keys struct {
	Prefix string
	// HashTag wraps the queue name in a Redis Cluster hash tag
	// ("queues:{default}") unless the name already contains one, so that all
	// keys of a queue live in the same slot. Laravel 13 does this on cluster
	// connections; earlier versions leave it to the queue name.
	HashTag bool
}

func NewKeys(prefix string) *Keys {
	return &Keys{Prefix: prefix}
}

func (k *Keys) Ready(queue string) string {
	if k.HashTag && !hasHashTag(queue) {
		return k.Prefix + "queues:{" + queue + "}"
	}
	return k.Prefix + "queues:" + queue
}

func (k *Keys) Delayed(queue string) string {
	return k.Ready(queue) + ":delayed"
}

func (k *Keys) Reserved(queue string) string {
	return k.Ready(queue) + ":reserved"
}

func (k *Keys) Notify(queue string) string {
	return k.Ready(queue) + ":notify"
}

// hasHashTag reports whether key contains a non-empty {tag}.
func hasHashTag(key string) bool {
	_, rest, found := strings.Cut(key, "{")
	return found && strings.IndexByte(rest, '}') > 0
}
