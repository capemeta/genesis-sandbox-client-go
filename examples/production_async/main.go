// Package main — 并发/异步模式示例
//
// 演示：
//   - RunBatch：每个 job 独立 Sandbox（Job 模式），全部并发执行（fan-out）
//   - RunBatchThrottled：channel 信号量限制并发数，防止配额耗尽
//   - RunSequential：多任务共享同一 Sandbox（Session 模式，保持状态）
//   - RunAsync + ExecHandle：异步执行 + Wait/Cancel/Logs
//   - 错误隔离：单个 job 失败不影响其他
//   - Suspend / 再唤醒生命周期
//
// Go 语言里"异步"就是 goroutine + channel/WaitGroup，无需 async/await 关键字。
// 所有 sandbox API 调用是阻塞的，但在独立 goroutine 中运行，效果等同于异步。
//
// 运行：
//
//	export GENESIS_SANDBOX_BASE_URL=http://127.0.0.1:18010
//	export GENESIS_SANDBOX_API_KEY=test-token-1
//	go run ./examples/production_async
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	sandbox "github.com/capemeta/genesis-sandbox-client-go"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	client, err := sandbox.NewClient(sandbox.Config{
		BaseURL: getenv("GENESIS_SANDBOX_BASE_URL", "http://127.0.0.1:18010"),
		Token:   getenv("GENESIS_SANDBOX_API_KEY", "test-token-1"),
		Timeout: 60 * time.Second,
	})
	if err != nil {
		log.Fatalf("create client: %v", err)
	}

	ctx := context.Background()

	demoBatchFanout(ctx, client)
	demoBatchThrottled(ctx, client)
	demoSequentialSharedState(ctx, client)
	demoConcurrentWithCtxCancel(ctx, client)
	demoRunAsync(ctx, client)
	demoDormantSuspend(ctx, client)

	log.Println("All concurrent demos completed.")
}

// ── Demo 1: fan-out — 3 languages concurrently ───────────────────────────────

func demoBatchFanout(ctx context.Context, client *sandbox.Client) {
	fmt.Println("\n=== Concurrent: Fan-out (Python + Shell + Node) ===")
	start := time.Now()

	results, err := sandbox.RunBatch(ctx, client, []sandbox.BatchJob{
		{
			CmdOrCode: "print('python result:', 6 * 7)",
			Opts:      []sandbox.ExecOption{sandbox.WithLang("python"), sandbox.WithTimeout(30 * time.Second)},
		},
		{
			CmdOrCode: "echo 'shell result:' $(( 6 * 7 ))",
			Opts:      []sandbox.ExecOption{sandbox.WithLang("shell"), sandbox.WithTimeout(30 * time.Second)},
		},
		{
			CmdOrCode: "console.log('node result:', 6 * 7)",
			Opts:      []sandbox.ExecOption{sandbox.WithLang("javascript"), sandbox.WithTimeout(30 * time.Second)},
		},
	})
	if err != nil {
		log.Printf("RunBatch error: %v", err)
		return
	}

	for _, r := range results {
		if r.Err != nil {
			log.Printf("job[%d] failed: %v", r.Index, r.Err)
			continue
		}
		fmt.Printf("job[%d] exit=%d: %s", r.Index, r.Result.ExitCode, r.Result.Stdout)
	}
	log.Printf("Fan-out completed in %s (ran concurrently, not serially)", time.Since(start).Round(time.Millisecond))
}

// ── Demo 2: throttled batch — stay within quota ───────────────────────────────

func demoBatchThrottled(ctx context.Context, client *sandbox.Client) {
	fmt.Println("\n=== Concurrent: Throttled Batch (6 jobs, max 3 concurrent) ===")

	jobs := make([]sandbox.BatchJob, 6)
	for i := range jobs {
		jobs[i] = sandbox.BatchJob{
			CmdOrCode: fmt.Sprintf("print('task %d: result =', %d ** 2)", i, i),
			Opts:      []sandbox.ExecOption{sandbox.WithLang("python"), sandbox.WithTimeout(30 * time.Second)},
		}
	}

	// At most 3 sandboxes active at once — quota-safe
	results, err := sandbox.RunBatchThrottled(ctx, client, jobs, 3)
	if err != nil {
		log.Printf("RunBatchThrottled error: %v", err)
		return
	}

	for _, r := range results {
		if r.Err != nil {
			log.Printf("task[%d] failed: %v", r.Index, r.Err)
			continue
		}
		fmt.Printf("task[%d]: %s", r.Index, r.Result.Stdout)
	}
}

// ── Demo 3: sequential on shared sandbox (Session mode) ──────────────────────

func demoSequentialSharedState(ctx context.Context, client *sandbox.Client) {
	fmt.Println("\n=== Sequential: Shared session workspace across tasks ===")

	sb, err := sandbox.New(client, sandbox.WithHints("runtime.python"))
	if err != nil {
		log.Fatalf("new: %v", err)
	}
	defer closeSandbox(sb)

	// RunSequential requires Open() — explicit Session mode
	if err := sb.Open(ctx); err != nil {
		log.Fatalf("open: %v", err)
	}

	results, err := sandbox.RunSequential(ctx, sb, []sandbox.BatchJob{
		{CmdOrCode: `
# Task 1: initialise shared workspace state
data = {"counter": 0}
import json, pathlib
pathlib.Path('shared/shared.json').parent.mkdir(parents=True, exist_ok=True)
pathlib.Path('shared/shared.json').write_text(json.dumps(data))
print("Task 1: initialised counter =", data["counter"])
`, Opts: []sandbox.ExecOption{sandbox.WithLang("python")}},
		{CmdOrCode: `
# Task 2: increment shared workspace state
import json, pathlib
p = pathlib.Path('shared/shared.json')
data = json.loads(p.read_text())
data["counter"] += 10
p.write_text(json.dumps(data))
print("Task 2: counter now =", data["counter"])
`, Opts: []sandbox.ExecOption{sandbox.WithLang("python")}},
		{CmdOrCode: `
# Task 3: read final value from session workspace
import json, pathlib
data = json.loads(pathlib.Path('shared/shared.json').read_text())
print("Task 3: final counter =", data["counter"])
`, Opts: []sandbox.ExecOption{sandbox.WithLang("python")}},
	})
	if err != nil {
		log.Printf("RunSequential error: %v", err)
		return
	}

	for _, r := range results {
		if r.Err != nil {
			log.Printf("task[%d] failed: %v", r.Index, r.Err)
			continue
		}
		fmt.Print(r.Result.Stdout)
	}
}

// ── Demo 4: context cancellation propagation ──────────────────────────────────

func demoConcurrentWithCtxCancel(ctx context.Context, client *sandbox.Client) {
	fmt.Println("\n=== Concurrent: Context cancellation ===")

	// Give everything 10 seconds; any job not done is cancelled
	batchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	results, err := sandbox.RunBatch(batchCtx, client, []sandbox.BatchJob{
		{CmdOrCode: "print('fast job done')", Opts: []sandbox.ExecOption{sandbox.WithLang("python"), sandbox.WithTimeout(5 * time.Second)}},
		{CmdOrCode: "import time; time.sleep(30); print('slow')", Opts: []sandbox.ExecOption{sandbox.WithLang("python"), sandbox.WithTimeout(60 * time.Second)}},
	})
	if err != nil {
		log.Printf("RunBatch ctx error: %v", err)
	}

	for _, r := range results {
		if r.Err != nil {
			fmt.Printf("job[%d] err (expected for slow job): %v\n", r.Index, r.Err)
		} else {
			fmt.Printf("job[%d] done: %s\n", r.Index, r.Result.Stdout)
		}
	}
}

// ── Demo 5: RunAsync + ExecHandle (Session mode) ─────────────────────────────

func demoRunAsync(ctx context.Context, client *sandbox.Client) {
	fmt.Println("\n=== Async: RunAsync + ExecHandle.Wait ===")

	sb, err := sandbox.New(client, sandbox.WithHints("runtime.python"))
	if err != nil {
		log.Fatalf("new: %v", err)
	}
	defer closeSandbox(sb)

	// RunAsync requires Session mode
	if err := sb.Open(ctx); err != nil {
		log.Fatalf("open: %v", err)
	}

	// Submit async exec
	handle, err := sb.RunAsync(ctx, `
import time
for i in range(3):
    print(f"step {i+1}/3")
    time.sleep(0.5)
print("async task completed")
`, sandbox.WithLang("python"))
	if err != nil {
		log.Fatalf("RunAsync: %v", err)
	}
	fmt.Printf("submitted async exec: id=%s\n", handle.ID())

	// Wait for completion
	result, err := handle.Wait(ctx)
	if err != nil {
		log.Fatalf("Wait: %v", err)
	}
	fmt.Printf("async result (exit=%d):\n%s", result.ExitCode, result.Stdout)
}

// ── Demo 6: Suspend + resume lifecycle ───────────────────────────────────────

func demoDormantSuspend(ctx context.Context, client *sandbox.Client) {
	fmt.Println("\n=== Concurrent suite: Dormant create + Suspend + resume ===")
	sb, err := sandbox.New(client,
		sandbox.WithHints("runtime.python"),
		sandbox.WithWorkspaceRetention("explicit_delete", 0),
	)
	if err != nil {
		log.Fatalf("new: %v", err)
	}
	defer closeSandbox(sb)

	// Open Session mode
	if err := sb.Open(ctx); err != nil {
		log.Fatalf("open: %v", err)
	}

	fmt.Printf("after open: session=%s workspace=%s\n", sb.SessionID(), sb.WorkspaceID())

	// Upload file to workspace
	if err := sb.Upload(ctx, "notes/async.txt", strings.NewReader("async persisted before runtime\n")); err != nil {
		log.Fatalf("upload: %v", err)
	}

	// First exec
	r, err := sb.RunPython(ctx, "from pathlib import Path; print(Path('notes/async.txt').read_text())")
	if err != nil {
		log.Fatalf("first exec: %v", err)
	}
	fmt.Printf("after first exec:\n%s", r.Stdout)

	// Suspend
	if err := sb.Suspend(ctx); err != nil {
		log.Fatalf("suspend: %v", err)
	}
	fmt.Println("after suspend: runtime released")

	// Resume by executing again
	r, err = sb.RunPython(ctx, "from pathlib import Path; print('resumed:', Path('notes/async.txt').read_text().strip())")
	if err != nil {
		log.Fatalf("resume: %v", err)
	}
	fmt.Printf("after resume:\n%s", r.Stdout)
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func closeSandbox(sb *sandbox.Sandbox) {
	if err := sb.Close(); err != nil {
		log.Printf("close sandbox: %v", err)
	}
}
