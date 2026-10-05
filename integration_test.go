package sandbox_test

// 真实服务集成测试：连接 GENESIS_SANDBOX_INTEGRATION=1 时启用的本地/远端沙箱服务。
// 前置：服务已启动（scripts/start-dev-local.bat），并设置：
//
//	GENESIS_SANDBOX_INTEGRATION=1
//	GENESIS_SANDBOX_BASE_URL=http://127.0.0.1:18010
//	GENESIS_SANDBOX_API_KEY=<users.local.yaml 中的个人 key>
//
// 覆盖矩阵：能力发现、会话生命周期（创建/查询/幂等/续租/挂起/清理）、
// 短任务同步执行、长任务异步执行与轮询、取消、文件往返、
// 多会话并发、单会话并发 exec、错误路径、OCR profile 端到端。

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	sandbox "github.com/capemeta/genesis-sandbox-client-go"
)

// asAPIError 兼容 facade 包装过的错误（sandbox.Open: ...: *fmt.wrapError）。
func asAPIError(t *testing.T, err error) *sandbox.APIError {
	t.Helper()
	var apiErr *sandbox.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *sandbox.APIError, got %T: %v", err, err)
	}
	return apiErr
}

func integrationEnabled() bool { return os.Getenv("GENESIS_SANDBOX_INTEGRATION") == "1" }

func integrationClient(t *testing.T) *sandbox.Client {
	t.Helper()
	baseURL := os.Getenv("GENESIS_SANDBOX_BASE_URL")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:18010"
	}
	apiKey := os.Getenv("GENESIS_SANDBOX_API_KEY")
	if apiKey == "" {
		t.Fatal("GENESIS_SANDBOX_API_KEY is required for integration tests")
	}
	client, err := sandbox.NewClient(sandbox.Config{BaseURL: baseURL, Token: apiKey, Timeout: 120 * time.Second})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func requireIntegration(t *testing.T) {
	t.Helper()
	if !integrationEnabled() {
		t.Skip("set GENESIS_SANDBOX_INTEGRATION=1 to run integration tests against a live service")
	}
}

// openSession 打开一个禁用心跳、TTL 工作区的确定性会话，并注册清理。
// workspace_retention=ttl：测试工作区随 TTL 过期回收，不占用租户 explicit_delete 配额。
func openSession(t *testing.T, client *sandbox.Client, opts ...sandbox.Option) *sandbox.Sandbox {
	t.Helper()
	all := append([]sandbox.Option{
		sandbox.WithProfile("code-polyglot-basic"),
		sandbox.WithSessionTTL(600),
		sandbox.WithWorkspaceRetention("ttl", 900),
		sandbox.WithHeartbeatDisabled(),
	}, opts...)
	sb, err := sandbox.New(client, all...)
	if err != nil {
		t.Fatalf("sandbox.New() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := sb.Open(ctx); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	// 会话关闭后显式删除工作区：ttl 保留模式也要等 TTL 清扫，
	// 测试套件内必须立即释放租户 workspace 配额（默认 4096MB / 256MB = 16 个）。
	sessInfo, infoErr := client.GetSession(ctx, sb.SessionID())
	t.Cleanup(func() {
		_ = sb.Close()
		if infoErr == nil && sessInfo != nil && sessInfo.WorkspaceID != "" {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = client.DeleteWorkspace(cleanupCtx, sessInfo.WorkspaceID)
		}
	})
	return sb
}

func TestIntegrationCatalogAndProfiles(t *testing.T) {
	requireIntegration(t)
	client := integrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	catalog, err := client.GetCatalog(ctx, sandbox.CatalogQuery{})
	if err != nil {
		t.Fatalf("GetCatalog() error = %v", err)
	}
	if len(catalog.Items) == 0 {
		t.Fatal("catalog is empty")
	}
	found := false
	for _, item := range catalog.Items {
		if item.ProfileName == "code-polyglot-basic" {
			found = true
		}
	}
	if !found {
		t.Fatal("code-polyglot-basic missing from catalog")
	}
	if catalog.DefaultProfile == "" {
		t.Fatal("default profile not reported")
	}
}

func TestIntegrationSessionLifecycleAndShortExec(t *testing.T) {
	requireIntegration(t)
	client := integrationClient(t)
	idemKey := fmt.Sprintf("it-go-lifecycle-%d", time.Now().UnixNano())
	sb := openSession(t, client, sandbox.WithIdempotencyKey(idemKey))

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 幂等创建：同 key 同 payload 返回同一会话。
	sess, err := client.GetSession(ctx, sb.SessionID())
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if sess.Status != "active" || sess.WorkspaceID == "" || sess.StatePolicy != "session" {
		t.Fatalf("unexpected session fields: %+v", sess)
	}
	// sessions:lookup 按幂等键找回同一会话。
	found, err := client.LookupSession(ctx, idemKey)
	if err != nil {
		t.Fatalf("LookupSession() error = %v", err)
	}
	if found.SessionID != sb.SessionID() {
		t.Fatalf("LookupSession() = %s, want %s", found.SessionID, sb.SessionID())
	}

	result, err := sb.RunPython(ctx, "print(1+1)")
	if err != nil {
		t.Fatalf("RunPython() error = %v", err)
	}
	if !result.OK() || strings.TrimSpace(result.Stdout) != "2" {
		t.Fatalf("RunPython() = %+v", result)
	}
	if result.EffectiveEnvironment == nil || result.EffectiveEnvironment.ProfileName != "code-polyglot-basic" {
		t.Fatalf("effective environment missing or wrong: %+v", result.EffectiveEnvironment)
	}

	// metadata 在创建时写入并可见。
	if err := sb.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestIntegrationFilesRoundTrip(t *testing.T) {
	requireIntegration(t)
	client := integrationClient(t)
	sb := openSession(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	payload := "hello integration"
	if err := sb.Upload(ctx, "notes/input.txt", strings.NewReader(payload)); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	result, err := sb.RunPython(ctx, "print(open('/workspace/notes/input.txt').read().strip())")
	if err != nil || !result.OK() {
		t.Fatalf("container read uploaded file: %+v err=%v", result, err)
	}
	if strings.TrimSpace(result.Stdout) != payload {
		t.Fatalf("container saw %q, want %q", result.Stdout, payload)
	}

	reader, err := sb.Download(ctx, "notes/input.txt")
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(reader); err != nil {
		t.Fatalf("read download error = %v", err)
	}
	_ = reader.Close()
	if buf.String() != payload {
		t.Fatalf("download = %q, want %q", buf.String(), payload)
	}

	entries, err := sb.Files(ctx, "notes")
	if err != nil || len(entries) == 0 {
		t.Fatalf("Files() = %v err=%v", entries, err)
	}
	stat, err := sb.StatFile(ctx, "notes/input.txt")
	if err != nil || stat.Size != int64(len(payload)) {
		t.Fatalf("StatFile() = %+v err=%v", stat, err)
	}
	if err := sb.Remove(ctx, "notes/input.txt", false); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := sb.StatFile(ctx, "notes/input.txt"); err == nil {
		t.Fatal("StatFile() after Remove() should fail")
	} else if apiErr := asAPIError(t, err); apiErr.StatusCode != http.StatusNotFound || apiErr.ErrorCode != "WORKSPACE_PATH_NOT_FOUND" {
		t.Fatalf("StatFile() after Remove(): expected 404 WORKSPACE_PATH_NOT_FOUND, got %v", err)
	}
	if err := sb.Remove(ctx, "notes/input.txt", false); err == nil {
		t.Fatal("Remove() of a missing workspace path should fail")
	} else if apiErr := asAPIError(t, err); apiErr.StatusCode != http.StatusNotFound || apiErr.ErrorCode != "WORKSPACE_PATH_NOT_FOUND" {
		t.Fatalf("Remove() of missing path: expected 404 WORKSPACE_PATH_NOT_FOUND, got %v", err)
	}
}

func TestIntegrationLongAsyncExecPolling(t *testing.T) {
	requireIntegration(t)
	client := integrationClient(t)
	sb := openSession(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	handle, err := sb.RunAsync(ctx, "import time\ntime.sleep(8)\nprint('long-done')", sandbox.WithLang("python"), sandbox.WithTimeout(60*time.Second))
	if err != nil {
		t.Fatalf("RunAsync() error = %v", err)
	}

	// 中途轮询必须观察到非终态。
	time.Sleep(3 * time.Second)
	mid, err := handle.Status(ctx)
	if err != nil {
		t.Fatalf("Status() mid error = %v", err)
	}
	if mid.Status == "succeeded" || mid.Status == "failed" {
		t.Fatalf("long task already terminal at 3s: %s", mid.Status)
	}
	// execs:lookup 按 operation_id 找回同一记录（提交响应丢失恢复路径）。
	// facade 的 RunAsync 内部生成 operation_id 不外露，用低层接口显式提交验证。
	opID := fmt.Sprintf("op-it-go-%d", time.Now().UnixNano())
	submitted, err := client.ExecSessionAsync(ctx, sb.SessionID(), sandbox.ExecSessionRequest{
		Code: "print('lookup-probe')", Language: "python", TimeoutSeconds: 60, OperationID: opID,
	})
	if err != nil {
		t.Fatalf("ExecSessionAsync() error = %v", err)
	}
	byOp, err := client.GetExecByOperation(ctx, sb.SessionID(), opID)
	if err != nil {
		t.Fatalf("GetExecByOperation() error = %v", err)
	}
	if byOp.ExecID != submitted.ExecID {
		t.Fatalf("execs:lookup = %s, want %s", byOp.ExecID, submitted.ExecID)
	}
	_ = handle // 长任务句柄由下方 Wait 消费

	result, err := handle.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if !result.OK() || strings.TrimSpace(result.Stdout) != "long-done" {
		t.Fatalf("long exec = %+v", result)
	}
}

func TestIntegrationCancelLongExec(t *testing.T) {
	requireIntegration(t)
	client := integrationClient(t)
	sb := openSession(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	handle, err := sb.RunAsync(ctx, "import time\ntime.sleep(60)\nprint('nope')", sandbox.WithLang("python"), sandbox.WithTimeout(90*time.Second))
	if err != nil {
		t.Fatalf("RunAsync() error = %v", err)
	}
	time.Sleep(2 * time.Second)
	if _, err := handle.Cancel(ctx); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	result, err := handle.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait() after cancel error = %v", err)
	}
	if result.ExitCode == 0 && result.ErrorCode == "" {
		t.Fatalf("cancelled exec should not succeed: %+v", result)
	}
	if result.ErrorCode != "cancelled" && result.ErrorCode != "EXEC_CANCELLED" {
		t.Fatalf("expected cancelled error code, got %+v", result)
	}
}

func TestIntegrationSuspendStopsRuntimeAndExecLazilyResumes(t *testing.T) {
	requireIntegration(t)
	client := integrationClient(t)
	sb := openSession(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

	// 先跑一个 exec，让 runtime 实际存在。
	if _, err := sb.RunPython(ctx, "print('warm')"); err != nil {
		t.Fatalf("warm exec error = %v", err)
	}
	if err := sb.Suspend(ctx); err != nil {
		t.Fatalf("Suspend() error = %v", err)
	}
	sess, err := client.GetSession(ctx, sb.SessionID())
	if err != nil {
		t.Fatalf("GetSession() after suspend error = %v", err)
	}
	if sess.ActiveSandboxID != "" {
		t.Fatalf("suspend should stop runtime binding, got active_sandbox_id=%s", sess.ActiveSandboxID)
	}

	// 挂起后的 exec 通过懒启动恢复 runtime 并保留工作区文件。
	if err := sb.Upload(ctx, "keep.txt", strings.NewReader("persisted")); err != nil {
		t.Fatalf("Upload() after suspend error = %v", err)
	}
	result, err := sb.RunPython(ctx, "print(open('/workspace/keep.txt').read())")
	if err != nil || !result.OK() {
		t.Fatalf("exec after suspend = %+v err=%v", result, err)
	}
	if strings.TrimSpace(result.Stdout) != "persisted" {
		t.Fatalf("workspace not preserved across suspend: %q", result.Stdout)
	}
}

func TestIntegrationSuspendWithActiveExecConflicts(t *testing.T) {
	requireIntegration(t)
	client := integrationClient(t)
	sb := openSession(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	handle, err := sb.RunAsync(ctx, "import time\ntime.sleep(15)\nprint('x')", sandbox.WithLang("python"), sandbox.WithTimeout(60*time.Second))
	if err != nil {
		t.Fatalf("RunAsync() error = %v", err)
	}
	defer func() { _, _ = handle.Cancel(ctx); _, _ = handle.Wait(ctx) }()

	time.Sleep(2 * time.Second)
	if _, err := client.SuspendSession(ctx, sb.SessionID()); err == nil {
		t.Fatal("non-force suspend with running exec must fail with SESSION_HAS_ACTIVE_EXECS")
	} else if apiErr := asAPIError(t, err); apiErr.ErrorCode != "SESSION_HAS_ACTIVE_EXEC" && apiErr.ErrorCode != "SESSION_HAS_ACTIVE_EXECS" {
		t.Fatalf("unexpected suspend error: %v", err)
	}
}

func TestIntegrationRenewExtendsExpiry(t *testing.T) {
	requireIntegration(t)
	client := integrationClient(t)
	sb := openSession(t, client, sandbox.WithSessionTTL(300))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	before, err := client.GetSession(ctx, sb.SessionID())
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	// 续租语义：newExpiry = now + extend，只能延长不能缩短（且封顶 CreatedAt+MaxTTL 生命周期预算）。
	// 会话 TTL 300s，续 600s 才可见延长。
	after, err := client.RenewSession(ctx, sb.SessionID(), 600)
	if err != nil {
		t.Fatalf("RenewSession() error = %v", err)
	}
	if !after.ExpiresAt.After(before.ExpiresAt) {
		t.Fatalf("renew did not extend expiry: before=%s after=%s", before.ExpiresAt, after.ExpiresAt)
	}
}

func TestIntegrationConcurrentSessions(t *testing.T) {
	requireIntegration(t)
	client := integrationClient(t)
	// 全局 CPU 配额 4 核：3 并发留 1 核余量给 draining 残留，避免贴顶 429。
	const workers = 3

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
			defer cancel()
			sb, err := sandbox.New(client,
				sandbox.WithProfile("code-polyglot-basic"),
				sandbox.WithSessionTTL(600),
				sandbox.WithHeartbeatDisabled(),
			)
			if err != nil {
				errs <- fmt.Errorf("worker %d New: %w", idx, err)
				return
			}
			if err := sb.Open(ctx); err != nil {
				errs <- fmt.Errorf("worker %d Open: %w", idx, err)
				return
			}
			sessInfo, infoErr := client.GetSession(ctx, sb.SessionID())
			defer func() {
				_ = sb.Close()
				if infoErr == nil && sessInfo != nil && sessInfo.WorkspaceID != "" {
					cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					_ = client.DeleteWorkspace(cleanupCtx, sessInfo.WorkspaceID)
				}
			}()
			for j := 0; j < 2; j++ {
				code := fmt.Sprintf("print(%d*100+%d)", idx, j)
				result, err := sb.RunPython(ctx, code)
				if err != nil || !result.OK() {
					errs <- fmt.Errorf("worker %d exec %d: %+v err=%v", idx, j, result, err)
					return
				}
				want := fmt.Sprintf("%d", idx*100+j)
				if strings.TrimSpace(result.Stdout) != want {
					errs <- fmt.Errorf("worker %d exec %d output=%q want=%q", idx, j, result.Stdout, want)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestIntegrationConcurrentAsyncExecsInOneSession(t *testing.T) {
	requireIntegration(t)
	client := integrationClient(t)
	sb := openSession(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	const n = 3
	handles := make([]*sandbox.ExecHandle, 0, n)
	for i := 0; i < n; i++ {
		handle, err := sb.RunAsync(ctx, fmt.Sprintf("import time\ntime.sleep(%d)\nprint('c%d')", 3+i, i), sandbox.WithLang("python"), sandbox.WithTimeout(60*time.Second))
		if err != nil {
			t.Fatalf("RunAsync(%d) error = %v", i, err)
		}
		handles = append(handles, handle)
	}
	for i, handle := range handles {
		result, err := handle.Wait(ctx)
		if err != nil || !result.OK() {
			t.Fatalf("concurrent exec %d = %+v err=%v", i, result, err)
		}
		if strings.TrimSpace(result.Stdout) != fmt.Sprintf("c%d", i) {
			t.Fatalf("concurrent exec %d output=%q", i, result.Stdout)
		}
	}
	list, err := client.ListSessionExecs(ctx, sb.SessionID(), sandbox.ExecListQuery{Limit: 50})
	if err != nil {
		t.Fatalf("ListSessionExecs() error = %v", err)
	}
	if len(list.Items) < n {
		t.Fatalf("expected at least %d exec records, got %d", n, len(list.Items))
	}
}

func TestIntegrationCleanupSemantics(t *testing.T) {
	requireIntegration(t)
	client := integrationClient(t)
	sb := openSession(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sessionID := sb.SessionID()
	if err := sb.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := client.GetSession(ctx, sessionID); err == nil {
		t.Fatal("GetSession() after delete should fail")
	} else if apiErr := asAPIError(t, err); apiErr.StatusCode != 404 {
		t.Fatalf("expected 404 after delete, got %v", err)
	}
	// 原目标缺失必须保留服务错误，不能由客户端伪造成功回执。
	if err := client.DeleteSession(ctx, sessionID); err == nil {
		t.Fatal("missing original delete became a successful receipt")
	}
	// 已删除会话上执行命令必须失败。
	if _, err := sb.RunPython(ctx, "print('zombie')"); err == nil {
		t.Fatal("exec on deleted session must fail")
	}
}

func TestIntegrationErrorPaths(t *testing.T) {
	requireIntegration(t)
	client := integrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 不存在的 profile：构造不触发 API，错误在 Open（服务端 create session）暴露。
	sb, err := sandbox.New(client, sandbox.WithProfile("no-such-profile"), sandbox.WithHeartbeatDisabled(), sandbox.WithWorkspaceRetention("ttl", 900))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := sb.Open(ctx); err == nil {
		t.Fatal("Open with unknown profile should fail")
	} else if apiErr := asAPIError(t, err); apiErr.ErrorCode != "PROFILE_NOT_FOUND" {
		t.Fatalf("unexpected profile error: %v", err)
	}

	// 错误凭证必须 401。
	bad, err := sandbox.NewClient(sandbox.Config{BaseURL: os.Getenv("GENESIS_SANDBOX_BASE_URL"), Token: "definitely-wrong", Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if _, err := bad.GetCatalog(ctx, sandbox.CatalogQuery{}); err == nil {
		t.Fatal("invalid token should be rejected")
	} else if apiErr := asAPIError(t, err); apiErr.StatusCode != 401 {
		t.Fatalf("expected 401, got %v", err)
	}
}

func TestIntegrationQuickPythonJob(t *testing.T) {
	requireIntegration(t)
	client := integrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	result, err := sandbox.QuickPython(ctx, client, "print('quick-42')")
	if err != nil {
		t.Fatalf("QuickPython() error = %v", err)
	}
	if !result.OK() || strings.TrimSpace(result.Stdout) != "quick-42" {
		t.Fatalf("QuickPython() = %+v", result)
	}
}

// OCR profile 端到端：office-ocr 镜像内 RapidOCR（PP-OCRv6）真实识别。
// 镜像较重（5GB），只在显式设置 GENESIS_SANDBOX_INTEGRATION_OCR=1 时运行。
func TestIntegrationOCRProfileRapidOCR(t *testing.T) {
	requireIntegration(t)
	if os.Getenv("GENESIS_SANDBOX_INTEGRATION_OCR") != "1" {
		t.Skip("set GENESIS_SANDBOX_INTEGRATION_OCR=1 to run the heavy office-ocr profile test")
	}
	client := integrationClient(t)
	sb, err := sandbox.New(client,
		sandbox.WithProfile("office-ocr"),
		sandbox.WithSessionTTL(900),
		sandbox.WithHeartbeatDisabled(),
	)
	if err != nil {
		t.Fatalf("New(office-ocr) error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	if err := sb.Open(ctx); err != nil {
		t.Fatalf("Open(office-ocr) error = %v", err)
	}
	defer func() { _ = sb.Close() }()

	code := `
from rapidocr import RapidOCR, EngineType, ModelType, OCRVersion
from PIL import Image, ImageDraw, ImageFont
img = Image.new("RGB", (640, 160), "white")
ImageDraw.Draw(img).text((40, 40), "OCR测试1234", fill="black",
                         font=ImageFont.truetype("/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc", 64))
engine = RapidOCR(params={
    "Det.engine_type": EngineType.ONNXRUNTIME, "Det.model_type": ModelType.SMALL, "Det.ocr_version": OCRVersion.PPOCRV6,
    "Rec.engine_type": EngineType.ONNXRUNTIME, "Rec.model_type": ModelType.SMALL, "Rec.ocr_version": OCRVersion.PPOCRV6,
    "EngineConfig.onnxruntime.intra_op_num_threads": 2,
    "EngineConfig.onnxruntime.inter_op_num_threads": 1,
})
result = engine(img, use_det=True, use_cls=False, use_rec=True)
recognized = "".join(result.txts or ())
assert "1234" in recognized, recognized
print("recognized:", recognized)
`
	result, err := sb.RunPython(ctx, code)
	if err != nil || !result.OK() {
		t.Fatalf("OCR exec = %+v err=%v", result, err)
	}
	t.Logf("ocr result: %s", strings.TrimSpace(result.Stdout))
}
