"""
Embeddings module for Tabula memory skill.

Uses OpenAI API (text-embedding-3-small) for vector embeddings.
Falls back gracefully when API is unavailable.
"""

import json
import math
import os
import sys
import urllib.request
import urllib.error
from pathlib import Path

ROOT = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

os.environ.setdefault("TABULA_HOME", ROOT)

from skills.lib import load_skill_config


def load_embedding_settings() -> dict:
    settings = load_skill_config(Path(__file__).resolve().parent)
    return {
        "api_key": settings["embedding.api_key"],
        "model": settings["embedding.model"],
        "base_url": settings["embedding.base_url"],
    }


SETTINGS = load_embedding_settings()
API_KEY = SETTINGS["api_key"]
MODEL = SETTINGS["model"]
BASE_URL = SETTINGS["base_url"]
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
