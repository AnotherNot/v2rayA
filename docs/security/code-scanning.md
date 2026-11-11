# Code scanning and Dependabot: how an alert is closed

The queue exists to show real defects. An alert that stays open because nobody
decided anything makes the next real one invisible.

## Principles

1. A finding is closed by a fix, or by an explicit dismissal with a reason. An
   open alert with a comment next to it is still open.
2. Fix the root cause. A guard the analyzer cannot follow is still a guard, but
   it needs a test that fails when the guard is removed.
3. A suppression comment (`#nosec`, `// codeql[...]`) does not replace a
   dismissal and must not claim one. It explains a decision, it does not make
   it.
4. No blanket path excludes and no rule disabled for the repository.
   Suppression is per alert, with the reason recorded on the alert itself.
5. The queue is read after every push to the default branch. An alert that
   reappears after being dismissed is triaged again, not dismissed by habit.

## Choosing the reason

| Situation | Reason |
| --- | --- |
| The code is being changed | `fix started`, or nothing: the push closes it |
| The analyzer cannot see a guard that is in the code | `false positive`, naming the guard and its test |
| The path is not reachable from untrusted input, and a fix would remove a capability | `not used` |
| No fix exists, or the risk is accepted with a printed argument | `tolerable risk` |
| The behaviour is deliberate and the alternative breaks compatibility | `won't fix` |

A dismissal comment answers three questions in two or three sentences: what the
code does, why the reported flow cannot happen (or why the risk is accepted),
and what would change the verdict.

## Guarded flows

When a check sits far from the sink (a wrapper, an entry-point validation), the
comment says so and points at the test. Moving the check closer to the sink is
preferred when it is cheap: an analyzer-visible guard is also a
reviewer-visible guard.

## Dependencies

Fix with a version bump when one exists inside the declared ranges. When the fix
requires a major outside the parents' ranges, an override is allowed only after
the full build and test toolchain passes; otherwise the alert is dismissed as
accepted risk and revisited when a parent moves. A dependency that ships only in
the build toolchain and consumes no untrusted input is accepted risk, stated as
such, not "fixed" by pinning something else.

## Records

The decision lives on the alert. This document records the rules, not the list
of alerts.
