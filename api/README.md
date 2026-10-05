`api/openapi.yaml` 是 SDK 内部使用的协议快照。

来源：

- 上游仓库：`D:\Work\workspace\go\sandbox\genesis-sandbox`
- 源文件：`api/openapi.yaml`、`api/shared-storage.openapi.yaml` 与 `api/execution-governance.openapi.yaml`
- 同步方式：`scripts/sync_protocol.py` 结构化解析当前服务协议，将共享 Schema 引用打包到单文件快照。

约定：

- 生成代码只面向 `internal/genapi`，不直接暴露给 SDK 用户。
- 更新协议时，先运行打包脚本同步该快照，再执行 `go generate ./internal/genapi`（生成器 v2.8.0）。
- 公共包 `sandbox` 继续提供稳定、手写的 facade 类型与易用 API。
