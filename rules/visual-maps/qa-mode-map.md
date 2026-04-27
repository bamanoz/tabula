---
description: QA Mode
globs: qa-mode-map.mdc
alwaysApply: false
---


> **TL;DR:** This enhanced QA mode provides comprehensive validation at any stage of development. It automatically detects the current phase, validates Memory Bank consistency, verifies task tracking, and performs phase-specific technical validation to ensure project quality throughout the development lifecycle.

> **Note:** This document describes the legacy / anytime QA validation mode. It is **not** the Level 3 Phase 4.5 runtime-validation gate. For the post-BUILD Level 3 gate, use `4-5-qa` and the contracts in `rules/Phases/QAPhase/qa-phase-shared.md` plus `qa-phase-{visual,backend,deep}.md`. Phase 4.5 QA records evidence and routes failures back to BUILD; it does not apply source fixes itself.

## 🔍 ENHANCED QA MODE PROCESS FLOW

```mermaid
graph TD
    Start["🚀 START QA MODE"] --> DetectPhase["🧭 PHASE DETECTION<br>Determine current project phase"]
    
    %% Phase detection decision path
    DetectPhase --> PhaseDetermination{"Current Phase?"}
    PhaseDetermination -->|"VAN"| VANChecks["VAN Phase Validation"]
    PhaseDetermination -->|"PLAN"| PLANChecks["PLAN Phase Validation"]
    PhaseDetermination -->|"CREATIVE"| CREATIVEChecks["CREATIVE Phase Validation"] 
    PhaseDetermination -->|"BUILD"| BUILDChecks["BUILD Phase Validation"]
    
    %% Universal checks that apply to all phases
    DetectPhase --> UniversalChecks["🔍 UNIVERSAL VALIDATION"]
    UniversalChecks --> MemoryBankCheck["1️⃣ MEMORY BANK VERIFICATION<br>Check consistency & updates"]
    MemoryBankCheck --> TaskTrackingCheck["2️⃣ TASK TRACKING VERIFICATION<br>Validate tasks.md as source of truth"]
    TaskTrackingCheck --> ReferenceCheck["3️⃣ REFERENCE VALIDATION<br>Verify cross-references between docs"]
    
    %% Phase-specific validations feed into comprehensive report
    VANChecks & PLANChecks & CREATIVEChecks & BUILDChecks --> PhaseSpecificResults["Phase-Specific Results"]
    ReferenceCheck & PhaseSpecificResults --> ValidationResults{"✅ All Checks<br>Passed?"}
    
    %% Results Processing
    ValidationResults -->|"Yes"| SuccessReport["📝 GENERATE SUCCESS REPORT<br>All validations passed"]
    ValidationResults -->|"No"| FailureReport["⚠️ GENERATE FAILURE REPORT<br>With specific fix instructions"]
    
    %% Success Path
    SuccessReport --> UpdateMB["📚 Update Memory Bank<br>Record successful validation"]
    UpdateMB --> ContinueProcess["🚦 CONTINUE: Phase processes<br>can proceed"]
    
    %% Failure Path
    FailureReport --> IdentifyFixes["🔧 IDENTIFY REQUIRED FIXES"]
    IdentifyFixes --> ApplyFixes["🛠️ APPLY FIXES"]
    ApplyFixes --> Revalidate["🔄 Re-run validation"]
    Revalidate --> ValidationResults
    
    %% Style nodes for clarity
    style Start fill:#4da6ff,stroke:#0066cc,color:white
    style DetectPhase fill:#f6ad55,stroke:#c27022,color:white
    style UniversalChecks fill:#f6546a,stroke:#c30052,color:white
    style MemoryBankCheck fill:#10b981,stroke:#059669,color:white
    style TaskTrackingCheck fill:#10b981,stroke:#059669,color:white
    style ReferenceCheck fill:#10b981,stroke:#059669,color:white
    style ValidationResults fill:#f6546a,stroke:#c30052,color:white
    style SuccessReport fill:#10b981,stroke:#059669,color:white
    style FailureReport fill:#f6ad55,stroke:#c27022,color:white
    style ContinueProcess fill:#10b981,stroke:#059669,color:white,stroke-width:2px
    style IdentifyFixes fill:#f6ad55,stroke:#c27022,color:white
```

## 🧭 PHASE DETECTION PROCESS

The enhanced QA mode first determines which phase the project is currently in:

```mermaid
graph TD
    PD["Phase Detection"] --> CheckMB["Analyze Memory Bank Files"]
    CheckMB --> CheckActive["Check activeContext.md<br>for current phase"]
    CheckActive --> CheckProgress["Check progress.md<br>for recent activities"]
    CheckProgress --> CheckTasks["Check tasks.md<br>for task status"]
    
    CheckTasks --> PhaseResult{"Determine<br>Current Phase"}
    PhaseResult -->|"VAN"| VAN["VAN Phase<br>Initialization"]
    PhaseResult -->|"PLAN"| PLAN["PLAN Phase<br>Task Planning"]
    PhaseResult -->|"CREATIVE"| CREATIVE["CREATIVE Phase<br>Design Decisions"]
    PhaseResult -->|"BUILD"| BUILD["BUILD Phase<br>Implementation"]
    
    VAN & PLAN & CREATIVE & BUILD --> LoadChecks["Load Phase-Specific<br>Validation Checks"]
    
    style PD fill:#4da6ff,stroke:#0066cc,color:white
    style PhaseResult fill:#f6546a,stroke:#c30052,color:white
    style LoadChecks fill:#10b981,stroke:#059669,color:white
```

## 📝 UNIVERSAL MEMORY BANK VERIFICATION

This process ensures Memory Bank files are consistent and up-to-date regardless of phase:

```mermaid
graph TD
    MBVS["Memory Bank<br>Verification"] --> CoreCheck["Check Core Files Exist"]
    CoreCheck --> CoreFiles["Verify Required Files:<br>projectbrief.md<br>activeContext.md<br>tasks.md<br>progress.md"]
    
    CoreFiles --> ContentCheck["Verify Content<br>Consistency"]
    ContentCheck --> LastModified["Check Last Modified<br>Timestamps"]
    LastModified --> CrossRef["Validate Cross-<br>References"]
    
    CrossRef --> ConsistencyCheck{"All Files<br>Consistent?"}
    ConsistencyCheck -->|"Yes"| PassMB["✅ Memory Bank<br>Verification Passed"]
    ConsistencyCheck -->|"No"| FailMB["❌ Memory Bank<br>Inconsistencies Found"]
    
    FailMB --> FixSuggestions["Generate Fix<br>Suggestions"]
    
    style MBVS fill:#4da6ff,stroke:#0066cc,color:white
    style ConsistencyCheck fill:#f6546a,stroke:#c30052,color:white
    style PassMB fill:#10b981,stroke:#059669,color:white
    style FailMB fill:#ff5555,stroke:#dd3333,color:white
```

## 📋 TASK TRACKING VERIFICATION

This process validates tasks.md as the single source of truth:

```mermaid
graph TD
    TTV["Task Tracking<br>Verification"] --> CheckTasksFile["Check tasks.md<br>Existence & Format"]
    CheckTasksFile --> VerifyReferences["Verify Task References<br>in Other Documents"]
    VerifyReferences --> ProgressCheck["Check Consistency with<br>progress.md"]
    ProgressCheck --> StatusCheck["Verify Task Status<br>Accuracy"]
    
    StatusCheck --> TaskConsistency{"Tasks Properly<br>Tracked?"}
    TaskConsistency -->|"Yes"| PassTasks["✅ Task Tracking<br>Verification Passed"]
    TaskConsistency -->|"No"| FailTasks["❌ Task Tracking<br>Issues Found"]
    
    FailTasks --> TaskFixSuggestions["Generate Task Tracking<br>Fix Suggestions"]
    
    style TTV fill:#4da6ff,stroke:#0066cc,color:white
    style TaskConsistency fill:#f6546a,stroke:#c30052,color:white
    style PassTasks fill:#10b981,stroke:#059669,color:white
    style FailTasks fill:#ff5555,stroke:#dd3333,color:white
```

## 🔄 REFERENCE VALIDATION PROCESS

This process ensures proper cross-referencing between documents:

```mermaid
graph TD
    RV["Reference<br>Validation"] --> FindRefs["Find Cross-References<br>in Documents"]
    FindRefs --> VerifyRefs["Verify Reference<br>Accuracy"]
    VerifyRefs --> CheckBackRefs["Check Bidirectional<br>References"]
    
    CheckBackRefs --> RefConsistency{"References<br>Consistent?"}
    RefConsistency -->|"Yes"| PassRefs["✅ Reference Validation<br>Passed"]
    RefConsistency -->|"No"| FailRefs["❌ Reference<br>Issues Found"]
    
    FailRefs --> RefFixSuggestions["Generate Reference<br>Fix Suggestions"]
    
    style RV fill:#4da6ff,stroke:#0066cc,color:white
    style RefConsistency fill:#f6546a,stroke:#c30052,color:white
    style PassRefs fill:#10b981,stroke:#059669,color:white
    style FailRefs fill:#ff5555,stroke:#dd3333,color:white
```

## 🚨 PHASE-SPECIFIC VALIDATION PROCESSES

### VAN Phase Validation

```mermaid
graph TD
    VAN["VAN Phase<br>Validation"] --> InitCheck["Check Initialization<br>Completeness"]
    InitCheck --> PlatformCheck["Verify Platform<br>Detection"]
    PlatformCheck --> ComplexityCheck["Validate Complexity<br>Determination"]
    
    ComplexityCheck --> VANConsistency{"VAN Phase<br>Complete?"}
    VANConsistency -->|"Yes"| PassVAN["✅ VAN Phase<br>Validation Passed"]
    VANConsistency -->|"No"| FailVAN["❌ VAN Phase<br>Issues Found"]
    
    style VAN fill:#4da6ff,stroke:#0066cc,color:white
    style VANConsistency fill:#f6546a,stroke:#c30052,color:white
    style PassVAN fill:#10b981,stroke:#059669,color:white
    style FailVAN fill:#ff5555,stroke:#dd3333,color:white
```

### PLAN Phase Validation

```mermaid
graph TD
    PLAN["PLAN Phase<br>Validation"] --> PlanCheck["Check Planning<br>Documentation"]
    PlanCheck --> TaskBreakdown["Verify Task<br>Breakdown"]
    TaskBreakdown --> ScopeCheck["Validate Scope<br>Definition"]
    
    ScopeCheck --> PLANConsistency{"PLAN Phase<br>Complete?"}
    PLANConsistency -->|"Yes"| PassPLAN["✅ PLAN Phase<br>Validation Passed"]
    PLANConsistency -->|"No"| FailPLAN["❌ PLAN Phase<br>Issues Found"]
    
    style PLAN fill:#4da6ff,stroke:#0066cc,color:white
    style PLANConsistency fill:#f6546a,stroke:#c30052,color:white
    style PassPLAN fill:#10b981,stroke:#059669,color:white
    style FailPLAN fill:#ff5555,stroke:#dd3333,color:white
```

### CREATIVE Phase Validation

```mermaid
graph TD
    CREATIVE["CREATIVE Phase<br>Validation"] --> DesignCheck["Check Design<br>Documents"]
    DesignCheck --> ArchCheck["Verify Architectural<br>Decisions"]
    ArchCheck --> PatternCheck["Validate Design<br>Patterns"]
    
    PatternCheck --> CREATIVEConsistency{"CREATIVE Phase<br>Complete?"}
    CREATIVEConsistency -->|"Yes"| PassCREATIVE["✅ CREATIVE Phase<br>Validation Passed"]
    CREATIVEConsistency -->|"No"| FailCREATIVE["❌ CREATIVE Phase<br>Issues Found"]
    
    style CREATIVE fill:#4da6ff,stroke:#0066cc,color:white
    style CREATIVEConsistency fill:#f6546a,stroke:#c30052,color:white
    style PassCREATIVE fill:#10b981,stroke:#059669,color:white
    style FailCREATIVE fill:#ff5555,stroke:#dd3333,color:white
```

### BUILD Phase Technical Validation

This retains the original QA validation from the previous version:

```mermaid
graph TD
    BUILD["BUILD Phase<br>Validation"] --> ReadDesign["Read Design Decisions"]
    ReadDesign --> FourChecks["Four-Point Technical<br>Validation"]
    
    FourChecks --> DepCheck["1️⃣ Dependency<br>Verification"]
    DepCheck --> ConfigCheck["2️⃣ Configuration<br>Validation"]
    ConfigCheck --> EnvCheck["3️⃣ Environment<br>Validation"]
    EnvCheck --> MinBuildCheck["4️⃣ Minimal Build<br>Test"]
    
    MinBuildCheck --> BUILDConsistency{"Technical<br>Prerequisites Met?"}
    BUILDConsistency -->|"Yes"| PassBUILD["✅ BUILD Phase<br>Validation Passed"]
    BUILDConsistency -->|"No"| FailBUILD["❌ BUILD Phase<br>Issues Found"]
    
    style BUILD fill:#4da6ff,stroke:#0066cc,color:white
    style FourChecks fill:#f6546a,stroke:#c30052,color:white
    style BUILDConsistency fill:#f6546a,stroke:#c30052,color:white
    style PassBUILD fill:#10b981,stroke:#059669,color:white
    style FailBUILD fill:#ff5555,stroke:#dd3333,color:white
```

## 📋 UNIVERSAL VALIDATION COMMAND EXECUTION

### Memory Bank Verification Commands:

```bash
# Check Memory Bank file existence and recency
ls -la memory-bank/
find memory-bank/ -type f -mtime -7 | sort

# Check for consistency between files
grep -r "task" memory-bank/
grep -r "requirement" memory-bank/
```

### Task Tracking Verification Commands:

```bash
# Verify tasks.md as source of truth
test -f tasks.md && echo "✅ tasks.md exists" || echo "❌ tasks.md missing"

# Check references to tasks in other files
grep -r "Task" --include="*.md" .
grep -r "task" --include="*.md" . | grep -v "tasks.md" | wc -l

# Verify task status consistency
grep -i "completed\|done\|finished" tasks.md
grep -i "in progress\|started" tasks.md
```

### Reference Validation Commands:

```bash
# Find cross-references between files
grep -r "see\|refer\|reference" --include="*.md" .

# Check for broken references
for file in $(grep -l "see\|refer\|reference" --include="*.md" .); do
  for ref in $(grep -o '[a-zA-Z0-9_-]*\.md' $file); do
    test -f $ref || echo "❌ Broken reference: $ref in $file"
  done
done
```

## 📋 1️⃣ DEPENDENCY VERIFICATION PROCESS (Original)

This validation point ensures all required packages are correctly installed.

### Command Execution:

```bash
# Check if packages are installed
npm list react react-dom tailwindcss postcss autoprefixer

# Verify package versions match requirements
npm list | grep -E "react|tailwind|postcss"

# Check for peer dependency warnings
npm ls --depth=0
```

### Validation Criteria:
- All required packages must be installed
- Versions must be compatible with requirements
- No critical peer dependency warnings
- Required dev dependencies must be present

### Common Fixes:
- `npm install [missing-package]` - Install missing packages
- `npm install [package]@[version]` - Fix version mismatches
- `npm install --save-dev [dev-dependency]` - Add development dependencies

## 📝 2️⃣ CONFIGURATION VALIDATION PROCESS (Original)

This validation point ensures configuration files are in the correct format for the project.

### Command Execution:

```bash
# Check package.json for module type
grep "\"type\":" package.json

# Verify configuration file extensions match module type
find . -name "*.config.*" | grep -E "\.(js|cjs|mjs)$"

# Test configuration syntax
node -c *.config.js || node -c *.config.cjs || node -c *.config.mjs
```

### Validation Criteria:
- Configuration file extensions must match module type in package.json
- File syntax must be valid
- Configuration must reference installed packages

### Common Fixes:
- Rename `.js` to `.cjs` for CommonJS in ES module projects
- Fix syntax errors in configuration files
- Adjust configuration to reference installed packages

## 🌐 3️⃣ ENVIRONMENT VALIDATION PROCESS (Original)

This validation point ensures the development environment is correctly set up.

### Command Execution:

```bash
# Check build tools 
npm run --help

# Verify node version compatibility
node -v

# Check for environment variables
printenv | grep -E "NODE_|PATH|HOME"

# Verify access permissions
ls -la .
```

### Validation Criteria:
- Node.js version must be compatible with requirements
- Build commands must be defined in package.json
- Environment must have necessary access permissions
- Required environment variables must be set

### Common Fixes:
- Update Node.js version
- Add missing scripts to package.json
- Fix file permissions with chmod/icacls
- Set required environment variables

## 🔥 4️⃣ MINIMAL BUILD TEST PROCESS (Original)

This validation point tests a minimal build to ensure basic functionality works.

### Command Execution:

```bash
# Run a minimal build
npm run build -- --dry-run || npm run dev -- --dry-run

# Test entry point file existence
find src -name "main.*" -o -name "index.*"

# Validate HTML entry point
grep -i "script.*src=" index.html
```

### Validation Criteria:
- Build process must complete without errors
- Entry point files must exist and be correctly referenced
- HTML must reference the correct JavaScript entry point
- Basic rendering must work in a test environment

### Common Fixes:
- Fix entry point references in HTML
- Correct import paths in JavaScript
- Fix build configuration errors
- Update incorrect paths or references

## 📊 ENHANCED COMPREHENSIVE QA REPORT FORMAT

```
╔═════════════════════════ 🔍 ENHANCED QA VALIDATION REPORT ═════════════════════╗
│                                                                               │
│ Project: [Project Name]               Date: [Current Date]                    │
│ Platform: [OS Platform]               Detected Phase: [Current Phase]         │
│                                                                               │
│ ━━━━━━━━━━━━━━━━━━━━━━━━ UNIVERSAL VALIDATION RESULTS ━━━━━━━━━━━━━━━━━━━━━━━ │
│                                                                               │
│ 1️⃣ MEMORY BANK VERIFICATION                                                   │
│    ✓ Core Files: [Status]                                                     │
│    ✓ Content Consistency: [Status]                                            │
│    ✓ Last Modified: [Status]                                                  │
│                                                                               │
│ 2️⃣ TASK TRACKING VERIFICATION                                                 │
│    ✓ tasks.md Status: [Status]                                                │
│    ✓ Task References: [Status]                                                │
│    ✓ Status Consistency: [Status]                                             │
│                                                                               │
│ 3️⃣ REFERENCE VALIDATION                                                       │
│    ✓ Cross-References: [Status]                                               │
│    ✓ Reference Accuracy: [Status]                                             │
│                                                                               │
│ ━━━━━━━━━━━━━━━━━━━━━━━ PHASE-SPECIFIC VALIDATION ━━━━━━━━━━━━━━━━━━━━━━━━━━━ │
│                                                                               │
│ [VAN/PLAN/CREATIVE/BUILD] PHASE VALIDATION                                    │
│    ✓ [Phase-specific check 1]: [Status]                                       │
│    ✓ [Phase-specific check 2]: [Status]                                       │
│    ✓ [Phase-specific check 3]: [Status]                                       │
│                                                                               │
│ [Technical validation section shown only for BUILD phase]                      │
│                                                                               │
│ ━━━━━━━━━━━━━━━━━━━━━━━━━━━ OVERALL STATUS ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ │
│                                                                               │
│ ✅ VALIDATION PASSED - Project quality verified for current phase              │
│                                                                               │
╚═══════════════════════════════════════════════════════════════════════════════╝
```

## 🚫 ENHANCED FAILURE REPORT FORMAT

If validation fails, a detailed failure report is generated:

```
╔═════════════════════════ ⚠️ QA VALIDATION FAILURES ═════════════════════════════╗
│                                                                                 │
│ Project: [Project Name]               Date: [Current Date]                      │
│ Platform: [OS Platform]               Detected Phase: [Current Phase]           │
│                                                                                 │
│ ━━━━━━━━━━━━━━━━━━━━━━━━━━ FAILED CHECKS ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ │
│                                                                                 │
│ ❌ MEMORY BANK ISSUES                                                           │
│    • [Specific issue details]                                                   │
│    • [Specific issue details]                                                   │
│                                                                                 │
│ ❌ TASK TRACKING ISSUES                                                         │
│    • [Specific issue details]                                                   │
│    • [Specific issue details]                                                   │
│                                                                                 │
│ ❌ REFERENCE ISSUES                                                             │
│    • [Specific issue details]                                                   │
│    • [Specific issue details]                                                   │
│                                                                                 │
│ ❌ [PHASE]-SPECIFIC ISSUES                                                      │
│    • [Specific issue details]                                                   │
│    • [Specific issue details]                                                   │
│                                                                                 │
│ ━━━━━━━━━━━━━━━━━━━━━━━━━━━ REQUIRED FIXES ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ │
│                                                                                 │
│ 1. [Specific fix instruction with command]                                      │
│ 2. [Specific fix instruction with command]                                      │
│ 3. [Specific fix instruction with command]                                      │
│                                                                                 │
│ ⚠️ VALIDATION FAILED - Please resolve issues before proceeding                  │
│                                                                                 │
╚═════════════════════════════════════════════════════════════════════════════════╝
```

## 🔄 QA-ANYTIME ACTIVATION PROTOCOL

The enhanced QA mode can be activated at any time in the development process:

```mermaid
graph TD
    Start["User Types: QA"] --> DetectContext["Detect Current Context"]
    DetectContext --> RunQA["Run QA with Context-Aware Checks"]
    RunQA --> GenerateReport["Generate Appropriate QA Report"]
    GenerateReport --> UserResponse["Present Report to User"]
    
    UserResponse --> FixNeeded{"Fixes<br>Needed?"}
    FixNeeded -->|"Yes"| SuggestFixes["Display Fix Instructions"]
    FixNeeded -->|"No"| ContinueWork["Continue Current Phase Work"]
    
    style Start fill:#4da6ff,stroke:#0066cc,color:white
    style FixNeeded fill:#f6546a,stroke:#c30052,color:white
    style SuggestFixes fill:#ff5555,stroke:#dd3333,color:white
    style ContinueWork fill:#10b981,stroke:#059669,color:white
```

This enhanced QA mode serves as a "quality guardian" throughout the development process, ensuring documentation is consistently maintained and all phase requirements are met before proceeding to the next phase.
