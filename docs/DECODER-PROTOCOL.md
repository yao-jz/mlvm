# Decoder Protocol

This document defines the committed request and result layout used by the TinyStories
Llama2-C decoder guest (`examples/decoder`). It is part of the `mlvm-1` execution profile.

## Artifacts

| Guest path | Host role | Commitment |
| --- | --- | --- |
| `model.bin` | llama2.c checkpoint (e.g. `stories260K.bin`) | staged-file bytes + filesystem root |
| `tokenizer.bin` | llama2.c tokenizer (e.g. `tok512.bin`) | staged-file bytes + filesystem root |
| `request.bin` | generation request | staged-file bytes + filesystem root |

A prestate is a pure function of the guest ELF, these three files, and the randomness policy.

## `request.bin` layout (276 bytes)

Little-endian fields:

| Offset | Type | Field |
| ---: | --- | --- |
| 0 | `u32` | `version` (must be `1`) |
| 4 | `u32` | `max_new_tokens` (1..16 for the demo profile) |
| 8 | `u32` | `temperature_milli` (`0` = greedy) |
| 12 | `u32` | reserved (`0`) |
| 16 | `u32` | `prompt_len` |
| 20 | `[256]u8` | UTF-8 prompt bytes, zero-padded |

The tokenizer runs **inside** the guest. Hosts must not pre-tokenize.

## `MLResult` payload (36 bytes used, ≤48)

Written at `MLVM_RESULT_BASE` via `mlvm_commit_result`:

| Offset | Type | Field |
| ---: | --- | --- |
| 0 | `u16` | result version (`1`) |
| 2 | `u16` | `count` (generated token count) |
| 4 | `[16]u16` | generated token IDs |

The final text is recovered by decoding those IDs with the same committed tokenizer.

## Settlement surface

- Integer, memory, and bitwise float transitions: `RISCV.sol` / slow Go, except syscalls
  they do not mirror (`getrandom` and several start-up stubs; see
  [SYSCALLS.md](SYSCALLS.md#coverage-outside-the-fast-vm)).
- Arithmetic `f32` transitions — most of this guest's float arithmetic: **no settlement route
  today.** `DecodeFloatSubGame` refuses single precision and no nested `f32` image exists;
  [STATUS.md](STATUS.md) lists this as experimental and incomplete.
- Arithmetic `f64` transitions: the experimental `f64` sub-game, round-to-nearest-even only.
  The decoder's trace contains `f64` instructions that use other rounding modes.
- Bonds, timeouts, and a complete interactive game are not implemented; see
  [STATUS.md](STATUS.md).
