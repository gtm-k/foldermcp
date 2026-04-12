"""Session management with connection pooling and retry logic.

This module provides a session manager that wraps HTTP connections
with configurable retry behavior, timeouts, and connection pooling.
It's representative of real-world Python code the indexer must parse.
"""

import time
import logging
from typing import Optional, Dict, Any
from dataclasses import dataclass, field

logger = logging.getLogger(__name__)


@dataclass
class RetryConfig:
    """Configuration for retry behavior."""
    max_retries: int = 3
    backoff_factor: float = 0.5
    retry_on_status: tuple = (429, 500, 502, 503, 504)


@dataclass
class PoolConfig:
    """Connection pool configuration."""
    pool_connections: int = 10
    pool_maxsize: int = 10
    pool_block: bool = False


class ConnectionPool:
    """Manages a pool of reusable HTTP connections.

    The pool pre-allocates connections and reuses them across requests
    to avoid the overhead of establishing new TCP connections for each
    request. Thread-safe via internal locking.
    """

    def __init__(self, config: PoolConfig):
        self._config = config
        self._connections: list = []
        self._available: int = config.pool_maxsize
        self._created: int = 0

    def acquire(self) -> Optional[Any]:
        """Acquire a connection from the pool.

        Returns None if pool is exhausted and blocking is disabled.
        """
        if self._connections:
            self._available -= 1
            return self._connections.pop()
        if self._created < self._config.pool_maxsize:
            self._created += 1
            self._available -= 1
            return self._create_connection()
        if self._config.pool_block:
            # In a real implementation, this would block until available
            raise TimeoutError("Connection pool exhausted")
        return None

    def release(self, conn: Any) -> None:
        """Return a connection to the pool."""
        self._connections.append(conn)
        self._available += 1

    def _create_connection(self) -> Dict[str, Any]:
        """Create a new connection object."""
        return {
            "created_at": time.time(),
            "requests_served": 0,
        }

    @property
    def stats(self) -> Dict[str, int]:
        """Pool statistics for monitoring."""
        return {
            "total_created": self._created,
            "available": self._available,
            "in_use": self._created - self._available,
        }


class SessionManager:
    """High-level session manager with retry and pooling.

    Usage:
        config = RetryConfig(max_retries=5)
        manager = SessionManager(retry_config=config)
        response = manager.request("GET", "https://api.example.com/data")
    """

    def __init__(
        self,
        retry_config: Optional[RetryConfig] = None,
        pool_config: Optional[PoolConfig] = None,
    ):
        self._retry_config = retry_config or RetryConfig()
        self._pool = ConnectionPool(pool_config or PoolConfig())
        self._request_count = 0

    def request(
        self,
        method: str,
        url: str,
        headers: Optional[Dict[str, str]] = None,
        body: Optional[bytes] = None,
        timeout: float = 30.0,
    ) -> Dict[str, Any]:
        """Send an HTTP request with retry logic.

        Args:
            method: HTTP method (GET, POST, etc.)
            url: Target URL
            headers: Optional request headers
            body: Optional request body
            timeout: Request timeout in seconds

        Returns:
            Response dict with status, headers, and body.

        Raises:
            ConnectionError: After all retries exhausted.
        """
        last_error = None
        for attempt in range(self._retry_config.max_retries + 1):
            conn = self._pool.acquire()
            if conn is None:
                raise ConnectionError("No connections available")
            try:
                response = self._send(conn, method, url, headers, body, timeout)
                if response["status"] not in self._retry_config.retry_on_status:
                    self._request_count += 1
                    return response
                last_error = f"HTTP {response['status']}"
            except Exception as e:
                last_error = str(e)
            finally:
                self._pool.release(conn)

            if attempt < self._retry_config.max_retries:
                delay = self._retry_config.backoff_factor * (2 ** attempt)
                logger.warning(
                    "Request failed (attempt %d/%d), retrying in %.1fs: %s",
                    attempt + 1,
                    self._retry_config.max_retries,
                    delay,
                    last_error,
                )
                time.sleep(delay)

        raise ConnectionError(
            f"All {self._retry_config.max_retries} retries exhausted: {last_error}"
        )

    def _send(
        self,
        conn: Any,
        method: str,
        url: str,
        headers: Optional[Dict[str, str]],
        body: Optional[bytes],
        timeout: float,
    ) -> Dict[str, Any]:
        """Send a single request on a connection (stub)."""
        conn["requests_served"] += 1
        return {"status": 200, "headers": {}, "body": b""}

    @property
    def pool_stats(self) -> Dict[str, int]:
        """Get connection pool statistics."""
        return self._pool.stats
