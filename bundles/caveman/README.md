# Caveman skills for Tabula

[Caveman](https://github.com/JuliusBrussee/caveman) integrated into Tabula as an optional reference skill bundle — demonstrates prompt-only, tool, and hook skills.

> why use many token when few do trick

## Skills

| Directory | Type | What it does |
|-----------|------|--------------|
| `caveman/` | user-invocable | `/caveman [level]` — switch response style (lite/full/ultra/wenyan) |
| `caveman-commit/` | user-invocable | `/caveman-commit` — terse commit messages |
| `caveman-review/` | user-invocable | `/caveman-review` — one-line code review comments |
| `caveman-help/` | user-invocable | `/caveman-help` — quick reference card |
| `caveman-compress/` | tool | `caveman_compress` — compress .md files via Claude, with validation |
| `hook-caveman/` | hook | Auto-activates caveman mode on session start, tracks `/caveman` commands |

## Upstream

SKILL.md files and `caveman-compress/scripts/` are copied from [JuliusBrussee/caveman](https://github.com/JuliusBrussee/caveman) with minimal changes:

- `+user-invocable: true` in frontmatter (Tabula requires this for slash commands)
- `+tools:` block in `caveman-compress/SKILL.md` (Tabula tool registration)
- `CLAUDE.md` references replaced with generic terms

Tabula-specific files (not from upstream):

- `caveman-compress/run.py` — thin JSON stdin/stdout wrapper around the original CLI
- `hook-caveman/` — kernel hook subscriber for session-level mode tracking

## License

Original caveman project: [MIT](https://github.com/JuliusBrussee/caveman/blob/main/LICENSE) by Julius Brussee.
