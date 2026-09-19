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
}
