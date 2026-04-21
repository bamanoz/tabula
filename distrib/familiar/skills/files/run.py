#!/usr/bin/env python3
"""File tools for reading, searching, and editing text files."""

from __future__ import annotations

import contextlib
import fcntl
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile


READ_LIMIT_DEFAULT = 2000
LIST_LIMIT_DEFAULT = 200
SEARCH_LIMIT_DEFAULT = 100
MAX_OUTPUT_BYTES = 50 * 1024
MAX_LINE_LENGTH = 2000
MAX_LINE_SUFFIX = f"... (line truncated to {MAX_LINE_LENGTH} chars)"
STATE_VERSION = 1
RG_REQUIRED_ERROR = "tool requires ripgrep (`rg`) to be installed and available in PATH"


class ToolError(Exception):
    """Raised when a tool should return a structured error."""


class AddFileOp:
    def __init__(self, *, path: str, lines: list[str]) -> None:
        self.path = path
        self.lines = lines


class DeleteFileOp:
    def __init__(self, *, path: str) -> None:
        self.path = path


class UpdateFileOp:
    def __init__(self, *, path: str, move_to: str | None, hunks: list[list[str]]) -> None:
        self.path = path
        self.move_to = move_to
        self.hunks = hunks


def _json_error(message: str) -> str:
    return json.dumps({"error": message}, ensure_ascii=False)


def _resolve_path(path: str) -> str:
    return os.path.abspath(os.path.expanduser(path))


def _coerce_int(params: dict, key: str, *, default: int, minimum: int) -> int:
    value = params.get(key, default)
    try:
        parsed = int(value)
    except (TypeError, ValueError) as exc:
        raise ToolError(f"{key} must be an integer") from exc
    if parsed < minimum:
        raise ToolError(f"{key} must be greater than or equal to {minimum}")
    return parsed


def _coerce_bool(value: object) -> bool:
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        lowered = value.strip().lower()
        if lowered in {"1", "true", "yes", "on"}:
            return True
        if lowered in {"0", "false", "no", "off", ""}:
            return False
    return bool(value)


def _require_string(params: dict, key: str, *, allow_empty: bool = False) -> str:
    if key not in params:
        raise ToolError(f"{key} is required")
    value = params[key]
    if not isinstance(value, str):
        raise ToolError(f"{key} must be a string")
    if not allow_empty and value == "":
        raise ToolError(f"{key} is required")
    return value


def _line_count(text: str) -> int:
    if not text:
        return 0
    return text.count("\n") + (0 if text.endswith("\n") else 1)


def _truncate_line(text: str) -> str:
    if len(text) <= MAX_LINE_LENGTH:
        return text
    return text[:MAX_LINE_LENGTH] + MAX_LINE_SUFFIX


def _is_binary_file(path: str) -> bool:
    with open(path, "rb") as handle:
        chunk = handle.read(8192)
    return b"\0" in chunk


def _sha256_file(path: str) -> str:
    digest = hashlib.sha256()
    with open(path, "rb") as handle:
        for chunk in iter(lambda: handle.read(65536), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _build_snapshot(path: str, *, full_read: bool) -> dict:
    stat = os.stat(path)
    return {
        "mtime_ns": stat.st_mtime_ns,
        "size": stat.st_size,
        "sha256": _sha256_file(path),
        "full_read": full_read,
    }


def _state_path() -> str:
    session = os.environ.get("TABULA_SESSION", "default")
    namespace = hashlib.md5(f"{session}\0{os.getcwd()}".encode("utf-8")).hexdigest()[:12]
    return os.path.join(tempfile.gettempdir(), f"tabula-files-{namespace}.json")


@contextlib.contextmanager
def _locked_state() -> dict:
    state_path = _state_path()
    with open(state_path, "a+", encoding="utf-8") as handle:
        fcntl.flock(handle.fileno(), fcntl.LOCK_EX)
        handle.seek(0)
        raw = handle.read().strip()
        state = {"version": STATE_VERSION, "files": {}}
        if raw:
            try:
                loaded = json.loads(raw)
                if isinstance(loaded, dict) and isinstance(loaded.get("files"), dict):
                    state = loaded
            except json.JSONDecodeError:
                state = {"version": STATE_VERSION, "files": {}}

        if state.get("version") != STATE_VERSION or not isinstance(state.get("files"), dict):
            state = {"version": STATE_VERSION, "files": {}}

        try:
            yield state
        finally:
            handle.seek(0)
            handle.truncate()
            json.dump(state, handle, ensure_ascii=False, sort_keys=True)
            handle.flush()
            os.fsync(handle.fileno())
            fcntl.flock(handle.fileno(), fcntl.LOCK_UN)


def _record_snapshot(path: str, *, full_read: bool) -> None:
    abs_path = _resolve_path(path)
    snapshot = _build_snapshot(abs_path, full_read=full_read)
    with _locked_state() as state:
        state["files"][abs_path] = snapshot


def _remove_snapshot(path: str) -> None:
    abs_path = _resolve_path(path)
    with _locked_state() as state:
        state["files"].pop(abs_path, None)


def _require_fresh_full_read(path: str) -> None:
    abs_path = _resolve_path(path)
    with _locked_state() as state:
        snapshot = state["files"].get(abs_path)
    if not isinstance(snapshot, dict) or not snapshot.get("full_read"):
        raise ToolError("file was not fully read first - use read before editing")
    current = _build_snapshot(abs_path, full_read=True)
    if (
        snapshot.get("mtime_ns") != current["mtime_ns"]
        or snapshot.get("size") != current["size"]
        or snapshot.get("sha256") != current["sha256"]
    ):
        raise ToolError("file has been modified since it was read - use read again before editing")


def _ensure_text_file(path: str) -> None:
    if _is_binary_file(path):
        raise ToolError(f"cannot read binary file: {path}")


def _read_window(path: str, *, offset: int, limit: int) -> tuple[list[str], int, bool, bool]:
    start = offset - 1
    selected: list[str] = []
    total = 0
    bytes_used = 0
    more = False
    truncated = False

    with open(path, encoding="utf-8") as handle:
        for raw_line in handle:
            total += 1
            if total <= start:
                continue
            if len(selected) >= limit:
                more = True
                continue

            line = _truncate_line(raw_line.rstrip("\r\n"))
            size = len(line.encode("utf-8")) + (1 if selected else 0)
            if bytes_used + size > MAX_OUTPUT_BYTES:
                truncated = True
                more = True
                break

            selected.append(line)
            bytes_used += size

    if total < start and not (total == 0 and offset == 1):
        raise ToolError(f"offset {offset} is out of range for this file ({total} lines)")

    return selected, total, more, truncated


def _read_text(path: str) -> str:
    try:
        with open(path, encoding="utf-8") as handle:
            return handle.read()
    except UnicodeDecodeError as exc:
        raise ToolError(f"file is not valid UTF-8 text: {path}") from exc


def _atomic_write(path: str, content: str) -> None:
    parent = os.path.dirname(path)
    if parent:
        os.makedirs(parent, exist_ok=True)

    existing_mode = None
    if os.path.exists(path):
        existing_mode = os.stat(path).st_mode

    fd, temp_path = tempfile.mkstemp(prefix=".tabula-", dir=parent or ".")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            handle.write(content)
            handle.flush()
            os.fsync(handle.fileno())
        if existing_mode is not None:
            os.chmod(temp_path, existing_mode)
        os.replace(temp_path, path)
    finally:
        if os.path.exists(temp_path):
            os.unlink(temp_path)


def _sort_paths_by_mtime(paths: list[str]) -> list[str]:
    return sorted(paths, key=lambda item: (-os.path.getmtime(item), item))


def _normalize_joined_path(root: str, relative: str) -> str:
    return os.path.abspath(os.path.normpath(os.path.join(root, relative)))


def _require_rg() -> str:
    rg = shutil.which("rg")
    if not rg:
        raise ToolError(RG_REQUIRED_ERROR)
    return rg


def _run_rg(args: list[str], *, cwd: str) -> tuple[subprocess.CompletedProcess[str], bool]:
    rg = _require_rg()
    proc = subprocess.run(
        [rg, *args],
        cwd=cwd,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    if proc.returncode in (0, 1):
        return proc, False
    if proc.returncode == 2 and proc.stdout.strip():
        return proc, True
    message = proc.stderr.strip() or f"ripgrep failed with exit code {proc.returncode}"
    raise ToolError(message)


def _format_page_notice(kind: str, *, offset: int, shown: int, total: int) -> str:
    if shown == 0:
        return f"(No {kind} found)"
    end = offset + shown - 1
    if end < total:
        return f"(Showing {kind} {offset}-{end} of {total}. Use offset={end + 1} to continue.)"
    return f"(End of {kind} - total {total})"


def tool_read(params: dict) -> str:
    try:
        path = _resolve_path(_require_string(params, "path"))
        offset = _coerce_int(params, "offset", default=1, minimum=1)
        limit = _coerce_int(params, "limit", default=READ_LIMIT_DEFAULT, minimum=1)

        if not os.path.exists(path):
            raise ToolError(f"file not found: {path}")
        if os.path.isdir(path):
            raise ToolError(f"path is a directory: {path} (use list_dir instead)")
        _ensure_text_file(path)

        lines, total, more, truncated = _read_window(path, offset=offset, limit=limit)
        full_read = offset == 1 and not more and not truncated
        _record_snapshot(path, full_read=full_read)

        body = [f"{index}: {line}" for index, line in enumerate(lines, start=offset)]
        if truncated:
            notice = f"(Output capped at {MAX_OUTPUT_BYTES // 1024} KB. Showing lines {offset}-{offset + len(lines) - 1}. Use offset={offset + len(lines)} to continue.)"
        elif more:
            notice = _format_page_notice("lines", offset=offset, shown=len(lines), total=total)
        else:
            notice = f"(End of file - total {total} lines)"

        return "\n".join(
            [
                f"<path>{path}</path>",
                "<type>file</type>",
                "<content>",
                *body,
                "</content>",
                "",
                notice,
            ]
        )
    except (OSError, ToolError) as exc:
        return _json_error(str(exc))


def _collect_dir_entries(path: str, *, depth: int) -> list[str]:
    entries: list[str] = []

    def walk(current: str, prefix: str, remaining: int) -> None:
        names = sorted(os.scandir(current), key=lambda entry: entry.name)
        for entry in names:
            rel = f"{prefix}{entry.name}" if not prefix else f"{prefix}/{entry.name}"
            if entry.is_symlink():
                entries.append(rel + "@")
                continue
            if entry.is_dir(follow_symlinks=False):
                entries.append(rel + "/")
                if remaining > 1:
                    walk(entry.path, rel, remaining - 1)
                continue
            entries.append(rel)

    walk(path, "", depth)
    return entries


def tool_list_dir(params: dict) -> str:
    try:
        path = _resolve_path(_require_string(params, "path"))
        offset = _coerce_int(params, "offset", default=1, minimum=1)
        limit = _coerce_int(params, "limit", default=LIST_LIMIT_DEFAULT, minimum=1)
        depth = _coerce_int(params, "depth", default=1, minimum=1)

        if not os.path.exists(path):
            raise ToolError(f"path not found: {path}")
        if not os.path.isdir(path):
            raise ToolError(f"path is not a directory: {path}")

        entries = _collect_dir_entries(path, depth=depth)
        start = offset - 1
        if len(entries) < start and not (len(entries) == 0 and offset == 1):
            raise ToolError(f"offset {offset} is out of range for this directory ({len(entries)} entries)")

        selected = entries[start : start + limit]
        notice = _format_page_notice("entries", offset=offset, shown=len(selected), total=len(entries))
        return "\n".join(
            [
                f"<path>{path}</path>",
                "<type>directory</type>",
                "<entries>",
                *selected,
                "</entries>",
                "",
                notice,
            ]
        )
    except (OSError, ToolError) as exc:
        return _json_error(str(exc))


def tool_glob(params: dict) -> str:
    try:
        pattern = _require_string(params, "pattern")
        search_path = _resolve_path(str(params.get("path", os.getcwd())))
        offset = _coerce_int(params, "offset", default=1, minimum=1)
        limit = _coerce_int(params, "limit", default=SEARCH_LIMIT_DEFAULT, minimum=1)

        if not os.path.exists(search_path):
            raise ToolError(f"path not found: {search_path}")
        if not os.path.isdir(search_path):
            raise ToolError(f"glob path must be a directory: {search_path}")

        proc, _partial = _run_rg(["--files", "--hidden", "--glob", "!.git/*", "--glob", pattern, "."], cwd=search_path)
        files = [_normalize_joined_path(search_path, item) for item in proc.stdout.splitlines() if item.strip()]
        files = _sort_paths_by_mtime(files)

        start = offset - 1
        if len(files) < start and not (len(files) == 0 and offset == 1):
            raise ToolError(f"offset {offset} is out of range for this result set ({len(files)} paths)")

        selected = files[start : start + limit]
        notice = _format_page_notice("paths", offset=offset, shown=len(selected), total=len(files))
        return "\n".join(selected + ([""] if selected else []) + [notice])
    except (OSError, ToolError) as exc:
        return _json_error(str(exc))


def _grep_scope(path_value: str | None) -> tuple[str, list[str]]:
    if path_value is None:
        return os.getcwd(), ["."]

    resolved = _resolve_path(path_value)
    if not os.path.exists(resolved):
        raise ToolError(f"path not found: {resolved}")
    if os.path.isdir(resolved):
        return resolved, ["."]
    return os.path.dirname(resolved) or os.getcwd(), [os.path.basename(resolved)]


def _parse_rg_matches(stdout: str, *, cwd: str) -> list[dict]:
    matches: list[dict] = []
    for line in stdout.splitlines():
        if not line.strip():
            continue
        event = json.loads(line)
        if event.get("type") != "match":
            continue
        data = event.get("data", {})
        rel_path = data.get("path", {}).get("text")
        if not isinstance(rel_path, str):
            continue
        abs_path = _normalize_joined_path(cwd, rel_path)
        text = str(data.get("lines", {}).get("text", "")).replace("\r", "").replace("\n", "\\n")
        submatches = data.get("submatches", [])
        matches.append(
            {
                "path": abs_path,
                "line": int(data.get("line_number") or 0),
                "text": _truncate_line(text),
                "count": len(submatches) if isinstance(submatches, list) else 1,
            }
        )
    return matches


def tool_grep(params: dict) -> str:
    try:
        pattern = _require_string(params, "pattern")
        include = params.get("include")
        if include is not None and not isinstance(include, str):
            raise ToolError("include must be a string")
        mode = str(params.get("mode", "lines"))
        if mode not in {"lines", "files", "count"}:
            raise ToolError("mode must be one of: lines, files, count")
        offset = _coerce_int(params, "offset", default=1, minimum=1)
        limit = _coerce_int(params, "limit", default=SEARCH_LIMIT_DEFAULT, minimum=1)
        ignore_case = _coerce_bool(params.get("ignore_case", False))
        multiline = _coerce_bool(params.get("multiline", False))

        cwd, targets = _grep_scope(params.get("path"))
        args = ["--json", "--hidden", "--glob", "!.git/*", "--no-messages"]
        if include:
            args.extend(["--glob", include])
        if ignore_case:
            args.append("-i")
        if multiline:
            args.append("-U")
        args.extend(["--", pattern, *targets])

        proc, partial = _run_rg(args, cwd=cwd)
        matches = _parse_rg_matches(proc.stdout, cwd=cwd)
        if not matches:
            return "No matches found"

        unique_paths = _sort_paths_by_mtime(sorted({item["path"] for item in matches}))
        mtime_order = {path: index for index, path in enumerate(unique_paths)}
        matches.sort(key=lambda item: (mtime_order[item["path"]], item["path"], item["line"]))

        if mode == "files":
            start = offset - 1
            if len(unique_paths) < start:
                raise ToolError(f"offset {offset} is out of range for this result set ({len(unique_paths)} files)")
            selected = unique_paths[start : start + limit]
            lines = selected + [""]
            lines.append(_format_page_notice("files", offset=offset, shown=len(selected), total=len(unique_paths)))
            if partial:
                lines.extend(["", "(Some paths were inaccessible and skipped)"])
            return "\n".join(lines)

        if mode == "count":
            per_file: dict[str, int] = {}
            total_matches = 0
            for item in matches:
                per_file[item["path"]] = per_file.get(item["path"], 0) + max(1, item["count"])
                total_matches += max(1, item["count"])
            rows = [f"{path}: {per_file[path]}" for path in unique_paths]
            start = offset - 1
            if len(rows) < start:
                raise ToolError(f"offset {offset} is out of range for this result set ({len(rows)} files)")
            selected = rows[start : start + limit]
            lines = [f"Found {total_matches} matches across {len(unique_paths)} files", *selected, "", _format_page_notice("files", offset=offset, shown=len(selected), total=len(rows))]
            if partial:
                lines.extend(["", "(Some paths were inaccessible and skipped)"])
            return "\n".join(lines)

        start = offset - 1
        if len(matches) < start:
            raise ToolError(f"offset {offset} is out of range for this result set ({len(matches)} matches)")
        selected_matches = matches[start : start + limit]
        output = [f"Found {len(matches)} matches"]
        current_path = None
        for item in selected_matches:
            if item["path"] != current_path:
                if current_path is not None:
                    output.append("")
                current_path = item["path"]
                output.append(f"{current_path}:")
            output.append(f"  Line {item['line']}: {item['text']}")
        output.extend(["", _format_page_notice("matches", offset=offset, shown=len(selected_matches), total=len(matches))])
        if partial:
            output.extend(["", "(Some paths were inaccessible and skipped)"])
        return "\n".join(output)
    except (json.JSONDecodeError, OSError, ToolError) as exc:
        return _json_error(str(exc))


def tool_write(params: dict) -> str:
    try:
        path = _resolve_path(_require_string(params, "path"))
        content = _require_string(params, "content", allow_empty=True)

        exists = os.path.exists(path)
        if exists and os.path.isdir(path):
            raise ToolError(f"path is a directory: {path}")
        if exists:
            _require_fresh_full_read(path)

        _atomic_write(path, content)
        _record_snapshot(path, full_read=True)
        return f"Wrote {_line_count(content)} lines to {path}"
    except (OSError, ToolError) as exc:
        return _json_error(str(exc))


def _replace_once_or_all(content: str, *, old: str, new: str, replace_all: bool) -> tuple[str, int]:
    count = content.count(old)
    if count == 0:
        raise ToolError("old_string not found in file")
    if count > 1 and not replace_all:
        raise ToolError(f"old_string found {count} times - set replace_all: true to replace all")
    if replace_all:
        return content.replace(old, new), count
    return content.replace(old, new, 1), 1


def tool_edit(params: dict) -> str:
    try:
        path = _resolve_path(_require_string(params, "path"))
        old = _require_string(params, "old_string")
        new = _require_string(params, "new_string", allow_empty=True)
        replace_all = _coerce_bool(params.get("replace_all", False))

        if not os.path.exists(path):
            raise ToolError(f"file not found: {path}")
        if os.path.isdir(path):
            raise ToolError(f"path is a directory: {path}")
        _require_fresh_full_read(path)

        content = _read_text(path)
        new_content, replaced = _replace_once_or_all(content, old=old, new=new, replace_all=replace_all)
        _atomic_write(path, new_content)
        _record_snapshot(path, full_read=True)
        return f"Replaced {replaced} occurrence(s) in {path}"
    except (OSError, ToolError) as exc:
        return _json_error(str(exc))


def tool_multiedit(params: dict) -> str:
    try:
        path = _resolve_path(_require_string(params, "path"))
        edits = params.get("edits")
        if not isinstance(edits, list) or not edits:
            raise ToolError("edits must be a non-empty array")

        if not os.path.exists(path):
            raise ToolError(f"file not found: {path}")
        if os.path.isdir(path):
            raise ToolError(f"path is a directory: {path}")
        _require_fresh_full_read(path)

        content = _read_text(path)
        total_replacements = 0
        for index, edit in enumerate(edits, start=1):
            if not isinstance(edit, dict):
                raise ToolError(f"edit #{index} must be an object")
            old = _require_string(edit, "old_string")
            new = _require_string(edit, "new_string", allow_empty=True)
            replace_all = _coerce_bool(edit.get("replace_all", False))
            content, replaced = _replace_once_or_all(content, old=old, new=new, replace_all=replace_all)
            total_replacements += replaced

        _atomic_write(path, content)
        _record_snapshot(path, full_read=True)
        return f"Applied {len(edits)} edit(s) with {total_replacements} replacement(s) to {path}"
    except (OSError, ToolError) as exc:
        return _json_error(str(exc))


def _detect_line_ending(text: str) -> str:
    return "\r\n" if "\r\n" in text else "\n"


def _join_lines(lines: list[str], *, line_ending: str, trailing_newline: bool) -> str:
    if not lines:
        return ""
    rendered = line_ending.join(lines)
    if trailing_newline:
        rendered += line_ending
    return rendered


def _find_subsequence(lines: list[str], target: list[str], start: int) -> int:
    if not target:
        return start
    upper = len(lines) - len(target) + 1
    for index in range(max(0, start), max(0, upper)):
        if lines[index : index + len(target)] == target:
            return index
    for index in range(0, max(0, upper)):
        if lines[index : index + len(target)] == target:
            return index
    raise ToolError("apply_patch verification failed: hunk context not found")


def _apply_update_hunks(content: str, hunks: list[list[str]]) -> str:
    line_ending = _detect_line_ending(content)
    trailing_newline = content.endswith(("\n", "\r"))
    lines = content.splitlines()
    cursor = 0

    for hunk in hunks:
        old_lines = [line[1:] for line in hunk if line[:1] in {" ", "-"}]
        new_lines = [line[1:] for line in hunk if line[:1] in {" ", "+"}]
        start = _find_subsequence(lines, old_lines, cursor)
        lines[start : start + len(old_lines)] = new_lines
        cursor = start + len(new_lines)

    return _join_lines(lines, line_ending=line_ending, trailing_newline=trailing_newline)


def _parse_patch(text: str) -> list[AddFileOp | DeleteFileOp | UpdateFileOp]:
    lines = text.splitlines()
    if lines[:1] != ["*** Begin Patch"] or lines[-1:] != ["*** End Patch"]:
        raise ToolError("patch rejected: patch must start with '*** Begin Patch' and end with '*** End Patch'")

    ops: list[AddFileOp | DeleteFileOp | UpdateFileOp] = []
    index = 1
    while index < len(lines) - 1:
        line = lines[index]
        if line.startswith("*** Add File: "):
            path = line[len("*** Add File: ") :].strip()
            index += 1
            content_lines: list[str] = []
            while index < len(lines) - 1 and not lines[index].startswith("*** "):
                if not lines[index].startswith("+"):
                    raise ToolError("patch rejected: add file contents must use '+' lines")
                content_lines.append(lines[index][1:])
                index += 1
            ops.append(AddFileOp(path=path, lines=content_lines))
            continue

        if line.startswith("*** Delete File: "):
            path = line[len("*** Delete File: ") :].strip()
            ops.append(DeleteFileOp(path=path))
            index += 1
            continue

        if line.startswith("*** Update File: "):
            path = line[len("*** Update File: ") :].strip()
            move_to = None
            index += 1
            if index < len(lines) - 1 and lines[index].startswith("*** Move to: "):
                move_to = lines[index][len("*** Move to: ") :].strip()
                index += 1

            hunks: list[list[str]] = []
            while index < len(lines) - 1 and not lines[index].startswith("*** "):
                if not lines[index].startswith("@@"):
                    raise ToolError("patch rejected: update hunks must start with '@@'")
                index += 1
                hunk_lines: list[str] = []
                while index < len(lines) - 1 and not lines[index].startswith("@@") and not lines[index].startswith("*** "):
                    if lines[index][:1] not in {" ", "+", "-"}:
                        raise ToolError("patch rejected: hunk lines must start with ' ', '+', or '-'")
                    hunk_lines.append(lines[index])
                    index += 1
                hunks.append(hunk_lines)

            ops.append(UpdateFileOp(path=path, move_to=move_to, hunks=hunks))
            continue

        raise ToolError(f"patch rejected: unknown patch header '{line}'")

    if not ops:
        raise ToolError("patch rejected: empty patch")
    return ops


def tool_apply_patch(params: dict) -> str:
    try:
        patch_text = _require_string(params, "patch_text")
        ops = _parse_patch(patch_text)

        planned: list[tuple[str, str, str | None, str | None]] = []
        touched_targets: set[str] = set()

        for op in ops:
            if isinstance(op, AddFileOp):
                target = _resolve_path(op.path)
                if os.path.exists(target):
                    raise ToolError(f"apply_patch verification failed: file already exists: {target}")
                content = "\n".join(op.lines)
                if target in touched_targets:
                    raise ToolError(f"apply_patch verification failed: duplicate target path: {target}")
                touched_targets.add(target)
                planned.append(("add", target, None, content))
                continue

            if isinstance(op, DeleteFileOp):
                target = _resolve_path(op.path)
                if not os.path.exists(target):
                    raise ToolError(f"apply_patch verification failed: file not found: {target}")
                if os.path.isdir(target):
                    raise ToolError(f"apply_patch verification failed: path is a directory: {target}")
                planned.append(("delete", target, None, None))
                continue

            source = _resolve_path(op.path)
            if not os.path.exists(source):
                raise ToolError(f"apply_patch verification failed: file not found: {source}")
            if os.path.isdir(source):
                raise ToolError(f"apply_patch verification failed: path is a directory: {source}")
            content = _read_text(source)
            updated = _apply_update_hunks(content, op.hunks) if op.hunks else content
            target = _resolve_path(op.move_to) if op.move_to else source
            if target != source and os.path.exists(target):
                raise ToolError(f"apply_patch verification failed: move target already exists: {target}")
            if target in touched_targets and target != source:
                raise ToolError(f"apply_patch verification failed: duplicate target path: {target}")
            touched_targets.add(target)
            planned.append(("move" if target != source else "update", source, target, updated))

        summary: list[str] = []
        for kind, source, target, content in planned:
            if kind == "add":
                assert content is not None
                _atomic_write(source, content)
                _record_snapshot(source, full_read=True)
                summary.append(f"A {source}")
                continue

            if kind == "delete":
                os.unlink(source)
                _remove_snapshot(source)
                summary.append(f"D {source}")
                continue

            assert content is not None
            destination = target or source
            _atomic_write(destination, content)
            _record_snapshot(destination, full_read=True)
            if kind == "move" and destination != source:
                os.unlink(source)
                _remove_snapshot(source)
                summary.append(f"R {source} -> {destination}")
            else:
                summary.append(f"M {destination}")

        return "Applied patch:\n" + "\n".join(summary)
    except (OSError, ToolError) as exc:
        return _json_error(str(exc))


TOOLS = {
    "read": tool_read,
    "list_dir": tool_list_dir,
    "glob": tool_glob,
    "grep": tool_grep,
    "write": tool_write,
    "edit": tool_edit,
    "multiedit": tool_multiedit,
    "apply_patch": tool_apply_patch,
}


def main() -> None:
    if len(sys.argv) >= 3 and sys.argv[1] == "tool":
        tool_name = sys.argv[2]
        handler = TOOLS.get(tool_name)
        if not handler:
            print(f"ERROR: unknown tool {tool_name}")
            sys.exit(1)

        try:
            params = json.load(sys.stdin)
        except json.JSONDecodeError as exc:
            print(_json_error(f"invalid JSON input: {exc}"))
            sys.exit(0)

        if not isinstance(params, dict):
            print(_json_error("tool input must be a JSON object"))
            sys.exit(0)

        print(handler(params))
        return

    print("Usage: run.py tool <tool_name>", file=sys.stderr)
    sys.exit(1)


if __name__ == "__main__":
    main()
