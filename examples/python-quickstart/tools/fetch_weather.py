"""A weather lookup tool.

tool: get_weather
description: Get the current weather for a city. Returns a human-readable weather summary.
risk: read_only
param: city: string: The city to get weather for
"""


def get_weather(city: str) -> str:
    """Get the current weather for a city. Returns a human-readable weather summary."""
    return f"Weather in {city}: 72F, Sunny, Humidity: 45%"
