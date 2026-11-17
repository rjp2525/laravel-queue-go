package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rjp2525/laravel-queue-go/pkg/phpserialize"
)

type Dispatcher struct {
	driver  Driver
	queue   string
	format  CommandFormat
	version LaravelVersion

	// Result of auto-detection, see laravelVersion.
	detectMu   sync.Mutex
	detected   LaravelVersion
	lastProbed time.Time
}

// VersionDetector is implemented by drivers that can tell which Laravel
// version shares their queue. *RedisDriver implements it.
type VersionDetector interface {
	DetectLaravelVersion(ctx context.Context, queue string) (LaravelVersion, error)
}

// detectInterval is how often an auto-detecting dispatcher looks at the
// queue again while it has not found a job to learn the version from.
const detectInterval = 30 * time.Second

// ForLaravel sets the Laravel version whose payload format the dispatcher
// writes. The default, LaravelAuto, asks the driver: a RedisDriver answers
// with its configured LaravelVersion or detects it from jobs already on the
// queue. Until a version is known the latest format is written, which older
// Laravel versions accept.
func ForLaravel(v LaravelVersion) DispatcherOption {
	return func(d *Dispatcher) { d.version = v }
}

// laravelVersion resolves the payload format for a dispatch to queue.
func (d *Dispatcher) laravelVersion(ctx context.Context, queue string) LaravelVersion {
	if d.version != LaravelAuto {
		return d.version
	}
	detector, ok := d.driver.(VersionDetector)
	if !ok {
		return LatestLaravel
	}

	d.detectMu.Lock()
	defer d.detectMu.Unlock()
	if d.detected != LaravelAuto {
		return d.detected
	}
	if !d.lastProbed.IsZero() && time.Since(d.lastProbed) < detectInterval {
		return LatestLaravel
	}
	d.lastProbed = time.Now()

	// Detection is best effort: a failed probe must not fail the dispatch.
	if v, err := detector.DetectLaravelVersion(ctx, queue); err == nil && v != LaravelAuto {
		d.detected = v
		return v
	}
	return LatestLaravel
}

type DispatcherOption func(*Dispatcher)

func WithDefaultQueue(queue string) DispatcherOption {
	return func(d *Dispatcher) { d.queue = queue }
}

// WithCommandFormat sets whether dispatched jobs use PHP serialize (default) or JSON
// for the data.command field. Use FormatJSON for Go-to-Go workers or custom setups.
func WithCommandFormat(f CommandFormat) DispatcherOption {
	return func(d *Dispatcher) { d.format = f }
}

func NewDispatcher(driver Driver, opts ...DispatcherOption) *Dispatcher {
	d := &Dispatcher{
		driver: driver,
		queue:  DefaultQueueName,
		format: FormatPHP,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

func (d *Dispatcher) Dispatch(ctx context.Context, jobName string, args map[string]any, opts ...PayloadOption) error {
	return d.dispatch(ctx, jobName, args, 0, opts...)
}

func (d *Dispatcher) Later(ctx context.Context, delay time.Duration, jobName string, args map[string]any, opts ...PayloadOption) error {
	return d.dispatch(ctx, jobName, args, delay, opts...)
}

func (d *Dispatcher) DispatchWithOptions(ctx context.Context, dopts DispatchOptions) error {
	command, err := d.marshalCommand(dopts.Job, dopts.Args)
	if err != nil {
		return fmt.Errorf("serialize command: %w", err)
	}

	queue := dopts.Queue
	if queue == "" {
		queue = d.queue
	}

	payloadOpts := make([]PayloadOption, 0, 10)
	payloadOpts = append(payloadOpts, WithLaravelVersion(d.laravelVersion(ctx, queue)))
	if dopts.Delay > 0 {
		payloadOpts = append(payloadOpts, WithDelay(dopts.Delay))
	}
	if dopts.MaxTries != nil {
		payloadOpts = append(payloadOpts, WithMaxTries(*dopts.MaxTries))
	}
	if dopts.MaxExceptions != nil {
		payloadOpts = append(payloadOpts, WithMaxExceptions(*dopts.MaxExceptions))
	}
	if dopts.FailOnTimeout {
		payloadOpts = append(payloadOpts, WithFailOnTimeout(true))
	}
	if dopts.Backoff != nil {
		payloadOpts = append(payloadOpts, WithBackoff(dopts.Backoff))
	}
	if dopts.Timeout != nil {
		payloadOpts = append(payloadOpts, WithTimeout(*dopts.Timeout))
	}
	if dopts.RetryUntil != nil {
		payloadOpts = append(payloadOpts, WithRetryUntil(*dopts.RetryUntil))
	}
	if len(dopts.Tags) > 0 {
		payloadOpts = append(payloadOpts, WithTags(dopts.Tags...))
	}

	payload := NewPayload(dopts.Job, command, payloadOpts...)

	if len(dopts.Chain) > 0 {
		chainPayloads, err := d.buildChainPayloads(dopts.Chain)
		if err != nil {
			return fmt.Errorf("build chain: %w", err)
		}
		payload.Data.Command, err = d.injectChain(payload.Data.Command, chainPayloads)
		if err != nil {
			return fmt.Errorf("inject chain: %w", err)
		}
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	if dopts.Delay > 0 {
		_, err = d.driver.Later(ctx, queue, dopts.Delay, data)
	} else {
		_, err = d.driver.Push(ctx, queue, data)
	}
	return err
}

func (d *Dispatcher) dispatch(ctx context.Context, jobName string, args map[string]any, delay time.Duration, opts ...PayloadOption) error {
	command, err := d.marshalCommand(jobName, args)
	if err != nil {
		return fmt.Errorf("serialize command: %w", err)
	}

	defaults := []PayloadOption{WithLaravelVersion(d.laravelVersion(ctx, d.queue))}
	if delay > 0 {
		defaults = append(defaults, WithDelay(delay))
	}
	payload := NewPayload(jobName, command, append(defaults, opts...)...)

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	if delay > 0 {
		_, err = d.driver.Later(ctx, d.queue, delay, data)
	} else {
		_, err = d.driver.Push(ctx, d.queue, data)
	}
	return err
}

func (d *Dispatcher) marshalCommand(jobName string, args map[string]any) (string, error) {
	switch d.format {
	case FormatJSON:
		return marshalCommandJSON(jobName, args)
	default:
		return phpserialize.MarshalObject(jobName, args)
	}
}

func marshalCommandJSON(jobName string, args map[string]any) (string, error) {
	payload := make(map[string]any, len(args)+1)
	payload["commandName"] = jobName
	for k, v := range args {
		payload[k] = v
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("json marshal command: %w", err)
	}
	return string(b), nil
}

// buildChainPayloads serializes the jobs that follow the first one in a
// chain. Laravel stores them on the first job's "chained" property as
// serialized commands (Queueable::chain runs serialize() on each job) and
// later runs unserialize() on the next entry, so each element must be a
// command in the dispatcher's format, not a queue payload.
func (d *Dispatcher) buildChainPayloads(chain []ChainedJob) ([]json.RawMessage, error) {
	commands := make([]json.RawMessage, 0, len(chain))
	for _, cj := range chain {
		command, err := d.marshalCommand(cj.Job, cj.Args)
		if err != nil {
			return nil, fmt.Errorf("chained job %s: %w", cj.Job, err)
		}
		commands = append(commands, json.RawMessage(command))
	}
	return commands, nil
}

func (d *Dispatcher) injectChain(command string, chain []json.RawMessage) (string, error) {
	chainStrs := make([]any, len(chain))
	for i, c := range chain {
		chainStrs[i] = string(c)
	}

	switch d.format {
	case FormatJSON:
		var obj map[string]any
		if err := json.Unmarshal([]byte(command), &obj); err != nil {
			return "", fmt.Errorf("inject chain: unmarshal JSON command: %w", err)
		}
		obj["chained"] = chainStrs
		b, err := json.Marshal(obj)
		if err != nil {
			return "", fmt.Errorf("inject chain: marshal JSON command: %w", err)
		}
		return string(b), nil
	default:
		decoded, err := phpserialize.Decode(command)
		if err != nil {
			return "", fmt.Errorf("inject chain: decode PHP command: %w", err)
		}
		obj, ok := decoded.(*phpserialize.Object)
		if !ok {
			return "", fmt.Errorf("inject chain: decoded command is not an object")
		}
		obj.Properties["chained"] = chainStrs
		return phpserialize.Encode(obj)
	}
}
