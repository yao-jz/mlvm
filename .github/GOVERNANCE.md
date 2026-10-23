# Governance

MLVM currently uses a maintainer-led governance model. The repository owner is responsible
for releases, security coordination, project scope, and final merge decisions.

## Decision Process

Routine fixes and documentation changes are decided through pull-request review. Changes to
machine state, witness formats, instruction semantics, syscall behavior, or contract trust
boundaries should begin with an issue that records:

1. the user or protocol problem;
2. the proposed observable behavior;
3. determinism and compatibility implications;
4. how fast, slow, and Solidity implementations will be tested;
5. alternatives considered.

The maintainer may request a short design document under `docs/design/` for changes that are
difficult to reverse. Decisions favor correctness, explicit limitations, reproducibility,
and a small consensus-critical surface over feature count.

## Maintainer Responsibilities

- Keep the public status matrix aligned with the implementation.
- Require reproducible evidence for performance or compatibility claims.
- Triage security reports privately and disclose fixes responsibly.
- Publish release notes, checksums, and compatibility changes.
- Attribute upstream code and contributor work accurately.
- Avoid presenting experimental components as production-ready.

## Becoming A Maintainer

Regular contributors may be invited after demonstrating sustained technical judgment,
constructive review, and ownership of an area across multiple releases. Maintainer access is
granted by the existing maintainer and should be scoped to the responsibilities the person
has already been exercising publicly.

## Project Changes

This governance model is intentionally lightweight. If the project gains multiple active
maintainers, this document should be revised to define voting, release authority, inactivity,
and conflict-resolution procedures before those mechanisms are needed.

