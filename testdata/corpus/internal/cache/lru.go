package cache

import (
	"container/list"
	"sync"
)

type entry struct {
	key   string
	value any
}

// LRUCache is a thread-safe least-recently-used cache.
type LRUCache struct {
	capacity int
	items    map[string]*list.Element
	evictList *list.List
	mu       sync.Mutex
}

// NewLRUCache creates an LRUCache with fixed capacity.
func NewLRUCache(capacity int) *LRUCache {
	return &LRUCache{
		capacity:  capacity,
		items:     make(map[string]*list.Element),
		evictList: list.New(),
	}
}

// Get retrieves an item and marks it as most recently used.
func (c *LRUCache) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.evictList.MoveToFront(elem)
		return elem.Value.(*entry).value, true
	}
	return nil, false
}

// Put adds an item, evicting the oldest if capacity is exceeded.
func (c *LRUCache) Put(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.evictList.MoveToFront(elem)
		elem.Value.(*entry).value = value
		return
	}

	if c.evictList.Len() >= c.capacity {
		c.evictOldest()
	}

	ent := &entry{key: key, value: value}
	elem := c.evictList.PushFront(ent)
	c.items[key] = elem
}

func (c *LRUCache) evictOldest() {
	elem := c.evictList.Back()
	if elem != nil {
		c.evictList.Remove(elem)
		kv := elem.Value.(*entry)
		delete(c.items, kv.key)
	}
}
