package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/gotcli/got/internal/routescan"
	"github.com/spf13/cobra"
)

type serviceRoutes struct {
	Service string            `json:"service,omitempty"`
	Routes  []routescan.Route `json:"routes"`
}

func newRoutesCommand(cfg *config) *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Use:   "routes",
		Short: "List the HTTP routes of the current project",
		Long: `List every Fiber route registered by the current generated project.

Routes are discovered by reading the source, starting at func main and
following router groups across packages; nothing is compiled or run. Paths
built at runtime are shown as {dynamic}. From a workspace root, routes of every
service are listed. The command exits with an error when the same method and
path are registered twice, because Fiber silently serves only the first.`,
		Example: `  got routes
  got routes --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			projects, workspace, err := discoverDoctorProjects(cfg.cwd)
			if err != nil {
				return err
			}
			results := make([]serviceRoutes, 0, len(projects))
			var duplicates []string
			for _, project := range projects {
				routes, err := routescan.Scan(project.Path)
				if err != nil {
					return err
				}
				result := serviceRoutes{Routes: routes}
				if workspace {
					result.Service = filepath.Base(project.Path)
				}
				for _, duplicate := range routescan.Duplicates(routes) {
					duplicates = append(duplicates, strings.TrimSpace(result.Service+" "+duplicate))
				}
				results = append(results, result)
			}
			if asJSON {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				if workspace {
					err = encoder.Encode(results)
				} else {
					err = encoder.Encode(results[0].Routes)
				}
				if err != nil {
					return err
				}
			} else {
				writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
				header := "METHOD\tPATH\tHANDLER\tSOURCE"
				if workspace {
					header = "SERVICE\t" + header
				}
				fmt.Fprintln(writer, header)
				for _, result := range results {
					for _, route := range result.Routes {
						row := fmt.Sprintf("%s\t%s\t%s\t%s", route.Method, route.Path, route.Handler, route.Source)
						if workspace {
							row = result.Service + "\t" + row
						}
						fmt.Fprintln(writer, row)
					}
				}
				if err := writer.Flush(); err != nil {
					return err
				}
			}
			if len(duplicates) > 0 {
				return fmt.Errorf("duplicate routes (Fiber serves only the first registration): %s", strings.Join(duplicates, "; "))
			}
			return nil
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "print routes as JSON")
	return command
}
