"""FolderMCP tool decorator for explicit tool registration.

Drop this file into your project alongside your tool scripts. It lets you
mark functions as FolderMCP tools with explicit metadata instead of relying
solely on docstring conventions.

Usage:
    from foldermcp_decorator import tool

    @tool(description="Add two numbers", risk="read_only")
    def add(a: int, b: int) -> int:
        return a + b

The decorator attaches metadata attributes that the FolderMCP Python
introspector reads during AST scanning.  It does NOT alter the function's
runtime behaviour — you can still call ``add(1, 2)`` as usual.
"""


def tool(description=None, risk="read_only", name=None):
    """Mark a function as a FolderMCP tool with explicit metadata.

    Args:
        description: Human-readable description of what the tool does.
                     Falls back to the function's docstring, then a default.
        risk:        One of read_only, side_effects, destructive, network.
        name:        Override the tool name (defaults to the function name).
    """
    def decorator(func):
        func._foldermcp_tool = True
        func._foldermcp_description = description or func.__doc__ or f"Calls {func.__name__}"
        func._foldermcp_risk = risk
        func._foldermcp_name = name or func.__name__
        return func
    return decorator
