package sqlserver

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"

	"github.com/gotcli/got-community/internal/dbschema"
	_ "github.com/microsoft/go-mssqldb"
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
	connectionURL := &url.URL{
		Scheme: "sqlserver",
		User:   url.UserPassword(input.User, input.Password),
		Host:   net.JoinHostPort(input.Host, strconv.Itoa(int(input.Port))),
	}
	query := connectionURL.Query()
	query.Set("database", input.Database)
	connectionURL.RawQuery = query.Encode()

	db, err := sql.Open("sqlserver", connectionURL.String())
	if err != nil {
		return dbschema.Database{}, fmt.Errorf("create SQL Server connection: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return dbschema.Database{}, fmt.Errorf("connect to SQL Server: %w", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT t.name, c.name, typ.name, c.is_nullable,
		       CASE WHEN dc.object_id IS NULL THEN CAST(0 AS bit) ELSE CAST(1 AS bit) END
		FROM sys.tables t
		JOIN sys.schemas s ON s.schema_id = t.schema_id
		JOIN sys.columns c ON c.object_id = t.object_id
		JOIN sys.types typ ON typ.user_type_id = c.user_type_id
		LEFT JOIN sys.default_constraints dc ON dc.object_id = c.default_object_id
		WHERE s.name = @schema
		ORDER BY t.name, c.column_id`, sql.Named("schema", input.Schema))
	if err != nil {
		return dbschema.Database{}, fmt.Errorf("read SQL Server columns: %w", err)
	}
	defer rows.Close()

	tableIndexes := make(map[string]int)
	result := dbschema.Database{Schema: input.Schema}
	for rows.Next() {
		var tableName string
		var column dbschema.Column
		if err := rows.Scan(&tableName, &column.Name, &column.DataType, &column.Nullable, &column.HasDefault); err != nil {
			return dbschema.Database{}, fmt.Errorf("scan SQL Server column: %w", err)
		}
		column.UDTName = column.DataType
		index, exists := tableIndexes[tableName]
		if !exists {
			index = len(result.Tables)
			tableIndexes[tableName] = index
			result.Tables = append(result.Tables, dbschema.Table{Name: tableName})
		}
		result.Tables[index].Columns = append(result.Tables[index].Columns, column)
	}
	if err := rows.Err(); err != nil {
		return dbschema.Database{}, fmt.Errorf("iterate SQL Server columns: %w", err)
	}

	pkRows, err := db.QueryContext(ctx, `
		SELECT t.name, c.name
		FROM sys.tables t
		JOIN sys.schemas s ON s.schema_id = t.schema_id
		JOIN sys.indexes i ON i.object_id = t.object_id AND i.is_primary_key = 1
		JOIN sys.index_columns ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
		JOIN sys.columns c ON c.object_id = ic.object_id AND c.column_id = ic.column_id
		WHERE s.name = @schema
		ORDER BY t.name, ic.key_ordinal`, sql.Named("schema", input.Schema))
	if err != nil {
		return dbschema.Database{}, fmt.Errorf("read SQL Server primary keys: %w", err)
	}
	defer pkRows.Close()
	for pkRows.Next() {
		var tableName, columnName string
		if err := pkRows.Scan(&tableName, &columnName); err != nil {
			return dbschema.Database{}, fmt.Errorf("scan SQL Server primary key: %w", err)
		}
		if index, exists := tableIndexes[tableName]; exists {
			result.Tables[index].PrimaryKey = append(result.Tables[index].PrimaryKey, columnName)
		}
	}
	if err := pkRows.Err(); err != nil {
		return dbschema.Database{}, fmt.Errorf("iterate SQL Server primary keys: %w", err)
	}
	return result, nil
}
