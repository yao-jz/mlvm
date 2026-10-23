// decoder.c - TinyStories Llama2-C decoder for MLVM.
//
// Reads a karpathy/llama2.c checkpoint, a tok*.bin tokenizer, and a committed
// request.bin. Runs greedy generation and commits the resulting token IDs via
// mlvm_commit_result so the answer lives in the machine state, not stdout.
//
// Staged paths (exact names):
//   model.bin     - llama2.c binary checkpoint
//   tokenizer.bin - llama2.c tokenizer
//   request.bin   - MLVM decoder request (see docs/DECODER-PROTOCOL.md)
//
// Build:  make -C examples/decoder
// Run:    ./scripts/run-elf.sh examples/decoder/build/decoder \
//         model.bin=models/pack260k/model.bin \
//         tokenizer.bin=models/pack260k/tokenizer.bin \
//         request.bin=models/pack260k/request.bin
//
// Derived from karpathy/llama2.c (MIT). Adapted for deterministic MLVM guests.

#include <stdio.h>
#include <stdlib.h>
#include <ctype.h>
#include <math.h>
#include <string.h>
#include <stdint.h>

#include "../mlvm_result.h"

// Each stage of the model stays a real function so `mlvm xray` can attribute instructions
// to matmul, attention, RoPE and so on; at -O2 GCC would otherwise inline the whole
// forward pass into main. noinline moves no arithmetic: every float operation runs in
// the same order on the same values.
#define MLVM_NOINLINE __attribute__((noinline))

#define MAX_PROMPT_TOKENS 256
#define MAX_GEN_TOKENS 16
#define RESULT_VERSION 1

typedef struct {
    int dim;
    int hidden_dim;
    int n_layers;
    int n_heads;
    int n_kv_heads;
    int vocab_size;
    int seq_len;
} Config;

typedef struct {
    float *token_embedding_table;
    float *rms_att_weight;
    float *rms_ffn_weight;
    float *wq;
    float *wk;
    float *wv;
    float *wo;
    float *w1;
    float *w2;
    float *w3;
    float *rms_final_weight;
    float *wcls;
} TransformerWeights;

typedef struct {
    float *x;
    float *xb;
    float *xb2;
    float *hb;
    float *hb2;
    float *q;
    float *k;
    float *v;
    float *att;
    float *logits;
    float *key_cache;
    float *value_cache;
} RunState;

typedef struct {
    Config config;
    TransformerWeights weights;
    RunState state;
    float *data; // owned heap copy; MLVM mmap is anonymous-only
    size_t file_size;
} Transformer;

typedef struct {
    char *str;
    int id;
} TokenIndex;

typedef struct {
    char **vocab;
    float *vocab_scores;
    TokenIndex *sorted_vocab;
    int vocab_size;
    unsigned int max_token_length;
    unsigned char byte_pieces[512];
} Tokenizer;

typedef struct {
    uint32_t version;
    uint32_t max_new_tokens;
    uint32_t temperature_milli; // 0 => greedy
    uint32_t reserved;
    uint32_t prompt_len;
    uint8_t prompt[256];
} Request;

static void malloc_run_state(RunState *s, Config *p) {
    int kv_dim = (p->dim * p->n_kv_heads) / p->n_heads;
    s->x = calloc(p->dim, sizeof(float));
    s->xb = calloc(p->dim, sizeof(float));
    s->xb2 = calloc(p->dim, sizeof(float));
    s->hb = calloc(p->hidden_dim, sizeof(float));
    s->hb2 = calloc(p->hidden_dim, sizeof(float));
    s->q = calloc(p->dim, sizeof(float));
    s->k = calloc(kv_dim, sizeof(float));
    s->v = calloc(kv_dim, sizeof(float));
    s->att = calloc(p->n_heads * p->seq_len, sizeof(float));
    s->logits = calloc(p->vocab_size, sizeof(float));
    s->key_cache = calloc(p->n_layers * p->seq_len * kv_dim, sizeof(float));
    s->value_cache = calloc(p->n_layers * p->seq_len * kv_dim, sizeof(float));
    if (!s->x || !s->xb || !s->xb2 || !s->hb || !s->hb2 || !s->q || !s->k || !s->v ||
        !s->att || !s->logits || !s->key_cache || !s->value_cache) {
        fprintf(stderr, "malloc failed\n");
        exit(EXIT_FAILURE);
    }
}

static void memory_map_weights(TransformerWeights *w, Config *p, float *ptr, int shared_weights) {
    int head_size = p->dim / p->n_heads;
    unsigned long long n_layers = p->n_layers;
    w->token_embedding_table = ptr;
    ptr += p->vocab_size * p->dim;
    w->rms_att_weight = ptr;
    ptr += n_layers * p->dim;
    w->wq = ptr;
    ptr += n_layers * p->dim * (p->n_heads * head_size);
    w->wk = ptr;
    ptr += n_layers * p->dim * (p->n_kv_heads * head_size);
    w->wv = ptr;
    ptr += n_layers * p->dim * (p->n_kv_heads * head_size);
    w->wo = ptr;
    ptr += n_layers * (p->n_heads * head_size) * p->dim;
    w->rms_ffn_weight = ptr;
    ptr += n_layers * p->dim;
    w->w1 = ptr;
    ptr += n_layers * p->dim * p->hidden_dim;
    w->w2 = ptr;
    ptr += n_layers * p->hidden_dim * p->dim;
    w->w3 = ptr;
    ptr += n_layers * p->dim * p->hidden_dim;
    w->rms_final_weight = ptr;
    ptr += p->dim;
    ptr += p->seq_len * head_size / 2; // skip freq_cis_real
    ptr += p->seq_len * head_size / 2; // skip freq_cis_imag
    w->wcls = shared_weights ? w->token_embedding_table : ptr;
}

static MLVM_NOINLINE void read_checkpoint(const char *checkpoint, Config *config, TransformerWeights *weights,
                            float **data, size_t *file_size) {
    // Read the whole checkpoint into malloc'd memory. MLVM's mmap(2) only allocates
    // anonymous pages and ignores the file descriptor, so a file-backed mapping would
    // silently become zeros and every token would be <unk>.
    FILE *file = fopen(checkpoint, "rb");
    if (!file) {
        fprintf(stderr, "couldn't open checkpoint %s\n", checkpoint);
        exit(EXIT_FAILURE);
    }
    if (fseek(file, 0, SEEK_END) != 0) {
        exit(EXIT_FAILURE);
    }
    long sz = ftell(file);
    if (sz < (long)sizeof(Config)) {
        fprintf(stderr, "checkpoint too small\n");
        exit(EXIT_FAILURE);
    }
    if (fseek(file, 0, SEEK_SET) != 0) {
        exit(EXIT_FAILURE);
    }
    *file_size = (size_t)sz;
    *data = malloc(*file_size);
    if (!*data) {
        fprintf(stderr, "malloc checkpoint failed\n");
        exit(EXIT_FAILURE);
    }
    if (fread(*data, 1, *file_size, file) != *file_size) {
        fprintf(stderr, "short read on checkpoint\n");
        exit(EXIT_FAILURE);
    }
    fclose(file);
    memcpy(config, *data, sizeof(Config));
    int shared_weights = config->vocab_size > 0 ? 1 : 0;
    config->vocab_size = abs(config->vocab_size);
    float *weights_ptr = *data + sizeof(Config) / sizeof(float);
    memory_map_weights(weights, config, weights_ptr, shared_weights);
}

static void build_transformer(Transformer *t, const char *checkpoint_path) {
    read_checkpoint(checkpoint_path, &t->config, &t->weights, &t->data, &t->file_size);
    malloc_run_state(&t->state, &t->config);
}

static MLVM_NOINLINE void rmsnorm(float *o, float *x, float *weight, int size) {
    float ss = 0.0f;
    for (int j = 0; j < size; j++) {
        ss += x[j] * x[j];
    }
    ss /= size;
    ss += 1e-5f;
    ss = 1.0f / sqrtf(ss);
    for (int j = 0; j < size; j++) {
        o[j] = weight[j] * (ss * x[j]);
    }
}

static MLVM_NOINLINE void softmax(float *x, int size) {
    float max_val = x[0];
    for (int i = 1; i < size; i++) {
        if (x[i] > max_val) max_val = x[i];
    }
    float sum = 0.0f;
    for (int i = 0; i < size; i++) {
        x[i] = expf(x[i] - max_val);
        sum += x[i];
    }
    for (int i = 0; i < size; i++) {
        x[i] /= sum;
    }
}

static MLVM_NOINLINE void matmul(float *xout, float *x, float *w, int n, int d) {
    for (int i = 0; i < d; i++) {
        float val = 0.0f;
        for (int j = 0; j < n; j++) {
            val += w[i * n + j] * x[j];
        }
        xout[i] = val;
    }
}

// RoPE: rotate each (q, k) pair by a position-dependent angle.
static MLVM_NOINLINE void rope(RunState *s, int dim, int kv_dim, int head_size, int pos) {
    for (int i = 0; i < dim; i += 2) {
        int head_dim = i % head_size;
        float freq = 1.0f / powf(10000.0f, head_dim / (float)head_size);
        float val = pos * freq;
        float fcr = cosf(val);
        float fci = sinf(val);
        int rotn = i < kv_dim ? 2 : 1;
        for (int v = 0; v < rotn; v++) {
            float *vec = v == 0 ? s->q : s->k;
            float v0 = vec[i];
            float v1 = vec[i + 1];
            vec[i] = v0 * fcr - v1 * fci;
            vec[i + 1] = v0 * fci + v1 * fcr;
        }
    }
}

// Multi-head attention over the KV cache for positions 0..pos.
static MLVM_NOINLINE void attention(RunState *s, Config *p, int loff, int kv_dim, int kv_mul, int head_size, int pos) {
    for (int h = 0; h < p->n_heads; h++) {
        float *q = s->q + h * head_size;
        float *att = s->att + h * p->seq_len;
        for (int t = 0; t <= pos; t++) {
            float *k = s->key_cache + loff + t * kv_dim + (h / kv_mul) * head_size;
            float score = 0.0f;
            for (int i = 0; i < head_size; i++) {
                score += q[i] * k[i];
            }
            score /= sqrtf(head_size);
            att[t] = score;
        }
        softmax(att, pos + 1);
        float *xb = s->xb + h * head_size;
        memset(xb, 0, head_size * sizeof(float));
        for (int t = 0; t <= pos; t++) {
            float *v = s->value_cache + loff + t * kv_dim + (h / kv_mul) * head_size;
            float a = att[t];
            for (int i = 0; i < head_size; i++) {
                xb[i] += a * v[i];
            }
        }
    }
}

// SwiGLU feed-forward gate: hb = silu(hb) * hb2.
static MLVM_NOINLINE void swiglu(RunState *s, int hidden_dim) {
    for (int i = 0; i < hidden_dim; i++) {
        float val = s->hb[i];
        val *= (1.0f / (1.0f + expf(-val)));
        val *= s->hb2[i];
        s->hb[i] = val;
    }
}

static MLVM_NOINLINE float *forward(Transformer *transformer, int token, int pos) {
    Config *p = &transformer->config;
    TransformerWeights *w = &transformer->weights;
    RunState *s = &transformer->state;
    float *x = s->x;
    int dim = p->dim;
    int kv_dim = (p->dim * p->n_kv_heads) / p->n_heads;
    int kv_mul = p->n_heads / p->n_kv_heads;
    int hidden_dim = p->hidden_dim;
    int head_size = dim / p->n_heads;

    float *content_row = w->token_embedding_table + token * dim;
    memcpy(x, content_row, dim * sizeof(*x));

    for (unsigned long long l = 0; l < (unsigned long long)p->n_layers; l++) {
        rmsnorm(s->xb, x, w->rms_att_weight + l * dim, dim);

        int loff = l * p->seq_len * kv_dim;
        s->k = s->key_cache + loff + pos * kv_dim;
        s->v = s->value_cache + loff + pos * kv_dim;

        matmul(s->q, s->xb, w->wq + l * dim * dim, dim, dim);
        matmul(s->k, s->xb, w->wk + l * dim * kv_dim, dim, kv_dim);
        matmul(s->v, s->xb, w->wv + l * dim * kv_dim, dim, kv_dim);

        rope(s, dim, kv_dim, head_size, pos);
        attention(s, p, loff, kv_dim, kv_mul, head_size, pos);

        matmul(s->xb2, s->xb, w->wo + l * dim * dim, dim, dim);
        for (int i = 0; i < dim; i++) {
            x[i] += s->xb2[i];
        }

        rmsnorm(s->xb, x, w->rms_ffn_weight + l * dim, dim);
        matmul(s->hb, s->xb, w->w1 + l * dim * hidden_dim, dim, hidden_dim);
        matmul(s->hb2, s->xb, w->w3 + l * dim * hidden_dim, dim, hidden_dim);
        swiglu(s, hidden_dim);
        matmul(s->xb, s->hb, w->w2 + l * dim * hidden_dim, hidden_dim, dim);
        for (int i = 0; i < dim; i++) {
            x[i] += s->xb[i];
        }
    }

    rmsnorm(x, x, w->rms_final_weight, dim);
    matmul(s->logits, x, w->wcls, dim, p->vocab_size);
    return s->logits;
}

static int compare_tokens(const void *a, const void *b) {
    return strcmp(((TokenIndex *)a)->str, ((TokenIndex *)b)->str);
}

static MLVM_NOINLINE void build_tokenizer(Tokenizer *t, const char *tokenizer_path, int vocab_size) {
    t->vocab_size = vocab_size;
    t->vocab = malloc(vocab_size * sizeof(char *));
    t->vocab_scores = malloc(vocab_size * sizeof(float));
    t->sorted_vocab = NULL;
    for (int i = 0; i < 256; i++) {
        t->byte_pieces[i * 2] = (unsigned char)i;
        t->byte_pieces[i * 2 + 1] = '\0';
    }
    FILE *file = fopen(tokenizer_path, "rb");
    if (!file) {
        fprintf(stderr, "couldn't load tokenizer %s\n", tokenizer_path);
        exit(EXIT_FAILURE);
    }
    if (fread(&t->max_token_length, sizeof(int), 1, file) != 1) {
        exit(EXIT_FAILURE);
    }
    int len;
    for (int i = 0; i < vocab_size; i++) {
        if (fread(t->vocab_scores + i, sizeof(float), 1, file) != 1) {
            exit(EXIT_FAILURE);
        }
        if (fread(&len, sizeof(int), 1, file) != 1) {
            exit(EXIT_FAILURE);
        }
        t->vocab[i] = malloc(len + 1);
        if (fread(t->vocab[i], len, 1, file) != 1) {
            exit(EXIT_FAILURE);
        }
        t->vocab[i][len] = '\0';
    }
    fclose(file);
}

static MLVM_NOINLINE char *decode_token(Tokenizer *t, int prev_token, int token) {
    char *piece = t->vocab[token];
    if (prev_token == 1 && piece[0] == ' ') {
        piece++;
    }
    unsigned char byte_val;
    if (sscanf(piece, "<0x%02hhX>", &byte_val) == 1) {
        piece = (char *)t->byte_pieces + byte_val * 2;
    }
    return piece;
}

static int str_lookup(char *str, TokenIndex *sorted_vocab, int vocab_size) {
    TokenIndex tok = {.str = str};
    TokenIndex *res = bsearch(&tok, sorted_vocab, vocab_size, sizeof(TokenIndex), compare_tokens);
    return res != NULL ? res->id : -1;
}

static void push_token(int *tokens, int *n_tokens, int cap, int id) {
    if (*n_tokens >= cap) {
        fprintf(stderr, "prompt too long to tokenize (cap %d)\n", cap);
        exit(EXIT_FAILURE);
    }
    tokens[(*n_tokens)++] = id;
}

static MLVM_NOINLINE void encode(Tokenizer *t, char *text, int8_t bos, int8_t eos, int *tokens, int *n_tokens, int cap) {
    if (t->sorted_vocab == NULL) {
        t->sorted_vocab = malloc(t->vocab_size * sizeof(TokenIndex));
        for (int i = 0; i < t->vocab_size; i++) {
            t->sorted_vocab[i].str = t->vocab[i];
            t->sorted_vocab[i].id = i;
        }
        qsort(t->sorted_vocab, t->vocab_size, sizeof(TokenIndex), compare_tokens);
    }

    *n_tokens = 0;
    if (bos) push_token(tokens, n_tokens, cap, 1);

    if (text[0] != '\0') {
        int dummy_prefix = str_lookup(" ", t->sorted_vocab, t->vocab_size);
        push_token(tokens, n_tokens, cap, dummy_prefix);
    }

    char *str_buffer = malloc((t->max_token_length * 2 + 1 + 2) * sizeof(char));
    size_t str_len = 0;
    for (char *c = text; *c != '\0'; c++) {
        if ((*c & 0xC0) != 0x80) {
            str_len = 0;
        }
        str_buffer[str_len++] = *c;
        str_buffer[str_len] = '\0';
        if ((*(c + 1) & 0xC0) == 0x80 && str_len < 4) {
            continue;
        }
        int id = str_lookup(str_buffer, t->sorted_vocab, t->vocab_size);
        if (id != -1) {
            push_token(tokens, n_tokens, cap, id);
        } else {
            for (size_t i = 0; i < str_len; i++) {
                push_token(tokens, n_tokens, cap, (unsigned char)str_buffer[i] + 3);
            }
        }
        str_len = 0;
    }

    while (1) {
        float best_score = -1e10;
        int best_id = -1;
        int best_idx = -1;
        for (int i = 0; i < (*n_tokens - 1); i++) {
            sprintf(str_buffer, "%s%s", t->vocab[tokens[i]], t->vocab[tokens[i + 1]]);
            int id = str_lookup(str_buffer, t->sorted_vocab, t->vocab_size);
            if (id != -1 && t->vocab_scores[id] > best_score) {
                best_score = t->vocab_scores[id];
                best_id = id;
                best_idx = i;
            }
        }
        if (best_idx == -1) {
            break;
        }
        tokens[best_idx] = best_id;
        for (int i = best_idx + 1; i < (*n_tokens - 1); i++) {
            tokens[i] = tokens[i + 1];
        }
        (*n_tokens)--;
    }

    if (eos) push_token(tokens, n_tokens, cap, 2);
    free(str_buffer);
}

static MLVM_NOINLINE int argmax(float *v, int n) {
    int max_i = 0;
    float max_p = v[0];
    for (int i = 1; i < n; i++) {
        if (v[i] > max_p) {
            max_i = i;
            max_p = v[i];
        }
    }
    return max_i;
}

static MLVM_NOINLINE void read_request(const char *path, Request *req) {
    FILE *f = fopen(path, "rb");
    if (!f) {
        fprintf(stderr, "couldn't open request %s\n", path);
        exit(EXIT_FAILURE);
    }
    if (fread(req, sizeof(Request), 1, f) != 1) {
        fprintf(stderr, "bad request.bin\n");
        exit(EXIT_FAILURE);
    }
    fclose(f);
    if (req->version != 1) {
        fprintf(stderr, "unsupported request version %u\n", req->version);
        exit(EXIT_FAILURE);
    }
    if (req->prompt_len > sizeof(req->prompt)) {
        fprintf(stderr, "prompt too long\n");
        exit(EXIT_FAILURE);
    }
    if (req->temperature_milli != 0) {
        fprintf(stderr, "non-zero temperature is not implemented; use greedy (0)\n");
        exit(EXIT_FAILURE);
    }
    if (req->max_new_tokens == 0 || req->max_new_tokens > MAX_GEN_TOKENS) {
        req->max_new_tokens = MAX_GEN_TOKENS;
    }
}

int main(void) {
    const char *checkpoint = "model.bin";
    const char *tokenizer_path = "tokenizer.bin";
    const char *request_path = "request.bin";

    // Allow host runs with env overrides for local debugging.
    const char *env;
    if ((env = getenv("MLVM_MODEL"))) checkpoint = env;
    if ((env = getenv("MLVM_TOKENIZER"))) tokenizer_path = env;
    if ((env = getenv("MLVM_REQUEST"))) request_path = env;

    Request req;
    read_request(request_path, &req);

    Transformer transformer;
    build_transformer(&transformer, checkpoint);

    Tokenizer tokenizer;
    build_tokenizer(&tokenizer, tokenizer_path, transformer.config.vocab_size);

    char prompt[257];
    memcpy(prompt, req.prompt, req.prompt_len);
    prompt[req.prompt_len] = '\0';

    int prompt_tokens[MAX_PROMPT_TOKENS];
    int num_prompt_tokens = 0;
    encode(&tokenizer, prompt, 1, 0, prompt_tokens, &num_prompt_tokens, MAX_PROMPT_TOKENS);
    if (num_prompt_tokens < 1) {
        fprintf(stderr, "empty prompt tokenization\n");
        exit(EXIT_FAILURE);
    }

    int generated[MAX_GEN_TOKENS];
    int n_gen = 0;
    int token = prompt_tokens[0];
    int pos = 0;
    int next;

    printf("MLVM decoder demo\n");
    printf("prompt: %s\n", prompt);
    printf("generated: ");
    fflush(stdout);

    while (pos < transformer.config.seq_len) {
        float *logits = forward(&transformer, token, pos);
        if (pos < num_prompt_tokens - 1) {
            next = prompt_tokens[pos + 1];
        } else {
            // Greedy only for the committed demo path.
            next = argmax(logits, transformer.config.vocab_size);
            generated[n_gen++] = next;
            char *piece = decode_token(&tokenizer, token, next);
            fputs(piece, stdout);
            fflush(stdout);
            if (n_gen >= (int)req.max_new_tokens || next == 2) {
                break;
            }
        }
        token = next;
        pos++;
    }
    printf("\n");

    // Result payload (little-endian):
    //   u16 version | u16 count | u16 tokens[MAX_GEN_TOKENS]
    // 16 tokens = 36 payload bytes, inside the 48-byte MLVM_RESULT window.
    struct {
        uint16_t version;
        uint16_t count;
        uint16_t tokens[MAX_GEN_TOKENS];
    } answer;
    memset(&answer, 0, sizeof answer);
    answer.version = RESULT_VERSION;
    answer.count = (uint16_t)n_gen;
    for (int i = 0; i < n_gen; i++) {
        answer.tokens[i] = (uint16_t)generated[i];
    }
    mlvm_commit_result(&answer, sizeof answer);

    printf("tokens:");
    for (int i = 0; i < n_gen; i++) {
        printf(" %u", answer.tokens[i]);
    }
    printf("\n");
    return 0;
}
