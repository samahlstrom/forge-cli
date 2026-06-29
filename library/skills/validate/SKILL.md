---
name: validate
description: Validate that UI work actually accomplishes its mission — visually, in code, and behaviorally — before and after building. Builds a "shot list" of every state the change affects, captures each state live as a human-paced screenshot, and uploads the flow to Linear as the source of review truth. Use when editing components, adding information, building features, or improving pages — anything where an agent could hallucinate a result that looks done but isn't. Invoked with /validate.
user-invocable: true
---

# validate

> Agents hallucinate. They report work as done that is broken, half-wired, or
> only correct for one user in one state. You own the outcome. This skill makes
> the agent **prove** its work matches the mission — for every state the change
> touches — by capturing each state live and posting the flow to Linear.

## When to run

Run `/validate` whenever work changes what renders or how a flow behaves:
editing a component, adding fields, building a feature, fixing a page. It has
two phases — **Shot List** (before you code) and **Validate** (after, and
mid-flight). Do Phase 1 *before* writing implementation code.

---

## Phase 1 — Build the Shot List (before coding)

A **shot** is one specific state to capture: a (page × user persona ×
interaction state) combination the change affects. Read the feature request and
enumerate every shot it implies. This is the contract you will validate against.

1. **Decompose the request into states.** For each thing the feature does, ask:
   what does it look like *before*, *during*, and *after*? Who sees it? What
   changes per user type? Each distinct answer is a shot.
2. **Enumerate the personas the change distinguishes** — only the ones that
   actually differ for this change. Common axes: logged-out; logged-in
   non-privileged; logged-in privileged (e.g. `@healthtree.org` / admin). If the
   change behaves identically for all users, that's one shot, not three.
3. **Enumerate the lifecycle states.** Modals: closed → open → mid-input →
   submitted. Tickets/records: each status in the progress cycle (e.g. a
   Domino's-tracker: triage → started → … → done, plus closed/merged/canceled).
   Empty vs populated. Error vs success.
4. **Write the shot list** as a checklist. Each row: `persona | page | state |
   expected result`. Example for a bug-reporter feature:
   - `logged-out | any page | no bug icon shown`
   - `logged-in @ht | any page | red bug icon bottom-right`
   - `logged-in @ht | click icon | report modal open`
   - `logged-in @ht | after screenshot capture | image thumbnail present in modal`
   - `logged-in @ht | typing | text appears in report field`
   - `logged-in @ht | submitted | ticket appears in Domino's tracker`
   - `@ht admin | ticket lifecycle | one shot per status stage`
   - `@ht admin | ticket closed / merged / done | each terminal state`
   - `logged-in non-@ht | 404 page | gets modal; no icon elsewhere`
   - `logged-out | 404 page | "Found an error?" button absent`
5. **Post the shot list to Linear first**, as a comment on the feature's issue
   (see *Linear flow* below). This is the agreed scope before any code lands.

### The shot list is living

If requirements change mid-flight, or you discover a state the request didn't
name, **add it to the shot list** (and update the Linear comment). Never
silently drop or add scope — the list is the source of truth for "what done
looks like."

### Scope discipline (critical)

Capture **only the states this change impacts.** Do not fan out across
irrelevant surfaces — e.g. don't screenshot every disease page when the change
is a global widget; one representative page is the shot. If you find yourself
adding shots that the change can't affect, stop. Over-capturing hides the real
diff and wastes the reviewer's attention.

---

## Phase 2 — Validate each shot (after coding, against localhost)

Work on a **localhost branch** with the dev server running. For each shot:

1. **Set up the exact state.** Log in as the right persona, navigate to the
   page, perform the interaction. (See *Personas & auth*.)
2. **Wait like a human. This is mandatory.** After every `open`, reload, click,
   or anything that animates or lazy-renders, **wait ~2–3 seconds and let the
   view settle before you screenshot.** Capturing the instant the DOM exists is
   the #1 cause of false failures — you photograph a half-painted frame and
   wrongly report the feature broken. Confirm the target element is actually
   *visible* (not just present in the DOM) before the shot. Operate as slowly as
   the real user would.
3. **Validate three ways, not just visually:**
   - **Visual** — the screenshot matches the shot's expected result.
   - **Code/console** — no errors in the browser console; the conditionally-
     rendered element is genuinely gated correctly (e.g. the icon is *absent*
     for logged-out, not merely hidden off-screen).
   - **Behavioral** — the interaction does what it claims (submit creates the
     record, the status actually advances, the request fires).
4. **Compare actual vs intended.** If a shot doesn't match its expected result,
   that's a finding — fix it or flag it. Do not paper over a mismatch.
5. **Upload the captured screenshot to the Linear flow** (below), mapped to its
   shot with a caption.

---

## Visual Verification

Verify implemented features work correctly through actual user interaction, not
just automated tests. A passing test suite is not proof that the thing a user
sees is right.

### When to use

- After implementing any UI feature
- Before marking an issue complete
- When acceptance criteria involve user-visible behavior
- After fixing a UI bug

### What counts as "visually verifiable"

If it can be seen or accessed through a UI of any kind, it is visually
verifiable — and it must be proven with captured evidence, never asserted from
the code alone. Concretely: any change that reaches a page serving a
`+page.svelte`, where the result is a data point, a visible button, a state, or
**anything a user can see or interact with**. If your change can surface in the
browser, you owe a screenshot.

### Verification checklist

Before marking any UI task complete:

- [ ] Dev server is running and the page is reachable
- [ ] Feature renders with no console errors
- [ ] Elements render correctly and do not overlap incorrectly
- [ ] User interactions behave as expected
- [ ] Edge cases handled (empty, loading, error states)
- [ ] Screenshot captured as evidence and posted to the Linear card

### What NOT to do

- Skip visual verification because "the tests pass"
- Mark an issue complete without testing it in the browser
- Assume dev mode catches every error — run `npm run build` too
- Test only the happy path

---

## Personas & auth

A change is only validated when every persona it distinguishes is checked —
including the negative cases (icon **absent** when logged out).

- **Fast persona setup → Firebase admin custom token.** Mint a token and inject
  it (`page.addInitScript` before navigation) to land as a given user without
  touching the login UI. Use this for most shots — it's fast and deterministic.
- **Real login via stored credentials** — only when the sign-in / sign-out flow
  *itself* is under test, or the shot depends on the real auth UI. Drive the
  actual login form in the browser using a provisioned test account.
- **You need real test accounts to run hands-off.** At minimum: one privileged
  account (e.g. `@healthtree.org`/admin) and one non-privileged account, plus the
  logged-out state. Provision them once and store credentials where the project
  keeps test secrets (e.g. `playwright/.env`), never hardcoded in the skill or a
  commit. If accounts aren't available, say so and stop — don't fake the states.

Discover the project's own login mechanism, dev-server command, and element
selectors (`data-testid`) before driving — check the project's playwright/e2e
skill and config. This skill is portable; the project supplies the specifics.

---

## Linear flow (source of review truth)

The point is to make Linear the place the human reviews *how the flow is
supposed to work and every view*, so they can verify the work didn't break it.

- **Target:** a **comment on the feature's Linear issue** (one issue per
  feature). Post the shot list there in Phase 1; then, as shots validate, post
  the captured screenshots — each with a caption naming its persona + state — so
  the comment reads as the full intended flow.
- **Upload mechanism:** use the `linear` CLI's image support — `linear upload
  <file>` to get an asset URL, or `linear issues comment <id> "<body>" --attach
  <file>`. Compose one captioned comment per feature (or per flow stage) rather
  than scattering loose images.
- **Caption every image** with the shot it proves: `**logged-out — bug icon
  absent**`, `**@ht admin — ticket: started**`, etc. An uncaptioned screenshot
  is not a validation.

---

## Definition of done

`/validate` is complete only when:

- [ ] Every shot on the (possibly-updated) shot list has a captured, captioned
      screenshot in the Linear comment.
- [ ] Each shot was verified visually **and** for console errors **and**
      behaviorally — including negative/absent states.
- [ ] Every persona the change distinguishes was exercised.
- [ ] Any mismatch between actual and intended was fixed or explicitly flagged.
- [ ] Scope stayed tight — only impacted states, no irrelevant surfaces.

If you cannot complete a shot (missing account, can't reach a state), say so
plainly and stop. A gap reported honestly is recoverable; a hallucinated
"all validated" is the exact failure this skill exists to prevent.

### Write the validation receipt (PR-gate handshake)

Only after the Definition of Done above is genuinely met, write the receipt the
PR gate reads. `gh pr create` is hard-blocked until this receipt names the
current branch. The receipt is pipe-delimited; the commander audits it without
reading your transcript, so it must carry a **link back to the proof**:

```
<branch> | validated | <Linear issue id> | shots=<N> | <Linear comment URL> | <UTC timestamp>
```

- `shots=<N>` — the number of captured, captioned screenshots you posted.
- `<Linear comment URL>` — the exact comment holding the shot flow (the field
  the commander clicks to confirm those N screenshots are really there).

```bash
mkdir -p .claude && echo "$(git branch --show-current) | validated | <Linear issue id> | shots=<N> | <Linear comment URL> | $(date -u +%Y-%m-%dT%H:%M:%SZ)" > .claude/.validate-receipt
```

Writing this receipt without doing the work is the exact hallucination this
skill exists to prevent — the gate trusts the act, and the URL lets the
commander check the claim. A receipt whose URL has no screenshots behind it is a
fabrication. If the change genuinely touches no UI surface (nothing a user can
see or interact with, no page serving a `+page.svelte` reached), record that
instead, honestly:

```bash
mkdir -p .claude && echo "$(git branch --show-current) | skipped | no UI surface touched: <one-line reason> | $(date -u +%Y-%m-%dT%H:%M:%SZ)" > .claude/.validate-receipt
```
