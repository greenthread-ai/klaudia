package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/config"
	"github.com/greenthread-ai/klaudia/internal/doctor"
	"github.com/greenthread-ai/klaudia/internal/mcp"
)

// newDoctorCommand builds `klaudia doctor [--json]`: the same environment
// diagnostics the interactive /doctor renders, run headlessly so a script or CI
// job can read them. It resolves the model and credential the way a normal run
// would but never contacts a provider — it only reports what it finds. When a
// critical check fails (no usable credential — see doctor.Critical) it exits
// non-zero so `klaudia doctor` is a usable preflight gate.
func newDoctorCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:           "doctor",
		Short:         "Print environment diagnostics (headless; --json for machine-readable output)",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, _ := os.Getwd()
			cfg := config.Load(cwd)
			// The model drives only the context-window line; resolve it from
			// config (—model is a run flag, not a doctor flag) so the report
			// reflects what an unflagged `klaudia` would use.
			model := api.ResolveModel(cfg.Model)
			mcpCfg, _ := mcp.LoadConfig(cwd)
			checks := doctor.Run(buildDoctorInput(cfg, model, cwd, len(mcpCfg.MCPServers)))

			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if err := enc.Encode(doctor.NewReport(checks)); err != nil {
					return err
				}
			} else {
				fmt.Fprintln(out, doctor.Format(checks))
			}

			// Non-zero exit on a critical failure so CI can gate on it. The
			// report has already been printed; exitError adds only the code.
			if doctor.Critical(checks) {
				return exitError{ExitError}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit the diagnostics as JSON instead of text")
	return cmd
}
