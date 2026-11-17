// Package dialect describes the SQL differences between the databases a
// Laravel application can keep its failed_jobs and job_batches tables in.
package dialect

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// Dialect is a SQL database family.
type Dialect int

const (
	// Auto picks the dialect from the *sql.DB's driver. See Detect.
	Auto Dialect = iota
	MySQL
	Postgres
	SQLite
	SQLServer
)

func (d Dialect) String() string {
	switch d {
	case MySQL:
		return "mysql"
	case Postgres:
		return "postgres"
	case SQLite:
		return "sqlite"
	case SQLServer:
		return "sqlserver"
	}
	return "auto"
}

// Detect guesses the dialect from the Go type of db's driver, which works
// for the common drivers (lib/pq, pgx, go-sql-driver/mysql, mattn and modernc
// sqlite, microsoft/go-mssqldb). Anything it does not recognise is treated as
// MySQL, whose "?" placeholders most other drivers also accept. Wrapped
// drivers (tracing, pooling) hide the real type; pass the dialect explicitly
// for those.
func Detect(db *sql.DB) Dialect {
	if db == nil {
		return MySQL
	}
	return FromDriverType(fmt.Sprintf("%T", db.Driver()))
}

// FromDriverType maps a driver's Go type name, such as "*pq.Driver" or
// "*stdlib.Driver", to a dialect.
func FromDriverType(typeName string) Dialect {
	name := strings.ToLower(typeName)
	switch {
	case strings.Contains(name, "pq."), strings.Contains(name, "pgx"),
		strings.Contains(name, "stdlib."), strings.Contains(name, "postgres"):
		return Postgres
	case strings.Contains(name, "sqlite"):
		return SQLite
	case strings.Contains(name, "mssql"), strings.Contains(name, "sqlserver"):
		return SQLServer
	}
	return MySQL
}

// Resolve returns d, or the detected dialect when d is Auto.
func (d Dialect) Resolve(db *sql.DB) Dialect {
	if d == Auto {
		return Detect(db)
	}
	return d
}

// Rebind rewrites a query written with "?" placeholders into the dialect's
// own style: $1, $2, ... for Postgres and @p1, @p2, ... for SQL Server.
// The queries in this module contain no "?" inside string literals.
func (d Dialect) Rebind(query string) string {
	var prefix string
	switch d {
	case Postgres:
		prefix = "$"
	case SQLServer:
		prefix = "@p"
	default:
		return query
	}

	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	for i := 0; i < len(query); i++ {
		if query[i] != '?' {
			b.WriteByte(query[i])
			continue
		}
		n++
		b.WriteString(prefix)
		b.WriteString(strconv.Itoa(n))
	}
	return b.String()
}

// LockForUpdate returns the row-lock clause to append to a SELECT inside a
// transaction, or "" where the database has none (SQLite locks the whole
// database; SQL Server uses table hints instead).
func (d Dialect) LockForUpdate() string {
	switch d {
	case MySQL, Postgres:
		return " FOR UPDATE"
	}
	return ""
}
