// Package main — Job + Artifact 同步示例
//
// 演示：
//   - SubmitJob + WaitJob 执行一次性任务
//   - /workspace/output 自动回收为 Artifact
//   - UploadJobFile + input_artifact_ids 注入输入文件
//   - 使用 EnvironmentSelector 选择执行环境
//
// 这个示例只展示无状态 Job 模式。
// Session + WorkspaceFS 主路径见：./examples/production_sync/
//
// 运行：
//
//	export GENESIS_SANDBOX_BASE_URL=http://127.0.0.1:18010
//	export GENESIS_SANDBOX_API_KEY=test-token-1
//	go run ./examples/job_artifact_sync
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
	demoOutputArtifact(ctx, client)
	demoInputArtifact(ctx, client)
	log.Println("Job + Artifact sync demos completed.")
}

func demoOutputArtifact(ctx context.Context, client *sandbox.Client) {
	fmt.Println("\n=== Job Sync: Output Artifact Collection ===")
	r, err := runJob(ctx, client, sandbox.SubmitJobRequest{
		Environment: &sandbox.EnvironmentSelector{
			Hints: &sandbox.EnvHints{Capabilities: []string{"runtime.python"}},
		},
		Code: `
import os
os.makedirs("/workspace/output", exist_ok=True)
with open("/workspace/output/hello.txt", "w", encoding="utf-8") as f:
    f.write("Hello from Sandbox output artifact.")
print("artifact generated")
`,
	})
	must(err, "exec job")
	fmt.Println(r.Stdout)

	jobResult, err := client.GetJob(ctx, r.JobID)
	must(err, "get job")
	if len(jobResult.OutputArtifacts) == 0 {
		log.Fatalf("no artifacts collected for job %s", r.JobID)
	}

	artifact := jobResult.OutputArtifacts[0]
	fmt.Printf("collected artifact: ID=%s Name=%s Size=%d MIME=%s\n", artifact.ArtifactID, artifact.Name, artifact.Size, artifact.MIME)

	reader, err := client.DownloadArtifact(ctx, artifact.ArtifactID)
	must(err, "download artifact")
	defer reader.Close()

	content, err := io.ReadAll(reader)
	must(err, "read artifact")
	fmt.Printf("downloaded artifact content: %s\n", string(content))
}

func demoInputArtifact(ctx context.Context, client *sandbox.Client) {
	fmt.Println("\n=== Job Sync: Input Artifact Injection ===")
	r1, err := runJob(ctx, client, sandbox.SubmitJobRequest{
		Environment: &sandbox.EnvironmentSelector{
			Hints: &sandbox.EnvHints{Capabilities: []string{"runtime.python"}},
		},
		Code: `print("step 1 execution completed")`,
	})
	must(err, "run job 1")
	fmt.Printf("job 1 completed. job_id=%s\n", r1.JobID)

	artifact, err := client.UploadJobFile(ctx, r1.JobID, "data.csv", strings.NewReader("key,value\nitem1,100\nitem2,200\n"))
	must(err, "upload job file")
	fmt.Printf("uploaded input artifact: ID=%s Name=%s Size=%d\n", artifact.ArtifactID, artifact.Name, artifact.Size)

	r2, err := runJob(ctx, client, sandbox.SubmitJobRequest{
		Environment: &sandbox.EnvironmentSelector{
			Hints: &sandbox.EnvHints{Capabilities: []string{"runtime.python"}},
		},
		InputArtifactIDs: []string{artifact.ArtifactID},
		Code: `
from pathlib import Path
input_file = Path("/workspace/input/data.csv")
if input_file.exists():
    print("Found input file!")
    print(input_file.read_text())
else:
    print("Input file data.csv not found!")
`,
	})
	must(err, "run job 2")
	fmt.Println(r2.Stdout)
}

func runJob(ctx context.Context, client *sandbox.Client, req sandbox.SubmitJobRequest) (*sandbox.JobResult, error) {
	job, err := client.SubmitJob(ctx, req)
	if err != nil {
		return nil, err
	}
	return client.WaitJob(ctx, job.JobID)
}

func must(err error, label string) {
	if err != nil {
		log.Fatalf("%s: %v", label, err)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
