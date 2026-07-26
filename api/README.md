`api/openapi.yaml` 是 SDK 内部使用的协议快照。

来源：

- 上游仓库：`D:\workspace\go\sandbox\genesis-sandbox`
- 源文件：`api/openapi.yaml`
- 同步基线提交：`9c76935980a24f998cdc7b3b5a9313a0d323a4ef`

约定：

- 生成代码只面向 `internal/genapi`，不直接暴露给 SDK 用户。
- 更新协议时，先同步该快照，再重新执行 `go generate ./internal/genapi`。
- 公共包 `sandbox` 继续提供稳定、手写的 facade 类型与易用 API。
