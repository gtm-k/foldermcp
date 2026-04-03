"""A simple addition tool.

tool: add
description: Adds two numbers together.
risk: read_only
param: a: number: First number
param: b: number: Second number
"""


def add(a: float = 0, b: float = 0) -> float:
    """Adds two numbers together."""
    return a + b
