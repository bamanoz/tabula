# Review Rubric: Developer Experience

## Purpose
Evaluate developer-facing decisions in CREATIVE phase for DX quality.

## Scoring Dimensions

### Dimension 1: Discoverability
| Score | Guidance |
|-------|----------|
| 0-3 | Developers cannot find what they need; hidden conventions |
| 4-6 | Some discoverability but requires tribal knowledge |
| 7-9 | Clear file structure, naming, and documentation |
| 10 | Self-documenting; IDE support enables rapid discovery |

### Dimension 2: Error Message Quality
| Score | Guidance |
|-------|----------|
| 0-3 | Cryptic errors; no guidance on resolution |
| 4-6 | Errors identify problem but not solution |
| 7-9 | Errors include context, cause, and actionable fix |
| 10 | Errors prevent mistakes; suggest alternatives |

### Dimension 3: Onboarding Friction
| Score | Guidance |
|-------|----------|
| 0-3 | New developers need days to become productive |
| 4-6 | Onboarding possible but requires significant guidance |
| 7-9 | Clear setup docs; productive within hours |
| 10 | Zero-config start; instant productivity with guard rails |

### Dimension 4: Tooling Integration
| Score | Guidance |
|-------|----------|
| 0-3 | Breaks existing toolchain; requires manual workarounds |
| 4-6 | Works with tooling but misses optimization opportunities |
| 7-9 | Integrates well with standard dev tools |
| 10 | Enhances tooling; provides custom dev-time utilities |

### Dimension 5: Documentation Alignment
| Score | Guidance |
|-------|----------|
| 0-3 | No docs; or docs conflict with implementation |
| 4-6 | Basic docs exist but gaps or staleness |
| 7-9 | Comprehensive docs that match current state |
| 10 | Living docs with examples, edge cases, and rationale |

## AI-Slop Detectors
Flag the decision if ANY of the following appear:
- [ ] Generic buzzword salad without concrete trade-offs ("developer-friendly")
- [ ] Options differ only in naming, not developer impact
- [ ] "Best of both worlds" claim without acknowledging DX costs
- [ ] Missing token/performance constraint acknowledgment
- [ ] Justification ignores stated eager/lazy loading requirements

## Evaluation Output Block
After applying this rubric, append to the creative document:

```yaml
Rubric Review:
  rubric: rubric-devex.md
  dimensions:
    discoverability: [0-10]
    error_message_quality: [0-10]
    onboarding_friction: [0-10]
    tooling_integration: [0-10]
    documentation_alignment: [0-10]
  ai_slop_flags: [list or "none"]
  verdict: [PASS | REVISE | REJECT]
  notes: [1-2 lines on highest-impact finding]
```
