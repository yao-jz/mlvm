package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfave/cli/v2"

	"github.com/yao-jz/mlvm/rvgo/internal/dispute"
	"github.com/yao-jz/mlvm/rvgo/internal/e2edemo"
	"github.com/yao-jz/mlvm/rvgo/internal/webui"
)

func DemoAction(ctx *cli.Context) error {
	prompt := PromptArg(ctx)
	opts := e2edemo.Options{
		Prompt:       prompt,
		ModelPackDir: ctx.String("model-pack"),
		Demo:         ctx.String("demo"),
		MaxNewTokens: ctx.Int("max-new-tokens"),
		JSON:         ctx.Bool("json"),
		VerifySlow:   ctx.Bool("verify"),
	}
	res, err := e2edemo.Run(opts)
	if err != nil {
		return err
	}

	if out := ctx.Path("receipt"); out != "" {
		blob, err := res.Receipt.JSON()
		if err != nil {
			return err
		}
		if err := os.WriteFile(out, blob, 0o644); err != nil {
			return err
		}
	}

	if ctx.Bool("json") {
		enc := json.NewEncoder(ctx.App.Writer)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res.Receipt); err != nil {
			return err
		}
	} else {
		fmt.Fprint(ctx.App.Writer, res.Text)
	}

	if ctx.Bool("web") {
		return serveWeb(res)
	}
	return nil
}

func serveWeb(res *e2edemo.Result) error {
	var (
		mu   sync.Mutex
		game *dispute.Game
	)
	kind := "replay-simulation"
	if res.Receipt != nil && res.Receipt.Demo == "mlp" {
		g, err := e2edemo.ReplayMLPPrefix(64)
		if err != nil {
			return err
		}
		game = g
		kind = "fast-prefix"
	} else {
		// Decoder traces are tens of millions of steps. The explorer shows
		// bisection shape from receipt hashes, not a committed decoder trace.
		game = syntheticGameFromReceipt(res)
	}

	mux := http.NewServeMux()
	mux.Handle("/", webui.Handler())
	mux.HandleFunc("/api/receipt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res.Receipt)
	})
	mux.HandleFunc("/api/dispute", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"kind":     kind,
			"preHash":  game.PreHash.Hex(),
			"postHash": game.PostHash.Hex(),
			"maxSteps": game.MaxSteps,
			"claims":   game.Claims,
		})
	})
	mux.HandleFunc("/api/dispute/bisect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		parent := len(game.Claims) - 1
		if parent < 0 {
			parent = 0
		}
		idx, err := game.Bisect(parent)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(game.Claims[idx])
	})
	mux.HandleFunc("/api/dispute/tamper", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		game.TamperPostHash(common.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"))
		verdict, _ := game.ResolveClaim(0)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"postHash": game.PostHash.Hex(),
			"verdict":  verdict,
			"detail":   "forged poststate rejected against the simulated receipt trace",
		})
	})
	mux.HandleFunc("/api/dispute/resolve", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		verdict, err := game.ResolveClaim(0)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"verdict": verdict})
	})
	srv := &http.Server{Addr: "127.0.0.1:8741", Handler: localOnly(mux)}
	url := "http://127.0.0.1:8741/"
	fmt.Fprintf(os.Stderr, "opening %s (ctrl-c to stop)\n", url)
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = openBrowser(url)
	}()
	return srv.ListenAndServe()
}

func syntheticGameFromReceipt(res *e2edemo.Result) *dispute.Game {
	pre := common.HexToHash(res.Receipt.PreState)
	post := common.HexToHash(res.Receipt.PostState)
	// Build a short synthetic trace so midpoint bisection is demonstrable offline.
	n := uint64(8)
	if res.Receipt.Steps > 0 && res.Receipt.Steps < n {
		n = res.Receipt.Steps
	}
	trace := make([]common.Hash, n+1)
	trace[0] = pre
	for i := uint64(1); i < n; i++ {
		b := append([]byte(nil), pre.Bytes()...)
		b[31] ^= byte(i)
		trace[i] = common.BytesToHash(b)
	}
	trace[n] = post
	return &dispute.Game{
		PreHash:  pre,
		PostHash: post,
		MaxSteps: n,
		Trace:    trace,
		Claims: []dispute.Claim{{
			Index: 0, Parent: -1, Lo: 0, Hi: n, Position: n, StateHash: post,
		}},
	}
}

func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Host {
		case "127.0.0.1:8741", "localhost:8741", "":
			next.ServeHTTP(w, r)
		default:
			http.Error(w, "refusing non-local Host", http.StatusForbidden)
		}
	})
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

var DemoCommand = &cli.Command{
	Name:      "demo",
	Usage:     "Run the one-command deterministic inference demo",
	ArgsUsage: "[prompt]",
	Description: "Without flags, runs the embedded TinyStories decoder: a prompt in, a short story out, " +
		"and an execution receipt whose poststate commits the generated tokens. " +
		"Pass --demo mlp for the zero-dependency MLP fallback. Use --web to open the local receipt explorer.",
	Action: DemoAction,
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "demo", Usage: "Demo to run: decoder (default) or mlp"},
		&cli.StringFlag{Name: "model-pack", Usage: "Override decoder assets (model.bin, tokenizer.bin, decoder.elf)"},
		&cli.IntFlag{Name: "max-new-tokens", Usage: "Decoder generation length (1..16)", Value: e2edemo.DefaultMaxNewTokens},
		&cli.BoolFlag{Name: "json", Usage: "Emit a machine-readable receipt"},
		&cli.BoolFlag{Name: "verify", Usage: "Cross-check the MLVM_OUT store with slow.Step"},
		&cli.PathFlag{Name: "receipt", Usage: "Write the JSON receipt to this path"},
		&cli.BoolFlag{Name: "web", Usage: "Serve the receipt explorer on 127.0.0.1:8741"},
	},
}
