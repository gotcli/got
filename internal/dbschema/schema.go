package dbschema

type Database struct {
	Schema string
	Tables []Table
}

type Table struct {
	Name       string
	Columns    []Column
	PrimaryKey []string
}

type Column struct {
	Name       string
	DataType   string
	UDTName    string
	Nullable   bool
	HasDefault bool
	EnumValues []string
	// The fields below are populated by PostgreSQL introspection and used to
	// reconstruct table DDL for migrations.
	DefaultExpr      string
	Identity         bool
	CharLength       int
	NumericPrecision int
	NumericScale     int
}
