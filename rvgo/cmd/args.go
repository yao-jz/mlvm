package cmd

import (
	"strings"

	"github.com/urfave/cli/v2"
)

// HoistFlags lets `mlvm demo "a prompt" --json` mean the same as
// `mlvm demo --json "a prompt"`.
//
// urfave/cli v2 stops parsing flags at the first positional argument, so a flag typed
// after a prompt was silently treated as more prompt. For the subcommand named in
// args[1], HoistFlags moves every recognized flag (and the value of a flag that takes
// one) ahead of the positional arguments, and inserts "--" so a positional that starts
// with "-" is never parsed as a flag. Unknown subcommands, global flags and anything
// after an explicit "--" are left alone.
func HoistFlags(commands []*cli.Command, args []string) []string {
	if len(args) < 3 {
		return args
	}
	var command *cli.Command
	for _, c := range commands {
		if c.HasName(args[1]) {
			command = c
			break
		}
	}
	if command == nil || len(command.Subcommands) > 0 {
		return args
	}

	takesValue := map[string]bool{}
	for _, f := range command.Flags {
		valued := true
		if df, ok := f.(cli.DocGenerationFlag); ok {
			valued = df.TakesValue()
		}
		for _, name := range f.Names() {
			takesValue[name] = valued
		}
	}
	takesValue["help"], takesValue["h"] = false, false

	flags := []string{}
	positional := []string{}
	rest := args[2:]
	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		if arg == "--" {
			positional = append(positional, rest[i+1:]...)
			break
		}
		name := strings.TrimLeft(arg, "-")
		if !strings.HasPrefix(arg, "-") || name == "" {
			positional = append(positional, arg)
			continue
		}
		name, _, hasValue := strings.Cut(name, "=")
		valued, known := takesValue[name]
		if !known {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		if valued && !hasValue && i+1 < len(rest) {
			i++
			flags = append(flags, rest[i])
		}
	}

	out := append([]string{}, args[:2]...)
	out = append(out, flags...)
	if len(positional) > 0 {
		out = append(out, "--")
		out = append(out, positional...)
	}
	return out
}

// PromptArg joins the positional arguments, so `mlvm demo Once upon a time` works
// without quotes.
func PromptArg(ctx *cli.Context) string {
	return strings.Join(ctx.Args().Slice(), " ")
}
