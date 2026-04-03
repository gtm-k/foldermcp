"""A read-only database query tool.

tool: query_database
description: Execute a read-only SQL query against the analytics database.
risk: read_only
param: sql: string: The SQL query to execute
param: limit: number: Maximum number of rows to return
"""


def query_database(sql: str, limit: int = 100) -> str:
    """Execute a read-only SQL query against the analytics database.
    Returns results as a formatted table string.
    """
    return f"Results for: {sql} (limit {limit})\n| id | name | value |\n| 1 | test | 42 |"
