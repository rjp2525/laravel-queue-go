package batch

import (
	"context"
	"database/sql/driver"
	"encoding/base64"
	"testing"
	"time"

	"github.com/rjp2525/laravel-queue-go/pkg/dialect"
	"github.com/rjp2525/laravel-queue-go/pkg/internal/dbtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var batchRowColumns = []string{"id", "name", "total_jobs", "pending_jobs", "failed_jobs", "failed_job_ids", "options", "cancelled_at", "created_at", "finished_at"}

// A row as Laravel's DatabaseBatchRepository stores it: unix-timestamp
// integers and PHP-serialized options.
func laravelRow(options string, failedIDs string, cancelledAt, finishedAt any) []driver.Value {
	return []driver.Value{"9d5b-batch", "import", int64(10), int64(4), int64(1), failedIDs, options, cancelledAt, int64(1760000000), finishedAt}
}

const laravelOptions = `a:2:{s:13:"allowFailures";b:1;s:4:"then";a:0:{}}`

func TestFindReadsLaravelRow(t *testing.T) {
	db, rec := dbtest.Open()
	repo, err := NewRepository(db, "", WithDialect(dialect.MySQL))
	require.NoError(t, err)
	rec.QueueRows(batchRowColumns, laravelRow(laravelOptions, `["job-1"]`, nil, int64(1760000600)))

	b, err := repo.Find(context.Background(), "9d5b-batch")
	require.NoError(t, err)

	assert.Equal(t, "import", b.Name)
	assert.Equal(t, 10, b.TotalJobs)
	assert.Equal(t, 4, b.PendingJobs)
	assert.Equal(t, []string{"job-1"}, b.FailedJobIDs)
	assert.Equal(t, time.Unix(1760000000, 0), b.CreatedAt)
	assert.False(t, b.Cancelled())
	require.True(t, b.Finished())
	assert.Equal(t, time.Unix(1760000600, 0), *b.FinishedAt)
	assert.True(t, b.AllowsFailures())
	assert.Equal(t, 60, b.Progress())
}

func TestFindDecodesBase64OptionsOnPostgres(t *testing.T) {
	db, rec := dbtest.Open()
	repo, err := NewRepository(db, "", WithDialect(dialect.Postgres))
	require.NoError(t, err)

	encoded := base64.StdEncoding.EncodeToString([]byte(laravelOptions))
	rec.QueueRows(batchRowColumns, laravelRow(encoded, `[]`, int64(1760000100), nil))

	b, err := repo.Find(context.Background(), "9d5b-batch")
	require.NoError(t, err)
	assert.Equal(t, laravelOptions, b.Options)
	assert.True(t, b.AllowsFailures())
	assert.True(t, b.Cancelled())
	assert.Contains(t, rec.Last().Query, "WHERE id = $1")
}

func TestFindToleratesNullOptionsAndPlainOptionsOnPostgres(t *testing.T) {
	db, rec := dbtest.Open()
	repo, err := NewRepository(db, "", WithDialect(dialect.Postgres))
	require.NoError(t, err)

	rec.QueueRows(batchRowColumns, laravelRow("", `[]`, nil, nil))
	rec.QueueRows(batchRowColumns, []driver.Value{"id", "n", int64(1), int64(1), int64(0), "[]", nil, nil, int64(1), nil})
	rec.QueueRows(batchRowColumns, laravelRow(laravelOptions, `[]`, nil, nil)) // written before Laravel base64-encoded

	for _, wantAllows := range []bool{false, false, true} {
		b, err := repo.Find(context.Background(), "x")
		require.NoError(t, err)
		assert.Equal(t, wantAllows, b.AllowsFailures())
	}
}

func TestCreateWritesLaravelColumnsTypes(t *testing.T) {
	for d, wantOptions := range map[dialect.Dialect]func(string) string{
		dialect.MySQL:    func(s string) string { return s },
		dialect.Postgres: func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) },
	} {
		t.Run(d.String(), func(t *testing.T) {
			db, rec := dbtest.Open()
			repo, err := NewRepository(db, "", WithDialect(d))
			require.NoError(t, err)

			before := time.Now().Unix()
			b, err := repo.Create(context.Background(), CreateOptions{Name: "import", TotalJobs: 3, AllowFailures: true})
			require.NoError(t, err)

			args := rec.Last().Args
			require.Len(t, args, 6)
			assert.Equal(t, b.ID, args[0])
			assert.Len(t, b.ID, 36)
			assert.Equal(t, "import", args[1])
			assert.Equal(t, int64(3), args[2])
			assert.Equal(t, int64(3), args[3])

			const serialized = `a:4:{s:13:"allowFailures";b:1;s:7:"onError";s:0:"";s:8:"onFinish";s:0:"";s:9:"onSuccess";s:0:"";}`
			assert.Equal(t, wantOptions(serialized), args[4], "options are PHP-serialized, base64 on PostgreSQL")
			assert.Equal(t, serialized, b.Options)
			assert.True(t, b.AllowsFailures())

			createdAt, ok := args[5].(int64)
			require.True(t, ok, "created_at is an integer column")
			assert.GreaterOrEqual(t, createdAt, before)
		})
	}
}

func TestCreateIDsAreTimeOrdered(t *testing.T) {
	db, _ := dbtest.Open()
	repo, err := NewRepository(db, "")
	require.NoError(t, err)

	first, err := repo.Create(context.Background(), CreateOptions{})
	require.NoError(t, err)
	time.Sleep(2 * time.Millisecond)
	second, err := repo.Create(context.Background(), CreateOptions{})
	require.NoError(t, err)
	assert.Less(t, first.ID, second.ID, "like Str::orderedUuid, newer batches sort last")
}

func TestIncrementFailedJobsLocksRowAndKeepsIDsUnique(t *testing.T) {
	db, rec := dbtest.Open()
	repo, err := NewRepository(db, "", WithDialect(dialect.Postgres))
	require.NoError(t, err)

	rec.QueueRows(batchRowColumns, laravelRow("", `["job-1"]`, nil, nil))
	require.NoError(t, repo.IncrementFailedJobs(context.Background(), "b", "job-2"))

	calls := rec.Calls()
	require.Len(t, calls, 2)
	assert.Contains(t, calls[0].Query, "WHERE id = $1 FOR UPDATE")
	assert.Equal(t, `UPDATE job_batches SET failed_jobs = failed_jobs + 1, failed_job_ids = $1 WHERE id = $2`, calls[1].Query)
	assert.Equal(t, `["job-1","job-2"]`, calls[1].Args[0])
	assert.Equal(t, 1, rec.Begun)
	assert.Equal(t, 1, rec.Committed)

	// The same job failing again is counted but listed once.
	rec.QueueRows(batchRowColumns, laravelRow("", `["job-1","job-2"]`, nil, nil))
	require.NoError(t, repo.IncrementFailedJobs(context.Background(), "b", "job-2"))
	assert.Equal(t, `["job-1","job-2"]`, rec.Last().Args[0])
}

func TestIncrementFailedJobsRollsBackWhenBatchIsMissing(t *testing.T) {
	db, rec := dbtest.Open()
	repo, err := NewRepository(db, "")
	require.NoError(t, err)

	err = repo.IncrementFailedJobs(context.Background(), "missing", "job-1")
	require.Error(t, err)
	assert.Equal(t, 1, rec.RolledBack)
	assert.Zero(t, rec.Committed)
}

func TestTimestampsAreWrittenAsUnixIntegers(t *testing.T) {
	db, rec := dbtest.Open()
	repo, err := NewRepository(db, "")
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, repo.Cancel(ctx, "b"))
	cancel := rec.Last()
	assert.Equal(t, `UPDATE job_batches SET cancelled_at = ?, finished_at = ? WHERE id = ?`, cancel.Query,
		"Laravel sets finished_at together with cancelled_at")
	assert.IsType(t, int64(0), cancel.Args[0])
	assert.Equal(t, cancel.Args[0], cancel.Args[1])

	_, err = repo.Prune(ctx, 24*time.Hour)
	require.NoError(t, err)
	assert.IsType(t, int64(0), rec.Last().Args[0])

	rec.QueueRows(batchRowColumns, laravelRow("", `[]`, nil, nil))
	_, err = repo.DecrementPendingJobs(ctx, "b")
	require.NoError(t, err)
	assert.IsType(t, int64(0), rec.Calls()[len(rec.Calls())-2].Args[0])
}
