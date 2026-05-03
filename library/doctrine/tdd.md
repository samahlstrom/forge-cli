# TDD Doctrine

Single source of truth for how tests are written across all forge-managed projects. All agents and pipeline scripts reference this file. Project-specific overrides may extend but not weaken these rules (declare them in the project's `forge.yaml` under `doctrine.tdd`).

## Sources

This doctrine synthesizes three converging teachings. Read them before challenging any rule below.

- Ian Cooper, "TDD: Where Did It All Go Wrong" — `https://www.youtube.com/watch?v=EZ05e7EMOLM`
- Matt Pocock, mattpocock/skills/engineering/tdd
- obra/superpowers, skills/test-driven-development

The single overarching principle: **tests verify what the system promises, not how it happens to do it.**

## System Under Test

The System Under Test (SUT) for behavior tests is the public API of a module — typically the application use case at its port boundary — not a class, method, hook, component, or function in isolation.

- Backend: the SUT is the application-layer use case invoked through its port.
- Frontend: the SUT is the rendered behavior observable through the user-facing affordance, not the React tree shape or component prop interface.
- Library code: the SUT is the package's public export.

The unit of isolation is the **test**, not the code under test. Tests must run independently and in any order. They are not isolated by mocking internal collaborators of the SUT.

## Trigger Rule

A new test is created because a new behavior or invariant is required.

A new test is **not** created because a new method, class, file, hook, or component was added.

If a method exists with no behavior tied to it, it does not need a dedicated test. If a behavior exists with no method tied to it, it still needs a test.

## Red-Green-Refactor Discipline

All behavior changes follow this cycle. The cycle runs as a vertical slice — one test, then the increment of code that satisfies it, then the next test. Writing all tests up front before any implementation ("horizontal slicing") is forbidden.

1. **Red.** Write one failing behavior test at the SUT's public surface. Run it. Confirm it fails for the intended reason — not for a typo, missing import, configuration error, or unrelated failure. The captured failure output is the **redline artifact**.
2. **Green.** Write the minimum code that makes the test pass. Speed beats elegance in this step. Speculative code, extra branches, defensive checks for impossible states, and "while I'm here" features are forbidden.
3. **Refactor.** Improve structure while all tests remain green. **No new tests are written during refactor.** A refactor that requires a new test is not a refactor — it is a new behavior, and it must restart the cycle at Red.

Refactoring is not allowed while any test is failing. Get to green first.

## Mocking Rules

Mocks exist to handle resources that cannot be used directly in a test, not to isolate internal code from itself.

**Mock only at true system boundaries:**

- outbound HTTP clients
- third-party SDKs
- payment processors, payer/provider APIs, healthcare/identity transports
- queue, storage, email, SMS, and push clients
- time, randomness, and other ambient sources of nondeterminism

**Never mock:**

- domain types or aggregates
- application use cases or handlers from another use case or handler in the same context
- ports between layers within the same bounded context
- repositories owned by the SUT (use a real test database or in-memory implementation of the port instead)
- any class, function, or module the team owns inside the same bounded context

When mocking is allowed, mock the complete data structure as it exists in production. Partial mocks that omit fields the production code does not currently read are forbidden — they break silently when the production code starts reading those fields.

Prefer SDK-style boundary interfaces (`payer.submitClaim(claim)`) over generic transports (`http.post(url, body)`). Each mock then returns one specific shape and contains no conditional logic in test setup.

## Banned Test Patterns

CI must block the following patterns. They are forbidden in all behavior tests and discouraged everywhere else.

- `toHaveBeenCalledWith`, `toHaveBeenCalled`, `toHaveBeenCalledTimes`, or any spy assertion targeting an internal collaborator. Behavior is verified by observing the SUT's outputs and persisted state, not by inspecting whether internal methods were called.
- Assertions on call order between internal collaborators.
- `describe('ClassName', () => describe('methodName', ...))` test scaffolding. Test scaffolding must read as a behavior specification (e.g. `describe('Recording vitals', () => it('rejects readings outside the configured safe range', ...))`).
- Bypassing the public interface to verify side effects. After a use case writes a record, the assertion reads that record back through the same interface or a published query contract — not through a direct database query in the test.
- Snapshot tests as the primary assertion for a behavior. Snapshots may supplement, never replace, behavior assertions.
- Tests that pass on first run with no observable failing state in the same branch when `behavior_change=true`. The redline artifact is mandatory.
- Tests asserting framework or library internals (e.g., asserting on the React fiber tree, ORM query builder shape, router metadata).

## Internals Visibility

Internals stay internal. No symbol may be promoted from private or module-internal to public, exported, or test-visible solely to enable testing. If a behavior is important enough to assert, it is observable through the public surface. If it is not observable through the public surface, it is an implementation detail and is not under test.

This rule applies to TypeScript `export`, ESM re-exports, internal module barrels, package-private boundaries, and any test-only injection of private state.

## Probe Tests

Tests written to investigate unfamiliar code, debug a defect, or feel out an implementation are allowed during development. They are not allowed in the merged history. Before merge, probe tests must be either deleted or promoted to behavior tests at the SUT's public surface that survive future refactors.

A merged test that exists only to document how today's implementation works is a future maintenance liability and is not accepted.

## Verification Before Completion

A change is not complete until:

1. The proof command set (typecheck, lint, test, plus any required adapter or browser smoke for the touched paths) has been re-run from a clean state on the final code.
2. The full output has been read.
3. The exit code is zero and there are no warnings introduced by the change.

A test run from earlier in the session does not satisfy this rule. Pristine output on the final state is the only acceptable evidence of completion.

## No Translation-Layer Frameworks for Behavior Changes

Behavior tests are written in the same language and test runner as the implementation. Cucumber, Gherkin, Fitnesse, and similar natural-language translation layers are not used as the primary behavior test surface. The customer conversation shapes the behavior tests; it does not become a separate test artifact.

This rule does not prohibit BDD-style naming inside the existing test runner. It prohibits the separate translation layer that historically accumulates broken, ignored, or unread acceptance suites.

## Test Naming

Test names describe behavior in the language a user, operator, or product owner would use.

Acceptable:

- "rejects readings outside the configured safe range"
- "creates an escalation when an assessment crosses the alert threshold"
- "rolls back the shift end when the audit write fails"

Unacceptable:

- "calls vitalsRepository.save once"
- "returns true"
- "VitalsService.recordReading"
- "should work"

## When the Doctrine Applies in Strict Form

The full doctrine — including the redline-test requirement — applies whenever the classifier sets `behavior_change=true`. This is set when the task description, files touched, or risk tier indicates the change alters how the system behaves (new feature, bug fix that changes outputs, new invariant, new state transition).

For pure styling, documentation, configuration, dependency bumps, and reformatting (`behavior_change=false`), the System Under Test, Mocking Rules, Banned Test Patterns, and Internals Visibility rules still apply, but the strict Red-Green-Refactor cycle and redline artifact are not required.

## Why This Doctrine Exists

Tests that describe implementation break on refactor and force teams to either freeze the implementation or delete the tests. Both outcomes destroy the value of the test suite. Tests that describe behavior survive any internal change that preserves the behavior, which is precisely the property that makes refactoring safe and the test suite a long-term asset.

The doctrine is not a stylistic preference. It is the mechanical condition under which any architecture's promise of "high changeability without broad breakage" can hold.
