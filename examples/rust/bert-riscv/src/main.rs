use candle_transformers::models::bert::{BertModel, Config, HiddenAct, DTYPE};

use anyhow::{Error as E, Result};
use candle_core::{Device, Tensor};
use candle_nn::VarBuilder;
use clap::Parser;
use tokenizers::{PaddingParams, Tokenizer};

#[derive(Parser, Debug)]
struct Args {
    /// Run on CPU rather than on GPU.
    #[arg(long)]
    cpu: bool,

    /// Enable tracing (generates a trace-timestamp.json file).
    #[arg(long)]
    tracing: bool,

    /// The model to use, check out available models: https://huggingface.co/models?library=sentence-transformers&sort=trending
    #[arg(long)]
    model_id: Option<String>,

    #[arg(long)]
    revision: Option<String>,

    /// Use the pytorch weights rather than the safetensors ones
    #[arg(long)]
    use_pth: bool,

    /// The number of times to run the prompt.
    #[arg(long, default_value = "1")]
    n: usize,

    /// L2 normalization for embeddings.
    #[arg(long, default_value = "true")]
    normalize_embeddings: bool,

    /// Use tanh based approximation for Gelu instead of erf implementation.
    #[arg(long, default_value = "false")]
    approximate_gelu: bool,

    /// Model config. Must be staged into the VM under this exact path.
    #[arg(long, default_value = "config.json")]
    config: String,

    /// Tokenizer. Must be staged into the VM under this exact path.
    #[arg(long, default_value = "tokenizer.json")]
    tokenizer: String,

    /// safetensors weights. Must be staged into the VM under this exact path.
    #[arg(long, default_value = "model.safetensors")]
    weights: String,

    /// Sentences to embed and compare, as a single semicolon-separated string.
    #[arg(long, default_value = "The cat sits outside;The cat plays in the garden")]
    sentences: String,
}

impl Args {
    fn build_model_and_tokenizer(&self) -> Result<(BertModel, Tokenizer)> {
        let device = Device::Cpu;

        let (config_filename, tokenizer_filename, weights_filename) =
            (self.config.as_str(), self.tokenizer.as_str(), self.weights.as_str());
        // print all the names
        println!("config_filename: {:?}", config_filename);
        println!("tokenizer_filename: {:?}", tokenizer_filename);
        println!("weights_filename: {:?}", weights_filename);
        let config = std::fs::read_to_string(config_filename)?;
        let mut config: Config = serde_json::from_str(&config)?;
        let tokenizer = Tokenizer::from_file(tokenizer_filename).map_err(E::msg)?;
        let mut weights = candle_core::safetensors::load(weights_filename, &Device::Cpu)?;

        // Task-head checkpoints (BertForMaskedLM, BertForSequenceClassification, ...)
        // store the encoder under a "bert." prefix, while candle's BertModel expects the
        // bare names. Strip it so both layouts load: without this,
        // google-bert/bert-base-uncased fails with
        // "cannot find tensor embeddings.word_embeddings.weight".
        if weights.keys().any(|k| k.starts_with("bert.")) {
            weights = weights
                .into_iter()
                .map(|(k, v)| (k.strip_prefix("bert.").unwrap_or(&k).to_string(), v))
                .collect();
            println!("stripped \"bert.\" prefix from checkpoint tensor names");
        }

        // The original 2018 BERT checkpoints name LayerNorm parameters gamma/beta, which
        // is what TensorFlow called them. Everything since uses weight/bias, and that is
        // what candle looks for. Renaming here means google-bert/bert-base-uncased and
        // its contemporaries load without converting the checkpoint.
        if weights.keys().any(|k| k.ends_with("LayerNorm.gamma")) {
            weights = weights
                .into_iter()
                .map(|(k, v)| {
                    let k = match k.strip_suffix("LayerNorm.gamma") {
                        Some(prefix) => format!("{prefix}LayerNorm.weight"),
                        None => match k.strip_suffix("LayerNorm.beta") {
                            Some(prefix) => format!("{prefix}LayerNorm.bias"),
                            None => k,
                        },
                    };
                    (k, v)
                })
                .collect();
            println!("renamed legacy LayerNorm gamma/beta to weight/bias");
        }
        let weights = VarBuilder::from_tensors(weights, DTYPE, &device);

        // let vb = if self.use_pth {
        //     VarBuilder::from_pth(&weights_filename, DTYPE, &device)?
        // } else {
        //     // unsafe { VarBuilder::from_mmaped_safetensors(&[weights_filename], DTYPE, &device)? } // TODO
        //     candle_core::safetensors::load(weights_filename, &Device::Cpu);
        // };
        if self.approximate_gelu {
            config.hidden_act = HiddenAct::GeluApproximate;
        }
        let model = BertModel::load(weights, &config)?;
        println!("load model over");
        Ok((model, tokenizer))
    }
}

fn main() -> Result<()> {
    let args = Args::parse();
    

    let (model, mut tokenizer) = args.build_model_and_tokenizer()?;
    let device = &model.device;

    let sentences: Vec<&str> = args.sentences.split(';').collect();
    println!("sentences: {:?}", sentences);
    let n_sentences = sentences.len();
    println!("n_sentences: {:?}", n_sentences);
    // Encode one sentence at a time and pad by hand, rather than calling encode_batch.
    //
    // encode_batch parallelises with rayon, and rayon builds its global thread pool
    // lazily on first use. Inside MLVM that pool cannot be built - clone(2) is not
    // supported, deliberately, because concurrent execution has no deterministic
    // interleaving to prove - so encode_batch panics with:
    //
    //   The global thread pool has not been initialized.:
    //   ThreadPoolBuildError { kind: IOError(PermissionDenied) }
    //
    // This is the single most common porting obstacle for Rust ML guests: the framework
    // is fine, but a parallel iterator somewhere in the data path is not. Encoding
    // sequentially produces identical token IDs.
    tokenizer.with_padding(None::<PaddingParams>);
    let encodings = sentences
        .iter()
        .map(|s| tokenizer.encode(*s, true).map_err(E::msg))
        .collect::<Result<Vec<_>>>()?;

    // BatchLongest padding, applied manually: pad every sequence out to the longest one
    // with the [PAD] id (0 for BERT WordPiece vocabularies).
    let max_len = encodings.iter().map(|e| e.get_ids().len()).max().unwrap_or(0);
    println!("tokenised: {} sentences, padded to {} tokens", n_sentences, max_len);
    let token_ids = encodings
        .iter()
        .map(|e| {
            let mut ids = e.get_ids().to_vec();
            ids.resize(max_len, 0u32);
            Ok(Tensor::new(ids.as_slice(), device)?)
        })
        .collect::<Result<Vec<_>>>()?;
    println!("token_ids: {:?}", token_ids);
    let token_ids = Tensor::stack(&token_ids, 0)?;
    println!("token_ids: {:?}", token_ids);
    let token_type_ids = token_ids.zeros_like()?;
    println!("running inference on batch {:?}", token_ids.shape());
    let embeddings = model.forward(&token_ids, &token_type_ids)?;
    println!("generated embeddings {:?}", embeddings.shape());
    // Apply some avg-pooling by taking the mean embedding value for all tokens (including padding)
    let (_n_sentence, n_tokens, _hidden_size) = embeddings.dims3()?;
    let embeddings = (embeddings.sum(1)? / (n_tokens as f64))?;
    let embeddings = if args.normalize_embeddings {
        normalize_l2(&embeddings)?
    } else {
        embeddings
    };
    println!("pooled embeddings {:?}", embeddings.shape());

    let mut similarities = vec![];
    for i in 0..n_sentences {
        let e_i = embeddings.get(i)?;
        for j in (i + 1)..n_sentences {
            let e_j = embeddings.get(j)?;
            let sum_ij = (&e_i * &e_j)?.sum_all()?.to_scalar::<f32>()?;
            let sum_i2 = (&e_i * &e_i)?.sum_all()?.to_scalar::<f32>()?;
            let sum_j2 = (&e_j * &e_j)?.sum_all()?.to_scalar::<f32>()?;
            let cosine_similarity = sum_ij / (sum_i2 * sum_j2).sqrt();
            similarities.push((cosine_similarity, i, j))
        }
    }
    similarities.sort_by(|u, v| v.0.total_cmp(&u.0));
    for &(score, i, j) in similarities[..1].iter() {
        println!("score: {score:.10} '{}' '{}'", sentences[i], sentences[j]);
        // Printing is not enough: stdout is not part of the machine's state, so a machine
        // that printed a different score would end in the same state root and could not be
        // challenged. Committing the score puts it under the root.
        commit_result(format!("score: {score:.10}").as_bytes());
    }
    Ok(())
}

/// Writes an answer into MLVM's result window, MLVM_OUT, in the layout of
/// examples/mlvm_result.h: magic, length, then up to 48 payload bytes. The final memory
/// root commits to it, and `mlvm liar --lie commit` can forge it.
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

/// The host build has no MLVM address space; its stdout is the comparison.
#[cfg(not(target_arch = "riscv64"))]
fn commit_result(_payload: &[u8]) {}

pub fn normalize_l2(v: &Tensor) -> Result<Tensor> {
    Ok(v.broadcast_div(&v.sqr()?.sum_keepdim(1)?.sqrt()?)?)
}
