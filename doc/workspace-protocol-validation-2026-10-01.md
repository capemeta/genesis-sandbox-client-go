# Go SDK 0.3.0 工作区协议实施证据

## 关键点矩阵

| 关键点 | 实现证据 | 验证证据 |
| --- | --- | --- |
| 共享引用，不接受宿主路径或自授所有者 | `shared_storage.go` 严格 JSON DTO、opaque 引用、正安全整数；`requests.go` + `genapi_adapter.go` + `client.go` 接线 | `TestSharedBindingStrictDTO`、`TestSharedCreateBindingReachesWire` |
| 共享创建保留持久源 | 显式删除保留策略，无 TTL，资源 ID 与工作区一致 | 公开创建请求往返与错误前置拒绝 |
| 实际存储事实与关闭能力分离 | `InspectSharedStorage` 严格事实 DTO，不将 `shared_attachment=false` 自动升级 | `TestStorageInspectionReportsClosedGateWithoutGrantingAttachment`、缺字段/宿主路径拒绝 |
| prepare/seal/history/purge/lookup/resume | `workspace_control.go` 与已存在的 `client.go` lookup/resume，保持服务 v1 | `TestWorkspaceLifecycleAndLookupWire` |
| 文件可执行标志与条件更新 | 公开 `WorkspaceFileInfo.Executable`、生成映射、PATCH If-Match 与 expected_executable | false 显式传输、一次请求未知结果、响应字段往返 |
| 执行环境、稳定操作 ID 与默认环境 | 公开事实 DTO、operation_id、effective_environment 映射；客户端默认 env 传入同步/异步执行，不发送已删除的 session context env | 执行 DTO/响应往返；默认环境覆盖与调用方 map 修改隔离 |
| 未知副作用不自动重发 | 仅 GET/HEAD/OPTIONS 可重试；POST/PUT/PATCH/DELETE 均单发，使用原键 lookup 恢复 | JSON/raw 两条调用链逐个 verb 请求次数回归，POST 幂等键与 chmod 同样一次请求 |
| 文件路径缺失精确错误 | 文件删除不吸收 404；stat/remove 保留 WORKSPACE_PATH_NOT_FOUND | 单元 HTTP APIError 精确断言；真实服务集成 stat 删除后、重复 remove 同样精确断言，仍保持 opt-in 门控 |
| 源码协议真实生成 | `scripts/sync_protocol.py` 结构化读取服务两个 OpenAPI 文件，再执行 oapi-codegen v2.8.0 | 服务 bundle 与 SDK snapshot 结构一致，实际 go generate 成功 |

## 实际验证

独立模块 `GOWORK=off`：`go test -count=1 ./...` 通过，64 个测试（包含参数化子项）通过，14 个真实服务测试因未启用集成环境跳过；`go vet ./...` 与 `go build ./...` 通过。
固定 Platform Python 验证协议结构一致、脚本语法、UTF-8 无 BOM 通过。
原依赖 `oapi-codegen/runtime v1.6.0` 的 go.mod 要求 Go 1.24.0；执行 go mod tidy 将本工程原错误的 Go 1.22 声明收敛为 1.24.0。

独立消费者：`C:/Users/caoshouling/AppData/Local/Temp/genesis-go-sdk-0.3.0-validation-20261001/consumer`。
消费者引用临时 `sdk` 发布源码快照，不引用工作区源码，不使用 go.work；独立 test/vet/build 通过，共享创建与 seal 事实、缺失路径 APIError、DELETE 未知结果仅一次请求使用公开包调用。
这是本地 0.3.0 候选验收，不代表已发布远端 v0.3.0 标签或公共模块代理。

## 来源与限制

服务 `api/openapi.yaml` SHA256：`9f144f0ecfdf607dc06aee92a5eba88f051dd6463593636e8223f0d465688c91`。
服务 `api/shared-storage.openapi.yaml` SHA256：`591c87fa9b19e3153ebf64316d516e7e2bbd1b801ec18beb5885cd6415b34f4a`。
SDK 打包快照 SHA256：`582040a04225f70dcaba6e50ead00cc1637eec9776491c8d5c24c94bb12d9570`。
SDK 真实生成代码 SHA256：`6f33586e5c337490113ebb2271ce1b91e764764d7799a7971e2e89857ffb130c`。

保留并承接另一程序留下的协议快照、生成代码、lookup/resume/effective_environment 与 facade 接线，不回滚用户修改；未拆分旧 client.go 或生成大文件，新增职责模块均少于 1000 行。
HTTP fixture 验证不证明容器隔离或共享同源/硬配额通过；真实服务、真实挂载仍待实机验收。
`go test -race` 因本机 CGO_ENABLED=0 且无 GCC 无法执行，不能声称竞态检查通过。
