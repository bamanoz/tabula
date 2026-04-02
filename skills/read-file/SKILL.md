# read-file

Reads a file and prints its contents to stdout.

## Usage

    SPAWN python3 skills/read-file/run.py <path>

## Arguments

- `<path>` (required) — path to the file to read (positional argument)

## Output

Prints the file contents to stdout. Errors are written to stderr with exit code 1.

## Examples

    SPAWN python3 skills/read-file/run.py /etc/hostname
    SPAWN python3 skills/read-file/run.py skills/read-file/SKILL.md
