# API Reference

## Session Manager

The `SessionManager` class provides connection pooling and automatic retry
for HTTP requests.

### Configuration

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `max_retries` | int | 3 | Maximum retry attempts |
| `backoff_factor` | float | 0.5 | Exponential backoff multiplier |
| `retry_on_status` | tuple | (429, 500, 502, 503, 504) | HTTP status codes that trigger retry |
| `pool_connections` | int | 10 | Number of connection pools |
| `pool_maxsize` | int | 10 | Max connections per pool |

### Usage Examples

#### Basic Usage

```python
from session_manager import SessionManager

manager = SessionManager()
response = manager.request("GET", "https://api.example.com/data")
print(response["status"])
```

#### Custom Retry Configuration

```python
from session_manager import SessionManager, RetryConfig

config = RetryConfig(max_retries=5, backoff_factor=1.0)
manager = SessionManager(retry_config=config)
```

#### Monitoring Pool Statistics

```python
stats = manager.pool_stats
print(f"Active connections: {stats['in_use']}")
print(f"Available: {stats['available']}")
```

## Data Pipeline

The `Pipeline` class provides composable data transformations.

### Validation Levels

- **STRICT** — Fail immediately on any validation error
- **LENIENT** — Skip bad records, accumulate errors
- **COERCE** — Attempt type coercion before failing

### Building a Pipeline

```python
from data_pipeline import Pipeline, RecordSchema, FieldSchema

schema = RecordSchema("user", [
    FieldSchema("name", str),
    FieldSchema("age", int),
    FieldSchema("email", str, pattern=r".+@.+\..+"),
])

pipeline = (
    Pipeline("user-transform")
    .with_input_schema(schema)
    .add_step("normalize", lambda r: {**r, "name": r["name"].strip()})
)

results, errors = pipeline.process(records)
```

## Cache

The `LRUCache` provides thread-safe caching with TTL expiration.

### Cache-Aside Pattern

```python
from cache import LRUCache

cache = LRUCache(max_size=1000, default_ttl=300)
user = cache.get_or_compute(
    f"user:{user_id}",
    lambda: fetch_user_from_db(user_id)
)
```
