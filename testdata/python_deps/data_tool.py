import pandas as pd
import requests
import os
import json

def fetch_data(url: str) -> str:
    """Fetch data from a URL and return as CSV string."""
    response = requests.get(url)
    df = pd.DataFrame(response.json())
    return df.to_csv()
