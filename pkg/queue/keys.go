package queue

// Keys generates Redis key names matching Laravel's convention.
// Uses string concatenation instead of fmt.Sprintf for hot-path performance.
type Keys struct {
	Prefix string
}

func NewKeys(prefix string) *Keys {
	return &Keys{Prefix: prefix}
}

func (k *Keys) Ready(queue string) string {
	return k.Prefix + "queues:" + queue
}

func (k *Keys) Delayed(queue string) string {
	return k.Prefix + "queues:" + queue + ":delayed"
}

func (k *Keys) Reserved(queue string) string {
	return k.Prefix + "queues:" + queue + ":reserved"
}

func (k *Keys) Notify(queue string) string {
	return k.Prefix + "queues:" + queue + ":notify"
}
