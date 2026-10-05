# Go SDK 心跳未知续租纪律验收

## 关键点、代码与验证

此前 `sandbox.go.heartbeatLoop` 续租报错后仅记录日志并等待下一个 tick；每次 tick 都是新的 POST 写请求，即使 transport 单发也违反未知结果只能先查询原身份的纪律。

| 关键点 | 代码 | 验证 |
| --- | --- | --- |
| 失败后停止所有定时续租 | `heartbeat.go.heartbeatLoop` | 真实 loopback HTTP TCP 断连、503、坏 JSON、错误 session/workspace 四种失败，等待多个 tick 后仍仅一次 renew |
| 暴露原身份与原因 | `HeartbeatRenewalError`、`Sandbox.HeartbeatError` | session/workspace 原样保留；503 status/error_code/request_id 经 errors.As/Unwrap 暴露；无额外 create/resume/reconnect |
| 不静默重启、保留失败证据 | `sandbox.go` 的 existing Open/Close + 锁内 heartbeatErr | 已打开对象重复 Open 不发送 create 或 renew；Close 后错误仍可读取，正常关闭等待已停止 goroutine |
| 正常续租可以按期继续 | 首次成功且返回相同身份，第二次失败停止 | 成功允许下一个 tick，失败后 renew 请求总数停在 2 |

仅增加独立 `heartbeat.go`、`heartbeat_test.go` 和 `sandbox.go` 小范围接线；保留其他程序已有 env/protocol 修改。未拆分或编辑旧 client.go，不改变所有写方法单发的既有 transport 政策。不自动核对后重新续租，调用方自行只读核对后作显式决策。

## 实际执行

- `go test ./...`、`go vet ./...`、`go build ./...`：Windows 宿主通过。
- `go test -race ./...`：未执行成功，工具报告 `-race requires cgo`，本机 CGO_ENABLED=0；没有安装编译器或改变环境冒充通过。
- 独立消费者 `C:/Users/caoshouling/AppData/Local/Temp/genesis-go-heartbeat-consumer`：将候选源码构件复制到消费者自己的 `sdk` 快照，go.mod 指向 `./sdk` 而非工作区，公开包 go run/vet/build 通过。实际断言 HeartbeatError、errors.As、原 session/workspace/error_code 与失败后单次续租。
- 尚未发布 v0.3.0 远程标签或公共模块代理。HTTP 测试证明心跳/错误/副作用纪律，不证明真实服务、OS 容器隔离、挂载、hard quota 或生产 heartbeat 网络环境已通过。
- 无运行数据变更、提权、安装 Docker、后端切换或自动重放。
