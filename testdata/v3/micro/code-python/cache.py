"""Thread-safe in-memory cache with TTL expiration and LRU eviction.

Designed for caching API responses, DNS lookups, or computed results
where staleness is bounded and memory usage must be controlled.
"""

import threading
import time
from typing import Any, Callable, Dict, Optional, Tuple
from collections import OrderedDict
from dataclasses import dataclass


@dataclass
class CacheEntry:
    """Single cache entry with metadata."""
    key: str
    value: Any
    created_at: float
    ttl: float
    hits: int = 0

    @property
    def is_expired(self) -> bool:
        return time.time() - self.created_at > self.ttl

    @property
    def age_seconds(self) -> float:
        return time.time() - self.created_at


class LRUCache:
    """Thread-safe LRU cache with TTL.

    Features:
        - O(1) get/put via OrderedDict
        - TTL per entry (checked on access, not background sweep)
        - Configurable max size with LRU eviction
        - Hit/miss statistics

    Example:
        cache = LRUCache(max_size=1000, default_ttl=300)
        cache.put("user:123", user_data)
        user = cache.get("user:123")  # returns None if expired
    """

    def __init__(self, max_size: int = 1000, default_ttl: float = 300.0):
        self._max_size = max_size
        self._default_ttl = default_ttl
        self._store: OrderedDict[str, CacheEntry] = OrderedDict()
        self._lock = threading.RLock()
        self._hits = 0
        self._misses = 0

    def get(self, key: str) -> Optional[Any]:
        """Get a value from cache. Returns None if missing or expired."""
        with self._lock:
            entry = self._store.get(key)
            if entry is None:
                self._misses += 1
                return None
            if entry.is_expired:
                del self._store[key]
                self._misses += 1
                return None
            # Move to end (most recently used)
            self._store.move_to_end(key)
            entry.hits += 1
            self._hits += 1
            return entry.value

    def put(self, key: str, value: Any, ttl: Optional[float] = None) -> None:
        """Store a value in cache with optional custom TTL."""
        with self._lock:
            if key in self._store:
                del self._store[key]
            elif len(self._store) >= self._max_size:
                self._evict_one()
            self._store[key] = CacheEntry(
                key=key,
                value=value,
                created_at=time.time(),
                ttl=ttl or self._default_ttl,
            )

    def delete(self, key: str) -> bool:
        """Remove an entry. Returns True if it existed."""
        with self._lock:
            if key in self._store:
                del self._store[key]
                return True
            return False

    def get_or_compute(
        self, key: str, compute_fn: Callable[[], Any], ttl: Optional[float] = None
    ) -> Any:
        """Get from cache or compute and store.

        This is the primary pattern for cache-aside usage:
            value = cache.get_or_compute("key", lambda: expensive_call())
        """
        value = self.get(key)
        if value is not None:
            return value
        value = compute_fn()
        self.put(key, value, ttl)
        return value

    def _evict_one(self) -> None:
        """Evict the least recently used entry."""
        if self._store:
            self._store.popitem(last=False)

    def clear(self) -> int:
        """Clear all entries. Returns count of entries removed."""
        with self._lock:
            count = len(self._store)
            self._store.clear()
            return count

    @property
    def stats(self) -> Dict[str, Any]:
        """Cache statistics."""
        with self._lock:
            total = self._hits + self._misses
            return {
                "size": len(self._store),
                "max_size": self._max_size,
                "hits": self._hits,
                "misses": self._misses,
                "hit_rate": self._hits / total if total > 0 else 0.0,
            }
