package dialect

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFromDriverType(t *testing.T) {
	for typeName, want := range map[string]Dialect{
		"*pq.Driver":            Postgres, // github.com/lib/pq
		"*stdlib.Driver":        Postgres, // github.com/jackc/pgx/v5/stdlib
		"*mysql.MySQLDriver":    MySQL,    // github.com/go-sql-driver/mysql
		"*sqlite3.SQLiteDriver": SQLite,   // github.com/mattn/go-sqlite3
		"*sqlite.Driver":        SQLite,   // modernc.org/sqlite
		"*mssql.Driver":         SQLServer,
		"*something.Else":       MySQL,
	} {
		assert.Equal(t, want, FromDriverType(typeName), typeName)
	}
}

func TestRebind(t *testing.T) {
	const q = `INSERT INTO t (a, b, c) VALUES (?, ?, ?)`
	assert.Equal(t, q, MySQL.Rebind(q))
	assert.Equal(t, q, SQLite.Rebind(q))
	assert.Equal(t, `INSERT INTO t (a, b, c) VALUES ($1, $2, $3)`, Postgres.Rebind(q))
	assert.Equal(t, `INSERT INTO t (a, b, c) VALUES (@p1, @p2, @p3)`, SQLServer.Rebind(q))
	assert.Equal(t, `SELECT 1`, Postgres.Rebind(`SELECT 1`))
}

func TestRebindManyPlaceholders(t *testing.T) {
	q := `SELECT ` + "?,?,?,?,?,?,?,?,?,?,?"
	assert.Equal(t, `SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11`, Postgres.Rebind(q))
}

func TestLockForUpdate(t *testing.T) {
	assert.Equal(t, " FOR UPDATE", MySQL.LockForUpdate())
	assert.Equal(t, " FOR UPDATE", Postgres.LockForUpdate())
	assert.Empty(t, SQLite.LockForUpdate())
	assert.Empty(t, SQLServer.LockForUpdate())
}

func TestDetectNilDB(t *testing.T) {
	assert.Equal(t, MySQL, Detect(nil))
	assert.Equal(t, Postgres, Postgres.Resolve(nil))
	assert.Equal(t, MySQL, Auto.Resolve(nil))
}
