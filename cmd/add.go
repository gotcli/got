package cmd

import (
	"fmt"

	"github.com/gotcli/got-community/internal/generator"
	"github.com/spf13/cobra"
)

func newAddCommand(cfg *config) *cobra.Command {
	add := &cobra.Command{
		Use:   "add",
		Short: "Extend the current generated project",
		Long: `Extend an existing GOT project without overwriting unrelated files.

Use "got add method" to extend a feature across repository, service, handler,
and route layers. Use "got add auth --jwt" to install local JWT support,
"got add upload" to add a secure multipart file upload endpoint, or
"got add swagger" to serve an OpenAPI specification and Swagger UI.`,
		Example: `  got add method --folder orders --name Approve --http-method PATCH
  got add auth --jwt
  got add upload
  got add swagger`,
	}
	swagger := &cobra.Command{
		Use:   "swagger",
		Short: "Add OpenAPI documentation and Swagger UI",
		Long: `Generate an embedded OpenAPI 3.0 specification and Swagger UI routes.

The specification is served at /swagger/openapi.json and the UI at
/swagger/index.html. Swagger is controlled by SWAGGER_ENABLED and is always
disabled when APP_ENV is production. The specification is embedded in the
binary; the UI loads its assets from jsDelivr and therefore needs browser
network access. No additional generator tool or runtime Go dependency is needed.`,
		Example: `  cd my_service
  got add swagger
  go run .

  open http://127.0.0.1:3000/swagger/index.html`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := (generator.SwaggerGenerator{}).Generate(cfg.cwd); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "added Swagger UI at /swagger/index.html and OpenAPI spec at /swagger/openapi.json")
			return nil
		},
	}
	upload := &cobra.Command{
		Use: "upload", Short: "Add a multipart file upload endpoint",
		Long: `Generate a local file-storage abstraction, upload service, Fiber handler,
and POST /api/uploads route. Uploaded files receive random names; path traversal
is prevented by discarding directory components. Runtime configuration controls
the directory, maximum size, and allowed extensions. The default limit is 10 MiB
and the default extensions are .jpg, .jpeg, .png, and .pdf.`,
		Example: `  cd my_service
  got add upload

  curl -F "file=@document.pdf" http://127.0.0.1:3000/api/uploads`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := (generator.UploadGenerator{}).Generate(cfg.cwd); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "added file upload endpoint at POST /api/uploads")
			return nil
		},
	}
	service := &cobra.Command{
		Use: "service [name]", Short: "Add a microservice to the current workspace",
		Long: `Add an independently buildable microservice to a GOT workspace created by
"got init workspace". The command reads got-workspace.json, generates the child
service, and updates go.work, compose.yml, Makefile, and workspace README.`,
		Example: `  cd restaurant-platform
  got add service payment
  got add service notification-service`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := (generator.WorkspaceGenerator{Output: cmd.ErrOrStderr()}).AddService(cmd.Context(), cfg.cwd, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "added service %s to workspace\n", args[0])
			return nil
		},
	}
	var authOptions generator.AuthOptions
	auth := &cobra.Command{
		Use:   "auth",
		Short: "Add authentication support to the current project",
		Long: `Add local JWT authentication to the current generated project.

The command generates access/refresh token management and Fiber bearer
middleware, appends JWT configuration, and installs golang-jwt/jwt v5.
Access and refresh secrets remain empty and must be supplied securely through
environment variables. Login, password verification, and user lookup remain
application-specific and are not generated.`,
		Example: `  cd my_service
  got add auth --jwt`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := (generator.AuthGenerator{}).Generate(cmd.Context(), cfg.cwd, authOptions); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "added JWT authentication")
			return nil
		},
	}
	auth.Flags().BoolVar(&authOptions.JWT, "jwt", false, "add JWT access/refresh tokens and bearer middleware")
	var options generator.MethodOptions
	method := &cobra.Command{
		Use:   "method",
		Short: "Add a method across all feature layers",
		Long: `Add an exported method across repository, service, handler, and route layers.

If the feature folder exists, GOT updates its interfaces and implementations
using the Go syntax tree. If it does not exist, GOT scaffolds the four layers.
New repository methods contain an intentional TODO panic until their database
operation is implemented. The default HTTP method is POST; the default path is
the kebab-case method name, such as /publish-order.`,
		Example: `  # Add to an existing orders feature
  got add method --folder orders --name Approve \
    --http-method PATCH --path /:id/approve

  # Scaffold a payments feature and POST /capture
  got add method --folder payments --name Capture`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := (generator.MethodGenerator{}).Generate(cfg.cwd, options); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "added method %s to %s\n", options.Name, options.Folder)
			return nil
		},
	}
	method.Flags().StringVarP(&options.Name, "name", "n", "", "exported method name, for example Approve")
	method.Flags().StringVarP(&options.Folder, "folder", "f", "", "feature folder, for example orders")
	method.Flags().StringVar(&options.HTTPMethod, "http-method", "POST", "HTTP method: GET, POST, PUT, PATCH, or DELETE")
	method.Flags().StringVar(&options.Path, "path", "", "Fiber route path (default: kebab-case method name)")
	_ = method.MarkFlagRequired("name")
	_ = method.MarkFlagRequired("folder")
	add.AddCommand(auth, method, service, swagger, upload)
	return add
}
