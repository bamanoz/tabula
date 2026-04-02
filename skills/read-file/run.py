import sys

if len(sys.argv) < 2:
    print("Error: no file path provided", file=sys.stderr)
    sys.exit(1)

filepath = sys.argv[1]

try:
    with open(filepath, 'r', encoding='utf-8') as f:
        content = f.read()
    print(content, end='')
except FileNotFoundError:
    print(f"Error: file not found: {filepath}", file=sys.stderr)
    sys.exit(1)
except PermissionError:
    print(f"Error: permission denied: {filepath}", file=sys.stderr)
    sys.exit(1)
except Exception as e:
    print(f"Error: {e}", file=sys.stderr)
    sys.exit(1)
