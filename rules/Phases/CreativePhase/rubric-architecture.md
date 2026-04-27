# Review Rubric: Architecture

## Purpose
Evaluate architectural decisions in CREATIVE phase for system design quality.

## Scoring Dimensions

### Dimension 1: Separation of Concerns
| Score | Guidance |
|-------|----------|
| 0-3 | Components tightly coupled; changes cascade unpredictably |
| 4-6 | Some boundaries exist but responsibilities overlap |
| 7-9 | Clear component boundaries with well-defined interfaces |
| 10 | Optimal separation enabling independent evolution and testing |

### Dimension 2: Extensibility
| Score | Guidance |
|-------|----------|
| 0-3 | Adding features requires rewriting core logic |
| 4-6 | Extension possible but requires touching multiple files |
| 7-9 | Extension points exist and are documented |
| 10 | Plugin-style architecture with clear extension contracts |

### Dimension 3: Failure Isolation
| Score | Guidance |
|-------|----------|
| 0-3 | Single component failure cascades to system-wide outage |
| 4-6 | Some isolation exists but error boundaries are unclear |
| 7-9 | Failures contained with graceful degradation paths |
| 10 | Comprehensive fault tolerance with recovery mechanisms |

### Dimension 4: Constraint Fit
| Score | Guidance |
|-------|----------|
| 0-3 | Design ignores stated constraints (token budget, permissions, etc.) |
| 4-6 | Constraints acknowledged but not fully addressed |
| 7-9 | Design meets all stated constraints with clear trade-offs |
| 10 | Constraints met elegantly with room for future tightening |

### Dimension 5: Simplicity (Occam)
| Score | Guidance |
|-------|----------|
| 0-3 | Overengineered with unnecessary abstractions |
| 4-6 | Some complexity justified but could be simpler |
| 7-9 | Minimal complexity for the requirements |
| 10 | Elegant simplicity that makes alternatives obviously inferior |

## AI-Slop Detectors
Flag the decision if ANY of the following appear:
- [ ] Generic buzzword salad without concrete trade-offs ("leverages best practices")
- [ ] Options differ only in naming, not substance
- [ ] "Best of both worlds" claim without acknowledging costs
- [ ] Missing constraint acknowledgment from the plan
- [ ] Justification cites irrelevant criteria or restates the problem

## Evaluation Output Block
After applying this rubric, append to the creative document:

```yaml
Rubric Review:
  rubric: rubric-architecture.md
  dimensions:
    separation_of_concerns: [0-10]
    extensibility: [0-10]
    failure_isolation: [0-10]
    constraint_fit: [0-10]
    simplicity: [0-10]
  ai_slop_flags: [list or "none"]
  verdict: [PASS | REVISE | REJECT]
  notes: [1-2 lines on highest-impact finding]
```
