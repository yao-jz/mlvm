# Decoder Result v1

See also [docs/DECODER-PROTOCOL.md](../docs/DECODER-PROTOCOL.md).

Request (`request.bin`): 276 bytes, little-endian, version `1`.
`max_new_tokens` is 1..16 for the public demo profile.
Result payload: `u16 version | u16 count | u16 tokens[16]`, committed at `MLVM_RESULT_BASE`.
