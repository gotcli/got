package generator

// Module versions installed into generated projects. Keep them free of known
// vulnerabilities: CI generates projects and scans them with govulncheck.
const (
	fiberVersion   = "v2.52.15"
	viperVersion   = "v1.19.0"
	gormVersion    = "v1.25.12"
	decimalVersion = "v1.4.0"
	jwtVersion     = "v5.3.1"
)

// securityFloors raise indirect dependencies above the versions their parents
// require when those versions have known vulnerabilities. Keys are database
// drivers; "" applies to every project.
var securityFloors = map[string][]string{
	"":   {"golang.org/x/text@v0.41.0"},
	"pg": {"github.com/jackc/pgx/v5@v5.9.2"},
}

// projectDependencies lists the "go get" arguments for a new project.
func projectDependencies(database string) []string {
	modules := []string{
		"github.com/gofiber/fiber/v2@" + fiberVersion,
		"github.com/spf13/viper@" + viperVersion,
		"gorm.io/gorm@" + gormVersion,
		databases[database].dependency,
		"github.com/shopspring/decimal@" + decimalVersion,
	}
	modules = append(modules, securityFloors[""]...)
	return append(modules, securityFloors[database]...)
}
