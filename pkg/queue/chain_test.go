package queue

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rjp2525/laravel-queue-go/v2/pkg/phpserialize"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type chainCaptureDriver struct {
	mockDriver
	pushed []byte
}

func (d *chainCaptureDriver) Push(_ context.Context, _ string, payload []byte) (string, error) {
	d.pushed = payload
	return "", nil
}

func dispatchChain(t *testing.T, opts ...DispatcherOption) *Job {
	t.Helper()
	driver := &chainCaptureDriver{}
	d := NewDispatcher(driver, opts...)

	require.NoError(t, d.DispatchWithOptions(context.Background(), DispatchOptions{
		Job:  `App\Jobs\ProcessPodcast`,
		Args: map[string]any{"podcastId": 7},
		Chain: []ChainedJob{
			{Job: `App\Jobs\OptimizePodcast`, Args: map[string]any{"podcastId": 7, "quality": "high"}},
			{Job: `App\Jobs\ReleasePodcast`, Args: map[string]any{"podcastId": 7}},
		},
	}))

	job, err := ParseJob(context.Background(), &RawJob{Body: string(driver.pushed), Queue: "default"}, driver)
	require.NoError(t, err)
	return job
}

func TestChainedJobsAreSerializedCommands(t *testing.T) {
	job := dispatchChain(t)

	chained := job.ChainedJobs()
	require.Len(t, chained, 2)

	// Each entry is what PHP's serialize($job) would produce: an object, not
	// a JSON queue payload.
	for i, want := range []string{`App\Jobs\OptimizePodcast`, `App\Jobs\ReleasePodcast`} {
		require.True(t, strings.HasPrefix(chained[i], "O:"), "entry %d: %s", i, chained[i])
		decoded, err := phpserialize.Decode(chained[i])
		require.NoError(t, err)
		obj, ok := decoded.(*phpserialize.Object)
		require.True(t, ok)
		assert.Equal(t, want, obj.ClassName)
		assert.Equal(t, int64(7), obj.Properties["podcastId"])
	}
}

func TestChainedJobsInJSONFormatAreCommands(t *testing.T) {
	job := dispatchChain(t, WithCommandFormat(FormatJSON))

	chained := job.ChainedJobs()
	require.Len(t, chained, 2)

	var next map[string]any
	require.NoError(t, json.Unmarshal([]byte(chained[0]), &next))
	assert.Equal(t, `App\Jobs\OptimizePodcast`, next["commandName"])
	assert.Equal(t, "high", next["quality"])
	assert.NotContains(t, next, "uuid", "a command, not a queue payload")
}

// TestChainIsReadableByPHP runs the pushed command through PHP the way
// Laravel's CallQueuedHandler and Queueable::dispatchNextJobInChain do.
func TestChainIsReadableByPHP(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php not installed")
	}

	job := dispatchChain(t)

	script := filepath.Join(t.TempDir(), "chain.php")
	require.NoError(t, os.WriteFile(script, []byte(`<?php
namespace App\Jobs;

trait Queueable { public $chained = []; }
class ProcessPodcast  { use Queueable; public $podcastId; }
class OptimizePodcast { use Queueable; public $podcastId; public $quality; }
class ReleasePodcast  { use Queueable; public $podcastId; }

$command = unserialize(stream_get_contents(STDIN));
$seen = [get_class($command)];
while (! empty($command->chained)) {
    // Queueable::dispatchNextJobInChain()
    $remaining = $command->chained;
    $next = unserialize(array_shift($remaining));
    if (! is_object($next)) {
        fwrite(STDERR, "chained entry does not unserialize to a job\n");
        exit(1);
    }
    $next->chained = $remaining;
    $command = $next;
    $seen[] = get_class($command).(isset($command->quality) ? ':'.$command->quality : '');
}
echo implode(' -> ', $seen);
`), 0o600))

	cmd := exec.Command(php, script)
	cmd.Stdin = strings.NewReader(job.Payload().Data.Command)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	assert.Equal(t, `App\Jobs\ProcessPodcast -> App\Jobs\OptimizePodcast:high -> App\Jobs\ReleasePodcast`, string(out))
}
