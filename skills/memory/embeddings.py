"""
Embeddings module for Tabula memory skill.

Uses OpenAI API (text-embedding-3-small) for vector embeddings.
Falls back gracefully when API is unavailable.
"""

import json
import math
import os
import urllib.request
import urllib.error

API_KEY = os.environ.get("TABULA_EMBEDDING_API_KEY") or os.environ.get("OPENAI_API_KEY", "")
MODEL = os.environ.get("TABULA_EMBEDDING_MODEL", "text-embedding-3-small")
BASE_URL = os.environ.get("OPENAI_BASE_URL", "https://api.openai.com")
API_URL = f"{BASE_URL}/v1/embeddings"


def available() -> bool:
    return bool(API_KEY)


def embed(texts: list[str]) -> list[list[float]] | None:
    """Embed a batch of texts. Returns list of vectors, or None on failure."""
    if not API_KEY or not texts:
        return None

    body = json.dumps({
        "model": MODEL,
        "input": texts,
    }).encode()

    req = urllib.request.Request(
        API_URL,
        data=body,
        headers={
            "Content-Type": "application/json",
            "Authorization": f"Bearer {API_KEY}",
        },
    )

    try:
        resp = urllib.request.urlopen(req, timeout=15)
        data = json.loads(resp.read())
        # Sort by index to preserve order
        items = sorted(data["data"], key=lambda x: x["index"])
        return [item["embedding"] for item in items]
    except (urllib.error.URLError, json.JSONDecodeError, KeyError, OSError):
        return None


def embed_one(text: str) -> list[float] | None:
    """Embed a single text. Returns vector or None."""
    result = embed([text])
    if result:
        return result[0]
    return None


def cosine_similarity(a: list[float], b: list[float]) -> float:
    """Cosine similarity between two vectors."""
    dot = sum(x * y for x, y in zip(a, b))
    norm_a = math.sqrt(sum(x * x for x in a))
    norm_b = math.sqrt(sum(x * x for x in b))
    if norm_a == 0 or norm_b == 0:
        return 0.0
    return dot / (norm_a * norm_b)
