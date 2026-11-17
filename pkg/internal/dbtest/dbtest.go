// Package dbtest provides a recording database/sql driver for tests, so the
// SQL this module sends can be checked without a database server.
package dbtest

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"sync"
)

// Call is one statement the code under test sent to the database.
type Call struct {
	Query string
	Args  []any
}

// Recorder is a fake database. It records every statement and answers
// queries with the rows queued through QueueRows.
type Recorder struct {
	mu      sync.Mutex
	calls   []Call
	results []rows
	// Begun, Committed and RolledBack count transaction boundaries.
	Begun, Committed, RolledBack int
}

type rows struct {
	columns []string
	values  [][]driver.Value
}

// Open returns a *sql.DB backed by a new Recorder.
func Open() (*sql.DB, *Recorder) {
	r := &Recorder{}
	return sql.OpenDB(connector{r}), r
}

// QueueRows sets the result of the next query. Queries are answered in the
// order results were queued; a query with nothing queued returns no rows.
func (r *Recorder) QueueRows(columns []string, values ...[]driver.Value) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.results = append(r.results, rows{columns: columns, values: values})
}

// Calls returns the statements recorded so far.
func (r *Recorder) Calls() []Call {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Call(nil), r.calls...)
}

// Last returns the most recent statement.
func (r *Recorder) Last() Call {
	calls := r.Calls()
	if len(calls) == 0 {
		return Call{}
	}
	return calls[len(calls)-1]
}

func (r *Recorder) record(query string, args []driver.NamedValue) {
	call := Call{Query: query}
	for _, a := range args {
		call.Args = append(call.Args, a.Value)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

func (r *Recorder) next() rows {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.results) == 0 {
		return rows{}
	}
	res := r.results[0]
	r.results = r.results[1:]
	return res
}

type connector struct{ r *Recorder }

func (c connector) Connect(context.Context) (driver.Conn, error) { return &conn{c.r}, nil }
func (c connector) Driver() driver.Driver                        { return fakeDriver{} }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return nil, driver.ErrBadConn }

type conn struct{ r *Recorder }

func (c *conn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c *conn) Close() error                        { return nil }
func (c *conn) Begin() (driver.Tx, error)           { return c.BeginTx(context.Background(), driver.TxOptions{}) }

func (c *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	c.r.mu.Lock()
	defer c.r.mu.Unlock()
	c.r.Begun++
	return tx{c.r}, nil
}

func (c *conn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.r.record(query, args)
	return driver.RowsAffected(1), nil
}

func (c *conn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.r.record(query, args)
	res := c.r.next()
	return &resultRows{rows: res}, nil
}

type tx struct{ r *Recorder }

func (t tx) Commit() error {
	t.r.mu.Lock()
	defer t.r.mu.Unlock()
	t.r.Committed++
	return nil
}

func (t tx) Rollback() error {
	t.r.mu.Lock()
	defer t.r.mu.Unlock()
	t.r.RolledBack++
	return nil
}

type resultRows struct {
	rows rows
	pos  int
}

func (r *resultRows) Columns() []string { return r.rows.columns }
func (r *resultRows) Close() error      { return nil }

func (r *resultRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows.values) {
		return io.EOF
	}
	copy(dest, r.rows.values[r.pos])
	r.pos++
	return nil
}
