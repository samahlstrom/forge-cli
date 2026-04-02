---
name: evaluate
description: Evaluate whether a pattern, structure, or approach is right for our codebase before building. Use when unsure how to structure something, when learning a new concept, or when you need to understand WHY something should be done a certain way. Invoked with /evaluate.
user-invocable: true
---

## Purpose

This skill is invoked when the user (or agent) is unsure whether an approach, pattern, or structure is correct for the codebase. It replaces "just build it and hope" with a structured evaluation.

The user is a prompt engineer, not a traditional coder. The goal is to surface the RIGHT questions and present answers in terms of tradeoffs, not implementation details.

---

## The Evaluation Protocol

When this skill is invoked, run through these levels IN ORDER. Do not skip levels. Present findings to the user at each level before proceeding.

### Level 1: Name the Pain

Before evaluating a solution, identify what problem it solves.

Ask the user (or determine from context):
- **What is hard right now?** (e.g., "changing the enrichment step breaks the firestore upload")
- **What triggered this?** (e.g., "senior engineer recommended X", "read about Y", "something broke")
- **What would success look like?** (e.g., "I can swap the enrichment step without touching anything else")

Output: A one-sentence problem statement. Example: "The pipeline stages are coupled through implicit JSON shapes, so changing one stage breaks downstream stages silently."

### Level 2: Find the Principle

Map the pain to a named software principle. This gives the user vocabulary to research further and communicate with engineers.

| Pain | Principle | One-liner |
|------|-----------|-----------|
| Changing one thing breaks other things | **Loose Coupling** | Modules should depend on each other as little as possible |
| A file does too many things | **Single Responsibility** | Each module does one thing |
| You need to read 10 files to understand 1 | **Locality of Reasoning** | You should understand code without holding the whole system in your head |
| The same logic is copy-pasted everywhere | **DRY** | Single source of truth for each piece of knowledge |
| Internal details leak to consumers | **Encapsulation** | Hide implementation, expose only what's necessary |
| Replacing a component requires rewriting consumers | **Dependency Inversion** | Depend on abstractions, not concrete implementations |
| Data shape is assumed, not validated | **Design by Contract** | Explicit input/output contracts at every boundary |
| You built something you don't need yet | **YAGNI** | Don't build for hypothetical future requirements |
| Too many abstraction layers to trace through | **KISS / Accidental Complexity** | The layers exist for the code, not for humans |

Output: The named principle + a plain-language explanation of why it applies here.

### Level 3: Survey the Codebase

Before recommending a structure, find out how the codebase ALREADY handles this pattern.

1. **Search for precedent**: Find 2-3 examples of how similar problems are solved in the existing codebase
2. **Search for anti-precedent**: Find examples where this problem is NOT solved well (to show the contrast)
3. **Check AGENTS.md**: Does the project already have a convention for this?
4. **Check types/**: Is there already a type definition that covers this domain?

Output: "Here's how we do it now" + "Here's where we do it badly" + "Here's what the project conventions say"

### Level 4: Present Options (Not One Answer)

Always present at least 2 approaches. For each:

```
Approach: [Name]
How it works: [1-2 sentences, no jargon]
Example from our codebase: [file path if one exists, or "no precedent"]
What it couples you to: [what becomes hard to change]
What it makes easy: [what becomes easy to change]
Effort: [small / medium / large]
Risk: [what breaks if you get it wrong]
```

### Level 5: Make a Recommendation

After presenting options, recommend one. Explain:
- **Why this one**: Which tradeoff matters most for our situation
- **What to watch for**: The failure mode of this approach
- **When to revisit**: Under what conditions this choice should be re-evaluated

---

## How to Invoke

The user can invoke this skill in several ways:

- `/evaluate` — then describe what you're unsure about
- `/evaluate firebase functions structure` — evaluate a specific topic
- `/evaluate should we use Zod here?` — evaluate a specific tool/library choice
- `/evaluate [senior engineer] recommended X` — evaluate a recommendation

## Examples

**User**: `/evaluate` I need to add a new data source to the NPPES pipeline
**Agent runs**: Level 1 (what's the pain?) → Level 2 (loose coupling, design by contract) → Level 3 (looks at current pipeline scripts, finds implicit JSON contracts) → Level 4 (Option A: add inline, Option B: typed stage interface, Option C: Zod validated pipeline) → Level 5 (recommends B or C based on codebase maturity)

**User**: `/evaluate` my engineer says we should use a repository pattern for Firestore
**Agent runs**: Level 1 (what pain does repository pattern solve?) → Level 2 (dependency inversion, encapsulation) → Level 3 (finds 153 direct firestore imports, shows the coupling) → Level 4 (Option A: keep current, Option B: repository pattern, Option C: query builder wrapper) → Level 5 (recommends based on migration cost vs benefit)

**User**: `/evaluate` is this component structured correctly?
**Agent runs**: Level 1 (reads the component, identifies responsibilities) → Level 2 (SRP, cohesion) → Level 3 (compares to similar components in codebase) → Level 4 (current structure vs split alternatives) → Level 5 (recommends based on reuse needs)

---

## Key Rule

**Never jump to "here's how to build it."** The entire point of this skill is to slow down and evaluate BEFORE building. If the user asks to build after evaluation, that's a separate task.