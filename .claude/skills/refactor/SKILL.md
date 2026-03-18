---
name: refactor
description: |
  Post-commit refactoring of newly added code. Improves readability and performance
  without changing business logic. Run after all pre-commit checks pass.
---

# Post-Commit Refactoring

After all pre-commit checks pass, refactor the newly added code. DO NOT CHANGE BUSINESS LOGIC.

## Constraints

- Do not introduce external libraries not already in the project
- Do not change API signatures
- Do not change business logic

## Steps (follow in order)

1. **Analyze** — Review the current code and explain potential bottlenecks within the repo
2. **Strategize** — Describe the optimization strategy options you suggest
3. **Divide and conquer** — Break the work into smaller, manageable subtasks. Address one logical unit at a time, review and test the changes, then move to the next step
4. **Test** — Thoroughly test the changes. Review tests with tester persona. Make use of negative testing. Add new tests and/or update existing ones as needed to maintain or increase test coverage. Re-evaluate existing tests for duplication. Remove unused tests. Make use of boundary value analysis and equivalence partitioning

## Focus Areas

- Readability and performance
- Eliminate duplications, unused imports, and unused libraries
- Apply industry best practices
