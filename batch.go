package sandbox

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// BatchJob defines a single job to run.
type BatchJob struct {
	CmdOrCode string       // command string or source code
	Opts      []ExecOption // per-job options (WithLang, WithTimeout, etc.)
}

// BatchResult holds the outcome of one BatchJob.
type BatchResult struct {
	Index  int
	Result *ExecResult
	Err    error
}

// OK returns true when the job succeeded without error.
func (r *BatchResult) OK() bool {
	return r.Err == nil && r.Result != nil && r.Result.OK()
}

// RunBatch creates one Sandbox per job (Job mode) and runs all jobs CONCURRENTLY.
// Each job uses SubmitJob/WaitJob independently. Individual failures do not abort other jobs.
func RunBatch(
	ctx context.Context,
	client *Client,
	jobs []BatchJob,
	baseOpts ...Option,
) ([]BatchResult, error) {
	if len(jobs) == 0 {
		return nil, nil
	}

	results := make([]BatchResult, len(jobs))
	var wg sync.WaitGroup

	for i, job := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sb, err := New(client, baseOpts...)
			if err != nil {
				results[i] = BatchResult{Index: i, Err: fmt.Errorf("job[%d] init: %w", i, err)}
				return
			}
			result, err := sb.Run(ctx, job.CmdOrCode, job.Opts...)
			if closeErr := sb.Close(); closeErr != nil {
				closeErr = fmt.Errorf("job[%d] cleanup: %w", i, closeErr)
				err = errors.Join(err, closeErr)
			}
			results[i] = BatchResult{Index: i, Result: result, Err: err}
		}()
	}

	wg.Wait()
	return results, nil
}

// RunBatchThrottled is like RunBatch but limits concurrent sandboxes to maxConcurrent.
func RunBatchThrottled(
	ctx context.Context,
	client *Client,
	jobs []BatchJob,
	maxConcurrent int,
	baseOpts ...Option,
) ([]BatchResult, error) {
	if len(jobs) == 0 {
		return nil, nil
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}

	results := make([]BatchResult, len(jobs))
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for i, job := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[i] = BatchResult{Index: i, Err: ctx.Err()}
				return
			}
			defer func() { <-sem }()

			sb, err := New(client, baseOpts...)
			if err != nil {
				results[i] = BatchResult{Index: i, Err: fmt.Errorf("job[%d] init: %w", i, err)}
				return
			}
			result, err := sb.Run(ctx, job.CmdOrCode, job.Opts...)
			if closeErr := sb.Close(); closeErr != nil {
				closeErr = fmt.Errorf("job[%d] cleanup: %w", i, closeErr)
				err = errors.Join(err, closeErr)
			}
			results[i] = BatchResult{Index: i, Result: result, Err: err}
		}()
	}

	wg.Wait()
	return results, nil
}

// RunSequential runs all jobs on a single opened Sandbox (Session mode, shared state).
// The Sandbox must already be Open()'d by the caller.
func RunSequential(
	ctx context.Context,
	sb *Sandbox,
	jobs []BatchJob,
) ([]BatchResult, error) {
	if !sb.IsOpen() {
		return nil, ErrNotOpened
	}
	results := make([]BatchResult, len(jobs))
	for i, job := range jobs {
		if ctx.Err() != nil {
			results[i] = BatchResult{Index: i, Err: ctx.Err()}
			continue
		}
		result, err := sb.Run(ctx, job.CmdOrCode, job.Opts...)
		results[i] = BatchResult{Index: i, Result: result, Err: err}
		if err != nil {
			sb.logf("sequential job[%d] failed: %v", i, err)
		}
	}
	return results, nil
}
