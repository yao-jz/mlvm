//! TinyStories-260K generating text through Hugging Face Candle, inside MLVM.
//!
//! The same model, tokenizer and request as the C decoder in examples/decoder, the guest
//! `mlvm demo` runs, but every tensor operation goes through Candle: the llama2.c
//! checkpoint is loaded into candle-transformers' `llama2_c::Llama` and tokens are generated
//! greedily with its KV cache. The prompt is tokenized and the answer committed exactly as
//! the C decoder does (docs/DECODER-PROTOCOL.md), so the two programs can be compared token
//! for token, and `mlvm liar --lie commit` can forge the answer.
//!
//! Inputs, staged under these names: model.bin, tokenizer.bin, request.bin.

use std::collections::HashMap;
use std::io::{Read, Write};

use anyhow::{bail, Context, Result};
use candle_core::{DType, Device, IndexOp, Tensor};
use candle_nn::VarBuilder;
use candle_transformers::models::llama2_c::{Cache, Config, Llama};

const MAX_GEN_TOKENS: usize = 16;
const RESULT_VERSION: u16 = 1;
const BOS: usize = 1;
const EOS: usize = 2;

fn main() -> Result<()> {
    let device = Device::Cpu;
    let request = Request::read("request.bin")?;

    let mut model_file = std::fs::File::open("model.bin").context("open model.bin")?;
    let config = Config::from_reader(&mut model_file)?;
    let vb = read_llama2c_weights(&mut model_file, &config, &device)?;
    let model = Llama::load(vb.clone(), config.clone())?;
    let mut cache = Cache::new(true, &config, vb.pp("rot"))?;

    let tokenizer = Tokenizer::read("tokenizer.bin", config.vocab_size)?;
    let prompt_tokens = tokenizer.encode(&request.prompt)?;

    println!("MLVM decoder demo (Candle)");
    println!("prompt: {}", String::from_utf8_lossy(&request.prompt));
    print!("generated: ");
    std::io::stdout().flush()?;

    // The C decoder's loop: feed the prompt one token at a time, then pick the most likely
    // next token until max_new_tokens or end of sequence.
    let mut generated = Vec::new();
    let mut token = prompt_tokens[0];
    let mut pos = 0;
    while pos < config.seq_len {
        let input = Tensor::new(&[token as u32], &device)?.unsqueeze(0)?;
        let logits = model.forward(&input, pos, &mut cache)?;
        let next = if pos + 1 < prompt_tokens.len() {
            prompt_tokens[pos + 1]
        } else {
            let logits: Vec<f32> = logits.i((0, logits.dim(1)? - 1))?.to_vec1()?;
            let next = argmax(&logits);
            generated.push(next);
            // One write per token, so `mlvm xray` can mark where each one is printed.
            print!("{}", String::from_utf8_lossy(&tokenizer.decode(token, next)));
            std::io::stdout().flush()?;
            if generated.len() >= request.max_new_tokens || next == EOS {
                break;
            }
            next
        };
        token = next;
        pos += 1;
    }
    println!();

    // The decoder's result layout: u16 version | u16 count | u16 tokens[16].
    let mut answer = Vec::with_capacity(4 + 2 * MAX_GEN_TOKENS);
    answer.extend_from_slice(&RESULT_VERSION.to_le_bytes());
    answer.extend_from_slice(&(generated.len() as u16).to_le_bytes());
    for i in 0..MAX_GEN_TOKENS {
        let t = generated.get(i).copied().unwrap_or(0) as u16;
        answer.extend_from_slice(&t.to_le_bytes());
    }
    commit_result(&answer);

    let ids: Vec<String> = generated.iter().map(|t| t.to_string()).collect();
    println!("tokens: {}", ids.join(" "));
    Ok(())
}

/// The first index of the largest value, as the C decoder's argmax picks it.
fn argmax(v: &[f32]) -> usize {
    let mut best = 0;
    for (i, &x) in v.iter().enumerate().skip(1) {
        if x > v[best] {
            best = i;
        }
    }
    best
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

/// Reads llama2.c checkpoint weights into the names `llama2_c::Llama` expects.
///
/// candle-transformers 0.4.1 has a reader for this format, but it assumes as many key and
/// value heads as query heads and reads the wrong number of floats for a grouped-query
/// checkpoint such as TinyStories-260K (8 heads, 4 key/value heads). The model itself
/// handles the grouping. This reader follows the file layout in llama2.c's run.c.
fn read_llama2c_weights(r: &mut impl Read, c: &Config, dev: &Device) -> Result<VarBuilder<'static>> {
    let mut read = |dims: &[usize]| -> Result<Tensor> {
        let n: usize = dims.iter().product();
        let mut bytes = vec![0u8; 4 * n];
        r.read_exact(&mut bytes)?;
        let data: Vec<f32> = bytes
            .chunks_exact(4)
            .map(|b| f32::from_le_bytes(b.try_into().unwrap()))
            .collect();
        Ok(Tensor::from_vec(data, dims, dev)?)
    };
    let (layers, dim, hidden) = (c.n_layers, c.dim, c.hidden_dim);
    let head = c.head_size();
    let kv = c.n_kv_heads * head;
    let embed = read(&[c.vocab_size, dim])?;
    let rms_att = read(&[layers, dim])?;
    let wq = read(&[layers, c.n_heads * head, dim])?;
    let wk = read(&[layers, kv, dim])?;
    let wv = read(&[layers, kv, dim])?;
    let wo = read(&[layers, dim, c.n_heads * head])?;
    let rms_ffn = read(&[layers, dim])?;
    let w1 = read(&[layers, hidden, dim])?;
    let w2 = read(&[layers, dim, hidden])?;
    let w3 = read(&[layers, hidden, dim])?;
    let rms_final = read(&[dim])?;
    let freq_real = read(&[c.seq_len, head / 2])?;
    let freq_imag = read(&[c.seq_len, head / 2])?;

    // Column-major weight matrices, as candle-transformers' own reader stores them on CPU:
    // a (1, k) x (k, n)ᵀ product is then a plain row-major gemm.
    let cm = |t: Tensor| -> Result<Tensor> { Ok(t.t()?.contiguous()?.t()?) };
    let mut ws: HashMap<String, Tensor> = HashMap::new();
    ws.insert("rot.freq_cis_real".into(), freq_real);
    ws.insert("rot.freq_cis_imag".into(), freq_imag);
    ws.insert("lm_head.weight".into(), cm(embed.clone())?); // shared with the embedding
    ws.insert("model.embed_tokens.weight".into(), embed);
    ws.insert("model.norm.weight".into(), rms_final);
    for l in 0..layers {
        let p = format!("model.layers.{l}");
        ws.insert(format!("{p}.self_attn.q_proj.weight"), cm(wq.i(l)?)?);
        ws.insert(format!("{p}.self_attn.k_proj.weight"), cm(wk.i(l)?)?);
        ws.insert(format!("{p}.self_attn.v_proj.weight"), cm(wv.i(l)?)?);
        ws.insert(format!("{p}.self_attn.o_proj.weight"), cm(wo.i(l)?)?);
        ws.insert(format!("{p}.mlp.gate_proj.weight"), cm(w1.i(l)?)?);
        ws.insert(format!("{p}.mlp.down_proj.weight"), cm(w2.i(l)?)?);
        ws.insert(format!("{p}.mlp.up_proj.weight"), cm(w3.i(l)?)?);
        ws.insert(format!("{p}.input_layernorm.weight"), rms_att.i(l)?);
        ws.insert(format!("{p}.post_attention_layernorm.weight"), rms_ffn.i(l)?);
    }
    Ok(VarBuilder::from_tensors(ws, DType::F32, dev))
}

/// llama2.c's tokenizer.bin and its byte-pair encoder, ported from run.c so the prompt
/// becomes the same token IDs as in the C decoder.
struct Tokenizer {
    vocab: Vec<Vec<u8>>,
    scores: Vec<f32>,
    ids: HashMap<Vec<u8>, usize>,
}

impl Tokenizer {
    fn read(path: &str, vocab_size: usize) -> Result<Self> {
        fn take<'a>(bytes: &'a [u8], at: &mut usize, n: usize) -> Result<&'a [u8]> {
            let s = bytes.get(*at..*at + n).context("tokenizer.bin is truncated")?;
            *at += n;
            Ok(s)
        }
        let bytes = std::fs::read(path).with_context(|| format!("read {path}"))?;
        let mut at = 4; // max_token_length, unused here
        let (mut vocab, mut scores, mut ids) = (Vec::new(), Vec::new(), HashMap::new());
        for id in 0..vocab_size {
            scores.push(f32::from_le_bytes(take(&bytes, &mut at, 4)?.try_into()?));
            let len = i32::from_le_bytes(take(&bytes, &mut at, 4)?.try_into()?) as usize;
            let piece = take(&bytes, &mut at, len)?.to_vec();
            ids.entry(piece.clone()).or_insert(id);
            vocab.push(piece);
        }
        Ok(Self { vocab, scores, ids })
    }

    fn encode(&self, text: &[u8]) -> Result<Vec<usize>> {
        let mut tokens = vec![BOS];
        if !text.is_empty() {
            tokens.push(*self.ids.get(&b" "[..]).context("no ' ' token")?);
        }
        // One token per UTF-8 character, or one per byte (offset by 3) for characters the
        // vocabulary lacks.
        let mut ch = Vec::new();
        for (i, &c) in text.iter().enumerate() {
            if c & 0xC0 != 0x80 {
                ch.clear();
            }
            ch.push(c);
            if text.get(i + 1).is_some_and(|&n| n & 0xC0 == 0x80) && ch.len() < 4 {
                continue;
            }
            match self.ids.get(&ch) {
                Some(&id) => tokens.push(id),
                None => tokens.extend(ch.iter().map(|&b| b as usize + 3)),
            }
            ch.clear();
        }
        // Merge the best-scoring adjacent pair until no pair is in the vocabulary.
        loop {
            let mut best: Option<(f32, usize, usize)> = None;
            for i in 0..tokens.len().saturating_sub(1) {
                let pair = [&self.vocab[tokens[i]][..], &self.vocab[tokens[i + 1]][..]].concat();
                if let Some(&id) = self.ids.get(&pair) {
                    if self.scores[id] > best.map_or(-1e10, |b| b.0) {
                        best = Some((self.scores[id], id, i));
                    }
                }
            }
            let Some((_, id, i)) = best else { break };
            tokens[i] = id;
            tokens.remove(i + 1);
        }
        Ok(tokens)
    }

    fn decode(&self, prev: usize, token: usize) -> Vec<u8> {
        let mut piece = &self.vocab[token][..];
        if prev == BOS && piece.first() == Some(&b' ') {
            piece = &piece[1..];
        }
        // Byte tokens are spelled <0xAB>.
        if piece.len() == 6 && piece.starts_with(b"<0x") && piece[5] == b'>' {
            if let Ok(b) = u8::from_str_radix(std::str::from_utf8(&piece[3..5]).unwrap_or(""), 16) {
                return vec![b];
            }
        }
        piece.to_vec()
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
