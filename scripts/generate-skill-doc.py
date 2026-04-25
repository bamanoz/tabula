#!/usr/bin/env python3
"""Generate a standard SKILL.md scaffold from SKILL.config.json.

Usage:
    python3 scripts/generate-skill-doc.py ../tabula-bundles/drivers/driver-openai
    python3 scripts/generate-skill-doc.py ../tabula-bundles/drivers/driver-openai --output ../tabula-bundles/drivers/driver-openai/SKILL.generated.md
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path


def load_frontmatter_and_title(skill_md_path: Path, fallback_id: str) -> tuple[str, str, str]:
    if not skill_md_path.is_file():
        return fallback_id, "TODO: add a short skill description", fallback_id

    raw = skill_md_path.read_text(encoding="utf-8")
    name = fallback_id
    description = "TODO: add a short skill description"
    title = fallback_id

    if raw.startswith("---\n"):
        end = raw.find("\n---\n", 4)
        if end != -1:
            frontmatter = raw[4:end]
            for line in frontmatter.splitlines():
                key, sep, value = line.partition(":")
                if not sep:
                    continue
                key = key.strip()
                value = value.strip().strip('"')
                if key == "name" and value:
                    name = value
                elif key == "description" and value:
                    description = value

    for line in raw.splitlines():
        if line.startswith("# "):
            title = line[2:].strip() or fallback_id
            break

    return name, description, title


def load_schema(skill_dir: Path) -> dict:
    schema_path = skill_dir / "SKILL.config.json"
    with schema_path.open("r", encoding="utf-8") as handle:
        return json.load(handle)


def render_default(value) -> str:
    if value is None:
        return "--"
    if isinstance(value, bool):
        return f"`{str(value).lower()}`"
    if isinstance(value, (int, float)):
        return f"`{value}`"
    if isinstance(value, str):
        return f"`{value}`"
    return f"`{json.dumps(value, ensure_ascii=False)}`"


def render_aliases(field: dict) -> str:
    aliases = field.get("env_aliases", [])
    if not aliases:
        return "--"
    return ", ".join(f"`{alias}`" for alias in aliases)


def render_notes(field: dict) -> str:
    notes: list[str] = []
    if field.get("required") and "default" not in field:
        notes.append("required")
    store_ids = field.get("store_ids")
    if isinstance(store_ids, list) and store_ids:
        notes.append("store fallback: " + ", ".join(f"`{item}`" for item in store_ids if isinstance(item, str)))
    elif isinstance(field.get("store_id"), str):
        notes.append(f"store id: `{field['store_id']}`")
    return "; ".join(notes) if notes else "--"


def render_configuration_table(entries: list[dict]) -> str:
    lines = [
        "| Key | Type | Default | Secret | Canonical env | Aliases | Notes |",
        "|---|---|---|---|---|---|---|",
    ]
    for field in entries:
        lines.append(
            "| {key} | {type_} | {default} | {secret} | {env} | {aliases} | {notes} |".format(
                key=f"`{field['key']}`",
                type_=f"`{field.get('type', 'string')}`",
                default=render_default(field.get("default")),
                secret="yes" if field.get("secret") else "no",
                env=f"`{field['env']}`" if field.get("env") else "--",
                aliases=render_aliases(field),
                notes=render_notes(field),
            )
        )
    return "\n".join(lines)


def render_scaffold(skill_dir: Path) -> str:
    schema = load_schema(skill_dir)
    skill_id = schema["id"]
    entries = schema.get("config", {}).get("entries", [])
    name, description, title = load_frontmatter_and_title(skill_dir / "SKILL.md", skill_id)

    return "\n".join(
        [
            "---",
            f"name: {name}",
            f'description: "{description}"',
            "---",
            "",
            f"# {title}",
            "",
            "TODO: add a short overview for this skill.",
            "",
            "## Run",
            "",
            "TODO: describe how this skill is started.",
            "",
            "## Config File",
            "",
            "Path:",
            "",
            f"    ~/.tabula/config/skills/{skill_id}.toml",
            "",
            "TODO: add a minimal example file.",
            "",
            "## Secrets",
            "",
            "Path:",
            "",
            "    ~/.tabula/secrets.json",
            "",
            "TODO: describe secret store ids or say that this skill has no schema-defined secrets.",
            "",
            "## Configuration",
            "",
            render_configuration_table(entries),
            "",
            "## Runtime Environment",
            "",
            "TODO: list runtime-only environment variables that are not part of `SKILL.config.json`.",
            "",
            "## Precedence",
            "",
            "1. env (`TABULA_SKILL_*`, then legacy alias)",
            f"2. `~/.tabula/config/skills/{skill_id}.toml`",
            "3. `~/.tabula/secrets.json` for secret fields",
            "4. schema defaults",
            "",
            "## Notes",
            "",
            "TODO: add behavior notes, protocol details, caveats, and examples.",
            "",
        ]
    )


def main() -> int:
    parser = argparse.ArgumentParser(description="Generate SKILL.md scaffold from SKILL.config.json")
    parser.add_argument("skill_dir", help="Path to the skill directory")
    parser.add_argument("--output", help="Write generated markdown to a file instead of stdout")
    args = parser.parse_args()

    skill_dir = Path(args.skill_dir).resolve()
    markdown = render_scaffold(skill_dir)
    if args.output:
        Path(args.output).write_text(markdown, encoding="utf-8")
    else:
        print(markdown, end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
