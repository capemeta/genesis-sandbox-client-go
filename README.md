# genesis-sandbox-client-go

`genesis-sandbox-client-go` 是独立的 Go SDK 模块，作为 `genesis-sandbox` HTTP API 的唯一客户端实现。

## 设计目标

- 单一公开包：`github.com/capemeta/genesis-sandbox-client-go`
- 单一源码来源：SDK 不再和服务仓库双向复制演进
- 同时覆盖低层 HTTP Client 与高层 `Sandbox` 会话封装
- 本地联调优先使用 `go.work`，而不是在业务模块里提交 `replace`
- 协议层与手写 facade 分离：OpenAPI 快照与生成模型放在 `internal/genapi`

## 协议生成层

SDK 内部保留一份 `api/openapi.yaml` 协议快照，并通过 `internal/genapi` 生成协议模型。

重新生成：

```bash
go generate ./internal/genapi
```

设计约束：

- `internal/genapi` 只服务内部协议边界，不直接暴露给 SDK 用户
- 对外仍然由手写的 `sandbox` 包提供稳定、易用的 public API
- 少量高层易用性能力允许在 facade 层做补充，例如 `CreateSessionRequest.Env`

## 安装

```bash
go get github.com/capemeta/genesis-sandbox-client-go
```

## 快速开始

```go
client, err := sandbox.NewClient(sandbox.Config{
    BaseURL: "http://127.0.0.1:18010",
    Token:   os.Getenv("GENESIS_SANDBOX_API_KEY"),
    Timeout: 30 * time.Second,
})
if err != nil {
    return err
}

sb, err := sandbox.New(client, sandbox.WithHints("runtime.python"))
if err != nil {
    return err
}
defer sb.Close()

if err := sb.Open(ctx); err != nil {
    return err
}

result, err := sb.RunPython(ctx, `print("hello")`)
if err != nil {
    return err
}
fmt.Print(result.Stdout)
```

如果你希望完全零配置，也可以直接 `sandbox.New(client)`，由服务端按默认 Public Profile 解析；
只有在你明确想绑定某个部署内 profile 名时，才建议使用 `WithProfile(...)`。

## 端点与凭据安全约束

与其他兄弟 SDK 保持一致的端点纪律：

- `BaseURL` 必须是绝对 HTTP(S) 地址，且不含内嵌凭据、查询、片段或路径前缀
- 非 HTTPS 的 `BaseURL` 仅允许回环地址（`localhost` / `127.0.0.1` / `::1`），用于本地联调
- `Token` 拒绝首尾空白与 CR/LF，避免 Authorization 头注入
- 默认 HTTP 客户端拒绝跟随重定向（3xx 按结构化 `APIError` 返回），Bearer 只发往配置的 `BaseURL`；
  自定义 `Config.HTTPClient` 的调用方需自行保证相同的重定向纪律
- JSON 响应解码受 16 MiB 预算约束，异常服务端无法耗尽客户端内存

## 术语约定

- `profile`：显式环境名，例如 `code-polyglot-basic`、`office-basic`。只有在你明确要绑定某个部署内环境时才直接指定。
- `language`：代码执行语言，只用于 `code` 类请求，当前公共值为 `python`、`node`、`javascript`、`typescript`。shell/二进制命令执行直接走 `command`，不是 `language=shell`。
- `hints`：能力提示，用来让服务端自动选环境，例如 `runtime.python`、`runtime.node`、`tool.libreoffice`。推荐作为跨部署、跨环境的默认写法。
- `resolution_id`：`/v1/environment:resolve` 返回的环境解析票据。它不是 profile 名，也不是长期环境身份；适合在一次调用链里复用已解析好的不可变环境。

推荐优先级：

1. 零配置或跨部署兼容：不传 `profile`，必要时用 `WithHints(...)`
2. 需要指定代码语言：传 `language`
3. 需要复用同一已解析环境：传 `resolution_id`
4. 只有明确要绑定某个部署内 profile 名时，才直接 `WithProfile(...)`

## 本地源码联调

推荐在共同父目录创建 `go.work`，把服务仓库和 SDK 仓库一起纳入：

```bash
go work init ./genesis-sandbox ./genesis-sandbox-client-go
```

这样：

- 业务项目 import `github.com/capemeta/genesis-sandbox-client-go` 时可直接命中本地 SDK 源码
- 本地跑起来的 `genesis-sandbox` 服务仍然是独立源码仓库
- 不需要在仓库内提交 `replace ../genesis-sandbox-client-go`

## CI / Release

- `main` 分支 push / PR 会触发 `ci`，执行 `go test ./...`
- 推送形如 `v0.1.0` 的 tag 会触发 `release`，先验证再创建 GitHub Release
- release 会附带源码压缩包与 `.sha256` 校验文件
- 如需补发已存在 tag，可手动触发 workflow，并传入 `tag`

## 示例

- `examples/quickstart`: 推荐入口，包含 Session、文件、异步执行、Suspend/Resume
- `examples/production_sync`: `Session + WorkspaceFS` 主路径
- `examples/production_async`: 并发批处理、异步执行、共享 Session
- `examples/job_artifact_sync`: `Job + Artifact` 无状态链路
