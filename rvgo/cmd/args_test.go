package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestHoistFlags(t *testing.T) {
	commands := []*cli.Command{DemoCommand}
	cases := []struct {
		in, want []string
	}{
		{
			in:   []string{"mlvm", "demo", "The dragon", "--json"},
			want: []string{"mlvm", "demo", "--json", "--", "The dragon"},
		},
		{
			in:   []string{"mlvm", "demo", "Once", "--max-new-tokens", "8", "upon", "--receipt=r.json"},
			want: []string{"mlvm", "demo", "--max-new-tokens", "8", "--receipt=r.json", "--", "Once", "upon"},
		},
		{
			in:   []string{"mlvm", "demo", "--demo", "mlp", "--verify"},
			want: []string{"mlvm", "demo", "--demo", "mlp", "--verify"},
		},
		{
			in:   []string{"mlvm", "demo", "--", "--json"},
			want: []string{"mlvm", "demo", "--", "--json"},
		},
		{
			in:   []string{"mlvm", "unknown", "x", "--json"},
			want: []string{"mlvm", "unknown", "x", "--json"},
		},
	}
	for _, c := range cases {
		require.Equal(t, c.want, HoistFlags(commands, c.in), "%q", c.in)
	}
}

func TestDemoPromptAfterFlags(t *testing.T) {
	var got string
	app := cli.NewApp()
	app.Commands = []*cli.Command{{
		Name:  "demo",
		Flags: DemoCommand.Flags,
		Action: func(ctx *cli.Context) error {
			got = PromptArg(ctx)
			require.True(t, ctx.Bool("json"))
			require.Equal(t, 8, ctx.Int("max-new-tokens"))
			return nil
		},
	}}
	args := HoistFlags(app.Commands, []string{"mlvm", "demo", "Once", "upon", "--json", "--max-new-tokens", "8"})
	require.NoError(t, app.Run(args))
	require.Equal(t, "Once upon", got)
}
