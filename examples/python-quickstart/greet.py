"""A simple greeting tool.

tool: greet
description: Returns a friendly greeting for the given name.
risk: read_only
param: name: string: The name to greet
"""


def greet(name: str = "World") -> str:
    """Returns a friendly greeting for the given name."""
    return f"Hello, {name}!"
