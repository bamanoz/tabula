# @tabula/skill-sdk

TypeScript SDK for writing Tabula skills.

This is the TypeScript counterpart of the Python `skills/_pylib`. It exposes the
same wire-protocol constants, a WebSocket client for long-running skills, and a
helper for tool subprocess skills.

The wire protocol is the source of truth. This SDK is a convenience layer; you
can write a Tabula skill in any language as long as it speaks the protocol.

## Install (workspace use)

The package is currently consumed in-repo. From a TypeScript skill:

```jsonc
// package.json
{
  "dependencies": {
    "@tabula/skill-sdk": "workspace:*"
  }
}
```

## Long-running skill

```ts
import {
  KernelConnection,
  MSG_CONNECT,
  MSG_JOIN,
  MSG_MESSAGE,
  tabulaUrl,
} from "@tabula/skill-sdk";

const url = tabulaUrl();
if (!url) throw new Error("TABULA_URL not set");

const conn = new KernelConnection(url);
await conn.ready();

await conn.send({
  type: MSG_CONNECT,
  name: "my-skill",
  sends: [MSG_MESSAGE],
  receives: [MSG_MESSAGE, "init"],
});

for await (const msg of conn.messages()) {
  if (msg.type === "init") {
    // bootstrapping payload from kernel
  } else if (msg.type === MSG_MESSAGE) {
    // handle inbound user/peer messages
  }
}
```

## Tool subprocess skill

```ts
// run.ts (entrypoint of a tool skill)
import { runTool } from "@tabula/skill-sdk";

await runTool(async ({ params }) => {
  const target = String(params.target ?? "world");
  return { greeting: `hello, ${target}` };
});
```

The kernel invokes the command, writes JSON params on stdin, and reads the
result from stdout. Non-zero exit + stderr signals failure.

## Stability

- Wire protocol: stable, versioned (`PROTOCOL_VERSION`).
- This SDK surface: pre-1.0, may change.
