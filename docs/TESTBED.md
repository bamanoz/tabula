# Tabula Testbed

`tabula-testbed` runs bundle and skill tests in an isolated Tabula runtime. It
does not modify your real `$TABULA_HOME`, restart your active kernel, or use fixed ports.

Install from a Tabula checkout:

```bash
python3 -m pip install -e /path/to/tabula/tools/tabula-testbed
```

Install from Git in CI:

```bash
python3 -m pip install \
  'git+https://github.com/bamanoz/tabula.git@v0.9.0#subdirectory=tools/tabula-testbed'
```

## Common Commands

List discovered suites:

```bash
tabula-testbed list --tabula-root /path/to/tabula
```

Run platform baseline:

```bash
tabula-testbed run --tabula-root /path/to/tabula
```

Use a pinned Tabula source ref instead of a local checkout:

```bash
tabula-testbed run --tabula-version v0.9.0
```

`--tabula-version` clones/caches the requested Tabula ref under
`~/.cache/tabula-testbed` when `--tabula-root` is not provided.

Run a bundle suite:

```bash
tabula-testbed run \
  --tabula-root /path/to/tabula \
  --source tabula-bundles=/path/to/tabula-bundles \
  --suite caveman
```

Run an external bundle repo:

```bash
tabula-testbed run \
  --tabula-root /path/to/tabula \
  --source ext=/path/to/my-tabula-bundles \
  --suite my-suite
```

Fast checks without starting a kernel:

```bash
tabula-testbed lint \
  --tabula-root /path/to/tabula \
  --source tabula-bundles=/path/to/tabula-bundles \
  --suite caveman
tabula-testbed direct \
  --tabula-root /path/to/tabula \
  --source tabula-bundles=/path/to/tabula-bundles \
  --suite caveman
```

Keep the temp runtime on failure or for debugging:

```bash
tabula-testbed run \
  --tabula-root /path/to/tabula \
  --source tabula-bundles=/path/to/tabula-bundles \
  --suite caveman \
  --keep
```

Emit machine-readable JSON on stdout:

```bash
tabula-testbed run --tabula-root /path/to/tabula --json
```

## Bundle Test Layout

Put tests next to the bundle:

```text
my-bundle/
  bundle.toml
  my-plugin/
    plugin.toml
    run.py
  tests/
    testbed.toml
    test_my_bundle.py
```

`tests/testbed.toml` declares what to install and what to run:

```toml
[bundles.my-bundle]
source = "source:ext#path=my-bundle"

[suites.my-suite]
set = "empty"
bundles = ["my-bundle"]
tests = ["test_my_bundle.py"]
```

For a bundle in the main `tabula-bundles` repo, use:

```toml
[suites.my-suite]
set = "empty"
components = ["workspace:exec", "my-bundle:my-component"]
tests = ["test_my_bundle.py"]
```

## Test API

Tests use the Python API:

```python
from tabula_testbed import TestbedClient

client = TestbedClient(url)
client.connect_join("testbed-my-suite")
client.wait_tools({"my_tool"})
result = client.call_tool("my_tool", {"text": "hello"}).json()
```

`client.wait_tools()` and `client.call_tool()` exercise plugin-published tools.
Instruction-only skills are installed as files but do not appear in the runtime
tool catalog.

## Pass Levels

- `--lint`: manifests, bundle roots, component paths, and test files exist.
- `--direct`: `--lint` plus Python syntax checks for selected components/tests.
- default run: full isolated runtime install + kernel + WebSocket/API tests.

## GitHub Actions

```yaml
jobs:
  testbed:
    runs-on: macos-latest
    steps:
      - uses: actions/checkout@v4
        with:
          path: bundle
      - uses: actions/setup-python@v5
        with:
          python-version: '3.12'
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
      - run: |
          python3 -m pip install \
            'git+https://github.com/bamanoz/tabula.git@v0.9.0#subdirectory=tools/tabula-testbed'
      - run: |
          tabula-testbed run \
            --tabula-version v0.9.0 \
            --source ext=${{ github.workspace }}/bundle \
            --suite my-suite
```
