//! A Hugging Face language model generating text through Candle, inside MLVM.
//!
//! Loads a Llama-architecture checkpoint, such as HuggingFaceTB/SmolLM2-135M, with
//! candle-transformers' `llama::Llama`, tokenizes the prompt with the checkpoint's own
//! tokenizer.json (Hugging Face `tokenizers`), and generates greedily with a KV cache. The
//! request and the committed answer use the TinyStories decoder's layouts
//! (docs/DECODER-PROTOCOL.md), so the same tools read them: `mlvm xray` marks each token as
//! it is printed, and `mlvm liar --lie commit` can forge the committed token IDs.
//!
//! Inputs, staged under these names: config.json, tokenizer.json, model.safetensors,
//! request.bin.

use std::io::{Read, Write};

use anyhow::{bail, Context, Result};
use candle_core::{DType, Device, Tensor};
use candle_nn::VarBuilder;
use candle_transformers::models::llama::{Cache, Llama, LlamaConfig};
use tokenizers::Tokenizer;

const MAX_GEN_TOKENS: usize = 16;
const RESULT_VERSION: u16 = 1;

fn main() -> Result<()> {
    let device = Device::Cpu;
    let request = Request::read("request.bin")?;

    let config_json = std::fs::read_to_string("config.json").context("read config.json")?;
    let config: LlamaConfig = serde_json::from_str(&config_json)?;
    let eos = end_of_sequence(&config_json);
    let config = config.into_config(false);

    // Checkpoints with tie_word_embeddings, such as SmolLM2, store no lm_head: the output
    // projection is the embedding matrix. candle-transformers 0.4.1 always loads one.
    let mut weights = candle_core::safetensors::load("model.safetensors", &device)?;
    if !weights.contains_key("lm_head.weight") {
        let embed = weights
            .get("model.embed_tokens.weight")
            .context("no model.embed_tokens.weight in the checkpoint")?
            .clone();
        weights.insert("lm_head.weight".into(), embed);
    }
    let vb = VarBuilder::from_tensors(weights, DType::F32, &device);
    let model = Llama::load(vb, &config)?;
    let mut cache = Cache::new(true, DType::F32, &config, &device)?;

    let tokenizer = Tokenizer::from_file("tokenizer.json").map_err(anyhow::Error::msg)?;
    let prompt = String::from_utf8(request.prompt.clone()).context("prompt is not UTF-8")?;
    let mut tokens = tokenizer
        .encode(prompt.as_str(), true)
        .map_err(anyhow::Error::msg)?
        .get_ids()
        .to_vec();
    if tokens.is_empty() {
        bail!("empty prompt tokenization");
    }

    println!("MLVM decoder demo (Candle, Llama)");
    println!("prompt: {prompt}");
    print!("generated: ");
    std::io::stdout().flush()?;

    // The whole prompt in one forward pass, then one token at a time from the KV cache.
    let mut generated: Vec<u32> = Vec::new();
    let mut printed = 0; // bytes of the decoded continuation already printed
    let mut pos = 0;
    while generated.len() < request.max_new_tokens {
        let input = if generated.is_empty() { &tokens[..] } else { &tokens[tokens.len() - 1..] };
        let input = Tensor::new(input, &device)?.unsqueeze(0)?;
        let logits: Vec<f32> = model.forward(&input, pos, &mut cache)?.squeeze(0)?.to_vec1()?;
        pos += input.dim(1)?;
        let next = argmax(&logits) as u32;
        generated.push(next);
        tokens.push(next);
        // Byte-level BPE: a token can end inside a UTF-8 character, so print the decoded
        // text as it grows, one write per token, so `mlvm xray` can mark each one.
        let text = tokenizer.decode(&generated, true).map_err(anyhow::Error::msg)?;
        if text.len() > printed && text.is_char_boundary(printed) {
            print!("{}", &text[printed..]);
            printed = text.len();
        }
        std::io::stdout().flush()?;
        if Some(next) == eos {
            break;
        }
    }
    println!();

    // The decoder's result layout: u16 version | u16 count | u16 tokens[16].
    let mut answer = Vec::with_capacity(4 + 2 * MAX_GEN_TOKENS);
    answer.extend_from_slice(&RESULT_VERSION.to_le_bytes());
    answer.extend_from_slice(&(generated.len() as u16).to_le_bytes());
    for i in 0..MAX_GEN_TOKENS {
        let t = generated.get(i).copied().unwrap_or(0);
        if t > u16::MAX as u32 {
            bail!("token {t} does not fit the committed layout");
        }
        answer.extend_from_slice(&(t as u16).to_le_bytes());
    }
    commit_result(&answer);

    let ids: Vec<String> = generated.iter().map(|t| t.to_string()).collect();
    println!("tokens: {}", ids.join(" "));
    Ok(())
}

/// The first index of the largest value.
fn argmax(v: &[f32]) -> usize {
    let mut best = 0;
    for (i, &x) in v.iter().enumerate().skip(1) {
        if x > v[best] {
            best = i;
        }
    }
    best
}

/// config.json's eos_token_id, a number or a list of them (the first is used).
fn end_of_sequence(config_json: &str) -> Option<u32> {
    let v: serde_json::Value = serde_json::from_str(config_json).ok()?;
    let id = &v["eos_token_id"];
    id.as_u64().or_else(|| id.get(0).and_then(|x| x.as_u64())).map(|x| x as u32)
}

/// request.bin v1 (docs/DECODER-PROTOCOL.md).
struct Request {
    max_new_tokens: usize,
    prompt: Vec<u8>,
}

impl Request {
    fn read(path: &str) -> Result<Self> {
        let mut buf = [0u8; 276];
        std::fs::File::open(path)
            .with_context(|| format!("open {path}"))?
            .read_exact(&mut buf)?;
        let u32_at = |off: usize| u32::from_le_bytes(buf[off..off + 4].try_into().unwrap());
        if u32_at(0) != 1 {
            bail!("unsupported request version {}", u32_at(0));
        }
        if u32_at(8) != 0 {
            bail!("non-zero temperature is not implemented; use greedy (0)");
        }
        let len = u32_at(16) as usize;
        if len > 256 {
            bail!("prompt too long");
        }
        let mut max_new_tokens = u32_at(4) as usize;
        if max_new_tokens == 0 || max_new_tokens > MAX_GEN_TOKENS {
            max_new_tokens = MAX_GEN_TOKENS;
        }
        Ok(Self { max_new_tokens, prompt: buf[20..20 + len].to_vec() })
    }
}

/// Writes an answer into MLVM's result window, MLVM_OUT, in the layout of
/// examples/mlvm_result.h: magic, length, then up to 48 payload bytes.
#[cfg(target_arch = "riscv64")]
fn commit_result(payload: &[u8]) {
    const BASE: usize = 0x3100_0000_0000_0000;
    const MAGIC: u64 = 0x4D4C_564D_5F4F_5554; // "MLVM_OUT"
    const MAX: usize = 48;
    let len = payload.len().min(MAX);
    // SAFETY: inside MLVM every address is backed by the sparse memory, and nothing else
    // uses this window. Volatile, so the stores happen in this order.
    unsafe {
        let out = (BASE + 16) as *mut u8;
        for i in 0..MAX {
            std::ptr::write_volatile(out.add(i), if i < len { payload[i] } else { 0 });
        }
        let hdr = BASE as *mut u64;
        std::ptr::write_volatile(hdr.add(1), len as u64);
        std::ptr::write_volatile(hdr, MAGIC); // last, so a partial write is never valid
    }
}

/// A host build has no MLVM address space; its stdout is the comparison.
#[cfg(not(target_arch = "riscv64"))]
fn commit_result(_payload: &[u8]) {}
