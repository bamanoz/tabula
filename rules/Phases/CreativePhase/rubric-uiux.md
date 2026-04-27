# Review Rubric: UI/UX

## Purpose
Evaluate UI/UX decisions in CREATIVE phase for user experience quality.

## Scoring Dimensions

### Dimension 1: User Task Fit
| Score | Guidance |
|-------|----------|
| 0-3 | Design does not map to user workflows or mental models |
| 4-6 | Basic task completion possible but friction points exist |
| 7-9 | Smooth task completion with clear affordances |
| 10 | Anticipates user needs with delight-inducing shortcuts |

### Dimension 2: Consistency with Style Guide
| Score | Guidance |
|-------|----------|
| 0-3 | Ignores existing patterns; introduces conflicting conventions |
| 4-6 | Mostly follows style guide but has notable deviations |
| 7-9 | Consistent with style guide; extends patterns thoughtfully |
| 10 | Perfect alignment; sets new standard for future components |

### Dimension 3: Accessibility (A11y)
| Score | Guidance |
|-------|----------|
| 0-3 | Major accessibility barriers; fails basic WCAG criteria |
| 4-6 | Some a11y considered but gaps remain |
| 7-9 | Meets WCAG AA; keyboard and screen reader friendly |
| 10 | Exceeds standards; inclusive design as core principle |

### Dimension 4: Responsiveness
| Score | Guidance |
|-------|----------|
| 0-3 | Only works on single viewport size |
| 4-6 | Adapts to major breakpoints but edge cases break |
| 7-9 | Fluid layout across all target devices |
| 10 | Progressive enhancement with device-specific optimizations |

### Dimension 5: Feedback Clarity
| Score | Guidance |
|-------|----------|
| 0-3 | User cannot tell what happened after actions |
| 4-6 | Some feedback exists but ambiguous or delayed |
| 7-9 | Clear success/error states with actionable messages |
| 10 | Predictive feedback with loading states and undo options |

## AI-Slop Detectors
Flag the decision if ANY of the following appear:
- [ ] Generic buzzword salad without concrete trade-offs ("modern and intuitive")
- [ ] Options differ only in color/naming, not interaction substance
- [ ] "Best of both worlds" claim without acknowledging UX costs
- [ ] Missing accessibility consideration entirely
- [ ] Justification ignores stated style guide constraints

## Evaluation Output Block
After applying this rubric, append to the creative document:

```yaml
Rubric Review:
  rubric: rubric-uiux.md
  dimensions:
    user_task_fit: [0-10]
    style_guide_consistency: [0-10]
    accessibility: [0-10]
    responsiveness: [0-10]
    feedback_clarity: [0-10]
  ai_slop_flags: [list or "none"]
  verdict: [PASS | REVISE | REJECT]
  notes: [1-2 lines on highest-impact finding]
```
