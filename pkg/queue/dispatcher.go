package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rjp2525/laravel-queue-go/pkg/phpserialize"
)

type Dispatcher struct {
	driver Driver
	queue  string
	format CommandFormat
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

	payloadOpts := make([]PayloadOption, 0, 8)
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

	queue := dopts.Queue
	if queue == "" {
		queue = d.queue
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

	payload := NewPayload(jobName, command, opts...)

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

func (d *Dispatcher) buildChainPayloads(chain []ChainedJob) ([]json.RawMessage, error) {
	payloads := make([]json.RawMessage, 0, len(chain))
	for _, cj := range chain {
		command, err := d.marshalCommand(cj.Job, cj.Args)
		if err != nil {
			return nil, err
		}
		payload := NewPayload(cj.Job, command)
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, data)
	}
	return payloads, nil
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
