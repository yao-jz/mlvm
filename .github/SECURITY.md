# Security Policy

## Current Status

MLVM is research-grade, pre-1.0 software. It has not received an independent security audit
and must not be used to secure funds, production rollups, or other high-value claims.

The fast emulator can still have correctness or determinism bugs. The Solidity and nested
proof components are experimental and do not form a complete dispute protocol.

## Reporting A Vulnerability

Please report security-sensitive issues through GitHub's private vulnerability reporting:

https://github.com/yao-jz/mlvm/security/advisories/new

Include:

- the affected commit or release;
- the smallest reproducing guest, state, witness, or contract call;
- the expected and observed state hashes;
- whether different hosts or implementations disagree;
- an impact assessment and any known workarounds.

Do not open a public issue for an unpatched vulnerability that could cause an invalid state
transition to be accepted, an honest transition to be rejected, or a committed input to be
interpreted inconsistently.

The maintainer will acknowledge a complete report on a best-effort basis, reproduce it, and
coordinate disclosure after a fix is available. No bug bounty is currently offered.

## Security-Critical Areas

Reports are especially valuable when they affect:

- state or witness encoding;
- Merkle proof construction or verification;
- RISC-V instruction semantics, rounding, NaNs, or exception flags;
- disagreement between fast, slow, and Solidity implementations;
- deterministic syscall or staged-input behavior;
- outer-to-nested dispute binding;
- untrusted ELF, JSON state, proof, or contract calldata parsing.

## Supported Versions

Before the first stable release, only the current `main` branch and most recent tagged
release receive security fixes. Compatibility and long-term support are not guaranteed.

