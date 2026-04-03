"""Tools using the @tool decorator."""


def tool(description=None, risk="read_only", name=None):
    """FolderMCP tool decorator (inline copy for testing)."""
    def decorator(func):
        func._foldermcp_tool = True
        func._foldermcp_description = description or func.__doc__ or f"Calls {func.__name__}"
        func._foldermcp_risk = risk
        func._foldermcp_name = name or func.__name__
        return func
    return decorator


@tool(description="Add two numbers", risk="read_only")
def add(a: int, b: int) -> int:
    """This docstring should be overridden by the decorator description."""
    return a + b


@tool(description="Delete a record permanently", risk="destructive", name="remove_record")
def delete_record(record_id: str) -> bool:
    """Remove a record from the database."""
    return True


@tool(description="Fetch user profile from API", risk="network")
def fetch_profile(user_id: str) -> dict:
    return {"id": user_id}


def plain_function(x: int) -> int:
    """A plain function without decorator should still be discovered."""
    return x * 2
