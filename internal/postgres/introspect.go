package postgres

import (
	"context"
	"fmt"

	"github.com/gotcli/got-community/internal/dbschema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type Config struct {
	Host     string
	Port     uint16
	Database string
	User     string
	Password string
	Schema   string
}

func Introspect(ctx context.Context, input Config) (dbschema.Database, error) {
	config, err := pgx.ParseConfig("")
	if err != nil {
		return dbschema.Database{}, fmt.Errorf("create PostgreSQL config: %w", err)
	}
	config.Host = input.Host
	config.Port = input.Port
	config.Database = input.Database
	config.User = input.User
	config.Password = input.Password
	config.RuntimeParams["search_path"] = input.Schema

	db := stdlib.OpenDB(*config)
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return dbschema.Database{}, fmt.Errorf("connect to PostgreSQL: %w", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT c.table_name, c.column_name, c.data_type, c.udt_name,
		       c.is_nullable = 'YES', c.column_default IS NOT NULL
		FROM information_schema.columns c
		JOIN information_schema.tables t
		  ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema = $1 AND t.table_type = 'BASE TABLE'
		ORDER BY c.table_name, c.ordinal_position`, input.Schema)
	if err != nil {
		return dbschema.Database{}, fmt.Errorf("read columns: %w", err)
	}
	defer rows.Close()

	tableIndexes := make(map[string]int)
	result := dbschema.Database{Schema: input.Schema}
	for rows.Next() {
		var tableName string
		var column dbschema.Column
		if err := rows.Scan(&tableName, &column.Name, &column.DataType, &column.UDTName, &column.Nullable, &column.HasDefault); err != nil {
			return dbschema.Database{}, fmt.Errorf("scan column: %w", err)
		}
		index, ok := tableIndexes[tableName]
		if !ok {
			index = len(result.Tables)
			tableIndexes[tableName] = index
			result.Tables = append(result.Tables, dbschema.Table{Name: tableName})
		}
		result.Tables[index].Columns = append(result.Tables[index].Columns, column)
	}
	if err := rows.Err(); err != nil {
		return dbschema.Database{}, fmt.Errorf("iterate columns: %w", err)
	}

	pkRows, err := db.QueryContext(ctx, `
		SELECT tc.table_name, kcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON tc.constraint_name = kcu.constraint_name
		 AND tc.constraint_schema = kcu.constraint_schema
		WHERE tc.table_schema = $1 AND tc.constraint_type = 'PRIMARY KEY'
		ORDER BY tc.table_name, kcu.ordinal_position`, input.Schema)
	if err != nil {
		return dbschema.Database{}, fmt.Errorf("read primary keys: %w", err)
	}
	defer pkRows.Close()
	for pkRows.Next() {
		var tableName, columnName string
		if err := pkRows.Scan(&tableName, &columnName); err != nil {
			return dbschema.Database{}, fmt.Errorf("scan primary key: %w", err)
		}
		if index, ok := tableIndexes[tableName]; ok {
			result.Tables[index].PrimaryKey = append(result.Tables[index].PrimaryKey, columnName)
		}
	}
	if err := pkRows.Err(); err != nil {
		return dbschema.Database{}, fmt.Errorf("iterate primary keys: %w", err)
	}

	for tableIndex := range result.Tables {
		for columnIndex := range result.Tables[tableIndex].Columns {
			column := &result.Tables[tableIndex].Columns[columnIndex]
			if column.DataType != "USER-DEFINED" {
				continue
			}
			enumRows, err := db.QueryContext(ctx, `
				SELECT e.enumlabel
				FROM pg_type typ
				JOIN pg_namespace ns ON ns.oid = typ.typnamespace
				JOIN pg_enum e ON e.enumtypid = typ.oid
				WHERE ns.nspname = $1 AND typ.typname = $2
				ORDER BY e.enumsortorder`, input.Schema, column.UDTName)
			if err != nil {
				return dbschema.Database{}, fmt.Errorf("read enum %s: %w", column.UDTName, err)
			}
			for enumRows.Next() {
				var value string
				if err := enumRows.Scan(&value); err != nil {
					enumRows.Close()
					return dbschema.Database{}, fmt.Errorf("scan enum %s: %w", column.UDTName, err)
				}
				column.EnumValues = append(column.EnumValues, value)
			}
			if err := enumRows.Close(); err != nil {
				return dbschema.Database{}, fmt.Errorf("close enum rows: %w", err)
			}
		}
	}
	return result, nil
}
