// Package main — Session 模式同步示例
//
// 演示：
//   - sandbox.New + sb.Open + Close 错误处理
//   - 内置自动续租心跳（默认开启，无需手动 Renew）
//   - Open → Run → Suspend → 再唤醒（见 demoDormantSuspend）
//   - RunPython / RunShell / RunNode / Run（双模式命令）
//   - 多任务复用同一 Session（共享 Workspace 文件）
//   - Upload / Download / Files 文件操作（auto-open）
//
// 边界（避免误导）：
//   - 默认主路径：Open → Upload → Run → Download → Close。Suspend 是可选优化。
//   - Session 模式下 Run 复用容器（ExecNamedSession）；Job 模式下每次 Run 独立容器。
//   - Close 会停心跳 + 删 Session + 释放 Runtime。Workspace 按 retention 策略处理。
//   - Upload/Download/Files 首次调用时自动触发 Open()（transparent upgrade）。
//
// 运行：
//
//	export GENESIS_SANDBOX_BASE_URL=http://127.0.0.1:18010
//	export GENESIS_SANDBOX_API_KEY=test-token-1
//	go run ./examples/production_sync
package main

import (
	"context"
	"fmt"
	"io"
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
	must(err, "create client")

	ctx := context.Background()

	demoPython(ctx, client)
	demoShell(ctx, client)
	demoNode(ctx, client)
	demoCommand(ctx, client)
	demoMultiTask(ctx, client)
	demoDormantSuspend(ctx, client)

	log.Println("Session + WorkspaceFS sync demos completed.")
}

// ── Demo 1: Python (Session mode) ────────────────────────────────────────────

func demoPython(ctx context.Context, client *sandbox.Client) {
	fmt.Println("\n=== Session Sync: Python ===")
	sb, err := sandbox.New(client, sandbox.WithHints("runtime.python"))
	must(err, "new")
	defer closeSandbox(sb)
	must(sb.Open(ctx), "open")

	r, err := sb.RunPython(ctx, `
import sys, platform
print(f"Python {sys.version}")
print(f"Platform: {platform.system()}")

def fib(n):
    a, b = 0, 1
    for _ in range(n): a, b = b, a + b
    return a

print("fib(30):", fib(30))
`)
	must(err, "run")
	fmt.Println(r.Stdout)
}

// ── Demo 2: Shell (Session mode) ─────────────────────────────────────────────

func demoShell(ctx context.Context, client *sandbox.Client) {
	fmt.Println("\n=== Session Sync: Shell ===")
	sb, err := sandbox.New(client, sandbox.WithHints("runtime.shell"))
	must(err, "new")
	defer closeSandbox(sb)
	must(sb.Open(ctx), "open")

	r, err := sb.RunShell(ctx, `
#!/bin/sh
echo "=== System Info ==="
uname -a
echo "=== Disk ==="
df -h /
`)
	must(err, "run")
	fmt.Println(r.Stdout)
}

// ── Demo 3: Node.js (Session mode) ──────────────────────────────────────────

func demoNode(ctx context.Context, client *sandbox.Client) {
	fmt.Println("\n=== Session Sync: Node.js ===")
	sb, err := sandbox.New(client, sandbox.WithHints("runtime.node"))
	must(err, "new")
	defer closeSandbox(sb)
	must(sb.Open(ctx), "open")

	r, err := sb.RunNode(ctx, `
const os = require('os');
console.log('Node:', process.version, '| Platform:', os.platform());
console.log('Squares:', Array.from({length:5}, (_,i) => i*i));
`)
	must(err, "run")
	fmt.Println(r.Stdout)
}

// ── Demo 4: Raw command (Session mode) ───────────────────────────────────────

func demoCommand(ctx context.Context, client *sandbox.Client) {
	fmt.Println("\n=== Session Sync: Raw Command ===")
	sb, err := sandbox.New(client, sandbox.WithHints("runtime.python"))
	must(err, "new")
	defer closeSandbox(sb)
	must(sb.Open(ctx), "open")

	// Run without WithLang → treated as shell command
	r, err := sb.Run(ctx, `python -c "import json; print(json.dumps({'ok': True, 'n': 42}))"`)
	must(err, "run")
	fmt.Println(r.Stdout)
}

// ── Demo 5: Multi-task on one Session ────────────────────────────────────────

func demoMultiTask(ctx context.Context, client *sandbox.Client) {
	fmt.Println("\n=== Session Sync: Multi-task on one session workspace ===")
	sb, err := sandbox.New(client, sandbox.WithHints("runtime.python"))
	must(err, "new")
	defer closeSandbox(sb)

	// Upload auto-triggers Open() (transparent upgrade)
	err = sb.Upload(ctx, "shared/state.txt", strings.NewReader("hello from task 1\n"))
	must(err, "upload workspace file")

	fmt.Printf("after upload (auto-opened): session=%s workspace=%s\n",
		sb.SessionID(), sb.WorkspaceID())

	entries, err := sb.Files(ctx, ".", sandbox.WithRecursive(), sandbox.WithFileLimit(100))
	must(err, "list workspace files")
	fmt.Println("workspace entries:")
	for _, entry := range entries {
		fmt.Printf("  - %s (%s)\n", entry.Path, entry.Kind)
	}

	reader, err := sb.Download(ctx, "shared/state.txt")
	must(err, "download workspace file")
	defer reader.Close()

	content, err := io.ReadAll(reader)
	must(err, "read workspace file body")
	fmt.Printf("shared state via Download: %s", string(content))

	r, err := sb.RunPython(ctx, `
from pathlib import Path
print(Path('shared/state.txt').read_text())
`)
	must(err, "task run")
	fmt.Printf("runtime sees same workspace:\n%s", r.Stdout)
}

// ── Demo 6: Dormant + Suspend + Resume ───────────────────────────────────────
//
// 1. New() 创建 Sandbox 对象（不启动容器）。
// 2. Open() 显式进入 Session 模式（创建 Session + Workspace）。
// 3. Upload 写文件到持久 Workspace。
// 4. Run 执行命令（在容器中访问 Workspace）。
// 5. Suspend 释放容器，Session/Workspace/心跳保留。
// 6. 再次 Run 自动懒启新 Runtime，文件仍在。
// 7. Close 销毁 Session（Workspace 按 retention 策略处理）。
//
// Suspend 适用场景：
//   - Agent 多轮对话之间空闲期：文件要保留但无需占容器配额。
//   - 长流程"等用户确认/等外部回调"：Session 未结束但 Runtime 可先释放。
//   - 心跳无需特殊处理：Suspend 后心跳继续续 Session TTL。
func demoDormantSuspend(ctx context.Context, client *sandbox.Client) {
	fmt.Println("\n=== Session Sync: Dormant create + Suspend + resume ===")

	sb, err := sandbox.New(client,
		sandbox.WithHints("runtime.python"),
		sandbox.WithWorkspaceRetention("explicit_delete", 0),
	)
	must(err, "new")
	closed := false
	defer func() {
		if !closed {
			closeSandbox(sb)
		}
	}()

	// Explicitly open Session
	must(sb.Open(ctx), "open")
	fmt.Printf("after open: session=%s workspace=%s\n", sb.SessionID(), sb.WorkspaceID())

	// Upload file to workspace
	err = sb.Upload(ctx, "notes/hello.txt", strings.NewReader("persisted before runtime\n"))
	must(err, "upload while dormant")
	fmt.Println("uploaded notes/hello.txt to workspace")

	// First exec — runtime launched, workspace mounted at /workspace
	r, err := sb.RunPython(ctx, `
from pathlib import Path
print(Path('notes/hello.txt').read_text())
`)
	must(err, "first exec")
	fmt.Printf("after first exec:\n%s", r.Stdout)

	// Suspend: release container, Session + Workspace + heartbeat continue
	must(sb.Suspend(ctx), "suspend")
	fmt.Println("after suspend: runtime released, workspace preserved")

	// Download after suspend — still works (workspace exists on host)
	reader, err := sb.Download(ctx, "notes/hello.txt")
	must(err, "download after suspend")
	body, err := io.ReadAll(reader)
	_ = reader.Close()
	must(err, "read body")
	fmt.Printf("read after suspend: %q\n", strings.TrimSpace(string(body)))

	// Run again — auto-launches new Runtime, same workspace
	r, err = sb.RunPython(ctx, `
from pathlib import Path
print('resumed:', Path('notes/hello.txt').read_text().strip())
`)
	must(err, "exec after suspend (lazy resume)")
	fmt.Printf("after resume:\n%s", r.Stdout)

	workspaceID := sb.WorkspaceID()
	fmt.Printf("before Close: workspace_id=%s still exists under explicit_delete\n", workspaceID)

	// Close: stop heartbeat + delete Session + release Runtime
	must(sb.Close(), "close session")
	closed = true

	// Explicit workspace cleanup (since retention=explicit_delete)
	if err := client.DeleteWorkspace(ctx, workspaceID); err != nil {
		log.Printf("DeleteWorkspace after Close (demo cleanup): %v", err)
	} else {
		fmt.Printf("explicit DeleteWorkspace(%s) — demo cleanup done\n", workspaceID)
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func must(err error, label string) {
	if err != nil {
		log.Fatalf("%s: %v", label, err)
	}
}

func closeSandbox(sb *sandbox.Sandbox) {
	if err := sb.Close(); err != nil {
		log.Printf("close sandbox: %v", err)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
