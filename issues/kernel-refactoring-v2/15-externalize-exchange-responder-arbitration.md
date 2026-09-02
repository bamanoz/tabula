# Externalize exchange responder arbitration

**Type:** HITL  
**Status:** proposed

## What to build

Decide and implement an ownership boundary for suspended-exchange responder selection. Kernel may coordinate one pending exchange and commit/route its resolution, but should not prefer responders based on product client roles such as `tabula.client_role=user`.

Move responder ranking to the requesting bundle/gateway, an explicit exchange capability, or a generic caller-supplied arbitration strategy that cannot override tenant/session authority.

## Evidence

`internal/kernel/exchange.go:exchangeResponderScore` assigns priority to clients whose metadata role equals `user`.

## Required design work

- Define who selects eligible responders and how competing replies resolve.
- Preserve authorization, tenant/session isolation, disconnect cleanup, and deterministic first valid resolution.
- Decide whether kernel broadcasts to all eligible responders or routes to an explicit target set.

## Acceptance criteria

- [ ] Maintainer approves the responder-arbitration owner.
- [ ] Kernel contains no product-specific client-role preference.
- [ ] Bundles can express human approval and non-human exchange flows without kernel changes.
- [ ] Spoofed client metadata cannot gain response authority.
- [ ] Concurrent replies, responder disconnect, requester disconnect, and timeout behavior remain deterministic.
- [ ] Security/approval bundle and installed gateway tests cover the selected contract.

## Blocked by

None. This is an architecture gate for exchange arbitration changes.
