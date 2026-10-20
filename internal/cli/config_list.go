package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rossedman/muscles/internal/config"
	"github.com/rossedman/muscles/internal/ui"
)

type settingJSON struct {
	Key    string `json:"key"`
	Value  any    `json:"value"`
	Origin string `json:"origin"`
	Env    string `json:"env"`
	Help   string `json:"help"`
}

type configJSON struct {
	Schema   string        `json:"schema"`
	User     string        `json:"user_file"`
	Project  string        `json:"project_file"`
	Settings []settingJSON `json:"settings"`
}

func newConfigListCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every setting, its value and where it came from",
		Long: `List every setting with its resolved value and the layer that set it:
flag, env, project, user or default. Plain output has one line per
setting: key, value, origin.`,
		Example: `  muscles config list
  muscles config list --config ci/.muscles.yaml
  muscles config list -o plain | grep -v default`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := configJSON{Schema: Schema, User: a.files.User, Project: a.files.Project}
			for _, k := range config.Keys {
				out.Settings = append(out.Settings, settingJSON{Key: k.Name, Value: a.v.Get(k.Name), Origin: a.origin(cmd, k.Name), Env: config.EnvVar(k.Name), Help: k.Help})
			}
			for i := range out.Settings {
				if out.Settings[i].Key == "cache.dir" {
					out.Settings[i].Value = a.cfg.Cache.Dir
				}
			}
			f, _ := a.format()
			if f == formatJSON {
				return a.printJSON(out)
			}
			t := &ui.Table{Columns: []ui.Column{{Title: "KEY"}, {Title: "VALUE"}, {Title: "ORIGIN"}}}
			for _, s := range out.Settings {
				origin := ui.Cell{Text: s.Origin}
				if s.Origin == "default" {
					origin.Paint = a.th.Painter(a.th.Muted)
				}
				t.Add(ui.C(s.Key), ui.C(valueString(s.Value)), origin)
			}
			return a.render(f, t, "Settings")
		},
	}
}

func valueString(v any) string {
	switch x := v.(type) {
	case []string:
		return strings.Join(x, ",")
	case []any:
		parts := make([]string, len(x))
		for i, p := range x {
			parts[i] = fmt.Sprint(p)
		}
		return strings.Join(parts, ",")
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}
