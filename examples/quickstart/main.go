// Quickstart 展示应用代码推荐采用的稳定 Session 调用方式。
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	sandbox "github.com/capemeta/genesis-sandbox-client-go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client, err := sandbox.NewClient(sandbox.Config{
		BaseURL: getenv("GENESIS_SANDBOX_BASE_URL", "http://127.0.0.1:18010"),
		Token:   os.Getenv("GENESIS_SANDBOX_API_KEY"),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}
	sb, err := sandbox.New(client,
		sandbox.WithHints("runtime.python"),
		sandbox.WithIdempotencyKey(fmt.Sprintf("quickstart-%d", time.Now().UnixNano())),
	)
	if err != nil {
		log.Fatal(err)
	}
	if err := sb.Open(ctx); err != nil {
		fail(err)
	}
	defer func() {
		if err := sb.Close(); err != nil {
			log.Printf("cleanup session: %v", err)
		}
	}()

	if err := sb.Upload(ctx, "input/name.txt", strings.NewReader("Genesis")); err != nil {
		fail(err)
	}
	result, err := sb.RunPython(ctx, `
from pathlib import Path
name = Path("/workspace/input/name.txt").read_text()
print(f"hello {name}")
`)
	if err != nil {
		fail(err)
	}
	if !result.OK() {
		log.Fatalf("execution failed: exit=%d code=%s stderr=%s", result.ExitCode, result.ErrorCode, result.Stderr)
	}
	fmt.Print(result.Stdout)

	handle, err := sb.RunAsync(ctx, `print("async complete")`,
		sandbox.WithLang("python"), sandbox.WithTimeout(30*time.Second))
	if err != nil {
		fail(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if _, err := handle.Wait(waitCtx); err != nil {
		fail(err)
	}

	if err := sb.Suspend(ctx); err != nil {
		fail(err)
	}
	// 下一次执行会自动创建新 Runtime，并恢复相同 Session/Workspace context。
	if _, err := sb.RunPython(ctx, `print("resumed")`); err != nil {
		fail(err)
	}
}

func fail(err error) {
	var apiErr *sandbox.APIError
	if errors.As(err, &apiErr) {
		log.Fatalf("API error code=%s status=%d request_id=%s retryable=%v: %s",
			apiErr.ErrorCode, apiErr.StatusCode, apiErr.RequestID, apiErr.Retryable(), apiErr.Message)
	}
	log.Fatal(err)
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
