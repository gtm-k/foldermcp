"""Text formatting utilities.

tool: format_markdown
description: Format text with markdown styling. Supports: bold, italic, code, heading.
risk: read_only
param: text: string: The text to format
param: style: string: The style to apply (bold, italic, code, heading)

tool: word_count
description: Count the number of words in the given text.
risk: read_only
param: text: string: The text to count words in
"""


def format_markdown(text: str, style: str = "bold") -> str:
    """Format text with markdown styling. Supports: bold, italic, code, heading."""
    styles = {"bold": f"**{text}**", "italic": f"*{text}*", "code": f"`{text}`", "heading": f"# {text}"}
    return styles.get(style, text)


def word_count(text: str) -> int:
    """Count the number of words in the given text."""
    return len(text.split())
