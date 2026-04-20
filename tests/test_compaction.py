#!/usr/bin/env python3
"""Unit tests for conversation compaction and history restore."""

from __future__ import annotations

import json
import os
import sys
import tempfile
from pathlib import Path
from unittest.mock import MagicMock

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

from skills.lib.compaction import (
    estimate_tokens,
    should_compact,
    get_context_window,
    compact_messages_anthropic,
    compact_messages_openai,
    _extract_summary,
    KEEP_LAST_MESSAGES,
    COMPACT_THRESHOLD,
)
from skills.lib.providers import AnthropicSession, OpenAISession


# ---------------------------------------------------------------------------
# estimate_tokens
# ---------------------------------------------------------------------------

def test_estimate_tokens_empty():
    assert estimate_tokens([]) == 0


def test_estimate_tokens_basic():
    msgs = [{"role": "user", "content": "hello"}]
    est = estimate_tokens(msgs)
    assert est > 0
    # "hello" is ~5 chars, with JSON overhead ~35 chars -> ~12 tokens
    assert est < 100


def test_estimate_tokens_scales_with_content():
    short = [{"role": "user", "content": "hi"}]
    long = [{"role": "user", "content": "x" * 10000}]
    assert estimate_tokens(long) > estimate_tokens(short) * 10


# ---------------------------------------------------------------------------
# get_context_window
# ---------------------------------------------------------------------------

def test_get_context_window_known():
    assert get_context_window("claude-sonnet-4-6") == 200_000


def test_get_context_window_unknown():
    assert get_context_window("some-unknown-model") == 200_000


# ---------------------------------------------------------------------------
# should_compact
# ---------------------------------------------------------------------------

def test_should_compact_too_few_messages():
    """Never compact if fewer than KEEP_LAST + 2 messages."""
    msgs = [{"role": "user", "content": "hi"}] * 3
    assert not should_compact(msgs, "claude-sonnet-4-6", system_prompt="x" * 1000)


def test_should_compact_below_threshold():
    """Don't compact when estimated tokens are below threshold."""
    msgs = [{"role": "user", "content": "hi"}] * 20
    # 20 tiny messages + no system prompt = well under 200k * 0.8
    assert not should_compact(msgs, "claude-sonnet-4-6")


def test_should_compact_with_large_system_prompt():
    """System prompt pushes estimate over threshold."""
    msgs = [{"role": "user", "content": "hi"}] * 20
    # 200k * 0.8 = 160k threshold. System prompt of 800k chars / 4 = 200k tokens > 160k.
    big_prompt = "x" * 800_000
    assert should_compact(msgs, "claude-sonnet-4-6", system_prompt=big_prompt)


def test_should_compact_with_large_messages():
    """Large messages alone can trigger compaction."""
    msgs = [{"role": "user", "content": "x" * 50_000}] * 20
    assert should_compact(msgs, "claude-sonnet-4-6")


def test_should_compact_respects_env_threshold(monkeypatch):
    """Low threshold makes compaction trigger sooner."""
    # Reload module with custom env
    monkeypatch.setenv("TABULA_COMPACT_THRESHOLD", "0.001")
    monkeypatch.setenv("TABULA_COMPACT_KEEP_LAST", "2")
    # Re-import to pick up env
    import importlib
    import skills.lib.compaction as comp
    importlib.reload(comp)
    try:
        msgs = [{"role": "user", "content": "hello world"}] * 5
        # threshold = 200k * 0.001 = 200 tokens. Even tiny messages + no prompt may be close.
        # With system_prompt it should definitely trigger.
        assert comp.should_compact(msgs, "claude-sonnet-4-6", system_prompt="x" * 2000)
    finally:
        importlib.reload(comp)  # restore defaults


# ---------------------------------------------------------------------------
# _extract_summary
# ---------------------------------------------------------------------------

def test_extract_summary_with_tags():
    text = "preamble\n<summary>\nthe summary\n</summary>\npostamble"
    assert _extract_summary(text) == "the summary"


def test_extract_summary_without_tags():
    text = "just plain text"
    assert _extract_summary(text) == "just plain text"


def test_extract_summary_empty():
    assert _extract_summary("") == ""


# ---------------------------------------------------------------------------
# Provider needs_compact / compact integration
# ---------------------------------------------------------------------------

def test_anthropic_needs_compact_includes_system_prompt():
    """AnthropicSession.needs_compact() accounts for system prompt."""
    session = AnthropicSession(
        system_prompt="x" * 600_000,  # ~150k tokens
        model="claude-sonnet-4-6",
        api_key="fake",
        base_url="http://localhost",
        tools=[],
    )
    for i in range(15):
        session.messages.append({"role": "user", "content": f"msg {i}"})
        session.messages.append({"role": "assistant", "content": [{"type": "text", "text": f"reply {i}"}]})
    assert session.needs_compact()


def test_anthropic_needs_compact_small_conversation():
    """Small conversation doesn't trigger compaction."""
    session = AnthropicSession(
        system_prompt="short prompt",
        model="claude-sonnet-4-6",
        api_key="fake",
        base_url="http://localhost",
        tools=[],
    )
    session.messages.append({"role": "user", "content": "hi"})
    session.messages.append({"role": "assistant", "content": [{"type": "text", "text": "hello"}]})
    assert not session.needs_compact()


def test_openai_needs_compact_includes_system_prompt():
    """OpenAISession.needs_compact() accounts for system prompt."""
    session = OpenAISession(
        system_prompt="x" * 600_000,
        model="gpt-4.1",
        api_key="fake",
        base_url="http://localhost",
        tools=[],
    )
    for i in range(15):
        session.pending_input.append({"role": "user", "content": f"msg {i}"})
        session.pending_input.append({"role": "assistant", "content": f"reply {i}"})
    assert session.needs_compact()


# ---------------------------------------------------------------------------
# _restore_history — the critical logic
# ---------------------------------------------------------------------------

def _make_driver_runtime(history_entries: list[dict], system_prompt: str = "test prompt"):
    """Create a DriverRuntime with a pre-populated history file."""
    from skills.lib.driver_runtime import DriverRuntime, DriverConfig

    tmpdir = tempfile.mkdtemp(prefix="tabula-test-")
    session_dir = Path(tmpdir) / "data" / "sessions" / "test-session"
    session_dir.mkdir(parents=True)
    history_path = session_dir / "history.jsonl"

    with open(history_path, "w") as f:
        for entry in history_entries:
            f.write(json.dumps(entry, ensure_ascii=False) + "\n")

    config = DriverConfig(name="test", url="ws://localhost:0/ws", session="test-session")

    class FakeProvider:
        def __init__(self):
            self.system_prompt = system_prompt
            self.messages = []
            self.model = "claude-sonnet-4-6"
            self._restored = []

        def restore_history(self, entries):
            self._restored = list(entries)
            for entry in entries:
                role = entry.get("role")
                if role == "user":
                    self.messages.append({"role": "user", "content": entry.get("text", "")})
                elif role == "assistant":
                    self.messages.append({"role": "assistant", "content": entry.get("text", "")})

    runtime = object.__new__(DriverRuntime)
    runtime.config = config
    runtime.provider = FakeProvider()
    runtime.log = lambda msg: None
    runtime._history_file = open(history_path, "a")

    return runtime, tmpdir


def test_restore_history_no_compaction():
    """Without compaction markers, all entries are restored."""
    entries = [
        {"role": "user", "text": "hello"},
        {"role": "assistant", "text": "hi there"},
        {"role": "user", "text": "how are you"},
        {"role": "assistant", "text": "good"},
    ]
    runtime, tmpdir = _make_driver_runtime(entries)
    try:
        runtime._restore_history()
        assert len(runtime.provider._restored) == 4
        assert runtime.provider._restored[0]["text"] == "hello"
        assert runtime.provider._restored[-1]["text"] == "good"
    finally:
        runtime._history_file.close()
        import shutil
        shutil.rmtree(tmpdir, ignore_errors=True)


def test_restore_history_with_compaction():
    """After compaction, only summary + post-compaction entries are restored."""
    entries = [
        {"role": "user", "text": "old message 1"},
        {"role": "assistant", "text": "old reply 1"},
        {"role": "user", "text": "old message 2"},
        {"role": "assistant", "text": "old reply 2"},
        {"role": "system", "type": "compaction", "summary": "User asked two questions. Assistant replied."},
        {"role": "user", "text": "new message after compaction"},
        {"role": "assistant", "text": "new reply"},
    ]
    runtime, tmpdir = _make_driver_runtime(entries)
    try:
        runtime._restore_history()
        restored = runtime.provider._restored
        # Should be: summary_user + summary_assistant + 2 post-compaction entries = 4
        assert len(restored) == 4
        assert "<conversation_summary>" in restored[0]["text"]
        assert "User asked two questions" in restored[0]["text"]
        assert restored[1]["text"] == "I have the full context from our previous conversation. I'll continue from where we left off."
        assert restored[2]["text"] == "new message after compaction"
        assert restored[3]["text"] == "new reply"
    finally:
        runtime._history_file.close()
        import shutil
        shutil.rmtree(tmpdir, ignore_errors=True)


def test_restore_history_multiple_compactions():
    """With multiple compactions, only the last one is used."""
    entries = [
        {"role": "user", "text": "ancient message"},
        {"role": "system", "type": "compaction", "summary": "First summary (stale)"},
        {"role": "user", "text": "middle message"},
        {"role": "assistant", "text": "middle reply"},
        {"role": "system", "type": "compaction", "summary": "Second summary (current)"},
        {"role": "user", "text": "latest message"},
        {"role": "assistant", "text": "latest reply"},
    ]
    runtime, tmpdir = _make_driver_runtime(entries)
    try:
        runtime._restore_history()
        restored = runtime.provider._restored
        # summary_user + summary_assistant + 2 post-compaction = 4
        assert len(restored) == 4
        assert "Second summary (current)" in restored[0]["text"]
        assert "First summary" not in restored[0]["text"]
        assert restored[2]["text"] == "latest message"
    finally:
        runtime._history_file.close()
        import shutil
        shutil.rmtree(tmpdir, ignore_errors=True)


def test_restore_history_compaction_at_end():
    """Compaction as last entry — only summary, no post-compaction messages."""
    entries = [
        {"role": "user", "text": "msg1"},
        {"role": "assistant", "text": "reply1"},
        {"role": "system", "type": "compaction", "summary": "Everything was discussed."},
    ]
    runtime, tmpdir = _make_driver_runtime(entries)
    try:
        runtime._restore_history()
        restored = runtime.provider._restored
        # Just summary_user + summary_assistant
        assert len(restored) == 2
        assert "Everything was discussed." in restored[0]["text"]
    finally:
        runtime._history_file.close()
        import shutil
        shutil.rmtree(tmpdir, ignore_errors=True)


def test_restore_history_empty():
    """Empty history file — nothing to restore."""
    runtime, tmpdir = _make_driver_runtime([])
    try:
        runtime._restore_history()
        assert len(runtime.provider._restored) == 0
    finally:
        runtime._history_file.close()
        import shutil
        shutil.rmtree(tmpdir, ignore_errors=True)


# ---------------------------------------------------------------------------
# In-memory compaction — provider.messages after compact()
# ---------------------------------------------------------------------------

def test_anthropic_compact_replaces_messages(monkeypatch):
    """After compact(), provider.messages contains only summary + recent, old messages are gone."""
    session = AnthropicSession(
        system_prompt="test prompt",
        model="claude-sonnet-4-6",
        api_key="fake",
        base_url="http://localhost",
        tools=[],
    )
    # Build a conversation: 10 user/assistant pairs = 20 messages
    for i in range(10):
        session.messages.append({"role": "user", "content": f"user message {i}"})
        session.messages.append({"role": "assistant", "content": [{"type": "text", "text": f"reply {i}"}]})

    original_count = len(session.messages)
    assert original_count == 20

    # Mock compact_messages_anthropic to return a fake summary without calling the API
    def fake_compact(*, api_key, api_url, model, system_prompt, messages, keep_last=10, logger=None):
        recent = messages[-keep_last:]
        summary = "Summary of old conversation."
        new_messages = [
            {"role": "user", "content": f"<conversation_summary>\n{summary}\n</conversation_summary>"},
            {"role": "assistant", "content": "I have the full context from our previous conversation. I'll continue from where we left off."},
        ] + recent
        return new_messages, summary

    monkeypatch.setattr("skills.lib.compaction.compact_messages_anthropic", fake_compact)

    # Force should_compact to return True
    monkeypatch.setattr("skills.lib.compaction.should_compact", lambda msgs, model, system_prompt="": True)

    summary = session.compact()
    assert summary == "Summary of old conversation."

    # Old messages should be gone, only summary pair + keep_last remain
    expected = 2 + 10  # summary_user + summary_assistant + recent
    assert len(session.messages) == expected

    # First message is the summary
    assert "<conversation_summary>" in session.messages[0]["content"]
    assert "Summary of old conversation" in session.messages[0]["content"]

    # Last messages are the most recent from original conversation
    assert session.messages[-1]["content"] == [{"type": "text", "text": "reply 9"}]
    assert session.messages[-2]["content"] == "user message 9"

    # Old messages are NOT present
    all_content = json.dumps(session.messages)
    assert "user message 0" not in all_content
    assert "reply 0" not in all_content


def test_anthropic_compact_subsequent_messages_append_normally(monkeypatch):
    """After compaction, new messages append to the compacted history, not the original."""
    session = AnthropicSession(
        system_prompt="test prompt",
        model="claude-sonnet-4-6",
        api_key="fake",
        base_url="http://localhost",
        tools=[],
    )
    for i in range(10):
        session.messages.append({"role": "user", "content": f"old msg {i}"})
        session.messages.append({"role": "assistant", "content": [{"type": "text", "text": f"old reply {i}"}]})

    def fake_compact(*, api_key, api_url, model, system_prompt, messages, keep_last=10, logger=None):
        recent = messages[-keep_last:]
        new_messages = [
            {"role": "user", "content": "<conversation_summary>\nSummary.\n</conversation_summary>"},
            {"role": "assistant", "content": "Got it."},
        ] + recent
        return new_messages, "Summary."

    monkeypatch.setattr("skills.lib.compaction.compact_messages_anthropic", fake_compact)
    monkeypatch.setattr("skills.lib.compaction.should_compact", lambda msgs, model, system_prompt="": True)

    session.compact()
    count_after_compact = len(session.messages)

    # Simulate new turn
    session.add_user_text("new question after compaction")
    session.messages.append({"role": "assistant", "content": [{"type": "text", "text": "new answer"}]})

    assert len(session.messages) == count_after_compact + 2
    assert session.messages[-2]["content"] == "new question after compaction"
    assert session.messages[-1]["content"] == [{"type": "text", "text": "new answer"}]

    # Verify old messages still absent
    all_content = json.dumps(session.messages)
    assert "old msg 0" not in all_content


def test_openai_compact_replaces_pending_input(monkeypatch):
    """After compact(), OpenAI provider's pending_input is replaced with summary + recent."""
    session = OpenAISession(
        system_prompt="test prompt",
        model="gpt-4.1",
        api_key="fake",
        base_url="http://localhost",
        tools=[],
    )
    for i in range(10):
        session.pending_input.append({"role": "user", "content": f"user msg {i}"})
        session.pending_input.append({"role": "assistant", "content": f"reply {i}"})

    def fake_compact(*, api_key, api_url, model, system_prompt, pending_input, keep_last=10, logger=None):
        recent = pending_input[-keep_last:]
        new_input = [
            {"type": "message", "role": "user", "content": "<conversation_summary>\nSummary.\n</conversation_summary>"},
            {"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "Got it."}]},
        ] + recent
        return new_input, "Summary."

    monkeypatch.setattr("skills.lib.compaction.compact_messages_openai", fake_compact)
    monkeypatch.setattr("skills.lib.compaction.should_compact", lambda msgs, model, system_prompt="": True)

    summary = session.compact()
    assert summary == "Summary."

    assert len(session.pending_input) == 2 + 10

    all_content = json.dumps(session.pending_input)
    assert "user msg 0" not in all_content
    assert "<conversation_summary>" in all_content


# ---------------------------------------------------------------------------

if __name__ == "__main__":
    import pytest
    sys.exit(pytest.main([__file__, "-v"]))
