# Iron Law of Debugging

> **Scope**: Applies to ALL BUILD agents. This file is eager-loaded.

## The Three Laws

### Law 1: No Fix Without Root Cause
Before changing ANY code to fix a bug:
- State your root-cause hypothesis in ONE sentence
- Identify the exact data flow path from input to failure
- If you cannot state the hypothesis, you are not ready to fix

### Law 2: Trace Before Touch
Before editing code:
- Read the actual failing code path (not just stack trace summary)
- Identify the specific line/condition where behavior diverges
- Confirm the divergence explains the symptoms

### Law 3: Stop After Three Failures
If you have attempted 3 distinct fix approaches and all failed:
- STOP making more changes
- Document: (1) what you tried, (2) what happened, (3) remaining hypotheses
- Escalate to human with this documentation

## Self-Check Before Every Fix

Ask yourself:
1. Can I state the root cause in one sentence? → If no, STOP
2. Have I read the actual failing code path? → If no, READ
3. Is this my 4th+ attempt? → If yes, ESCALATE

## Escalation Format

When escalating, provide:
```
ESCALATION: [brief symptom]
Root-cause hypothesis: [best guess]
Attempts:
1. [what] → [result]
2. [what] → [result]
3. [what] → [result]
Remaining hypotheses: [list]
```
