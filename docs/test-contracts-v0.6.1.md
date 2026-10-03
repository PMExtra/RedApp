# v0.6.1 测试契约审计

本轮从 v0.6.0 的 `bd91176a98aa808d70724589da2dfbf295c1d3fe` 开始，集中处理失效入口、可能误通过的协议负例、安装器重复组合和前端会话竞态。不更换测试框架，不修改安装器材料或数据库格式。

## 删除与合并

- 删除旧浏览器脚本及文档中的运行命令。脚本依赖已删除的 `records`/`versions` 表、旧路由和 43 项指标，不能验证当前 41 项指标的 SPA。历史浏览器报告保留其版本边界，不作为当前验收证据。
- Claude Shell 原先在平台 × 目标 × 解析器的每个组合重复整套启动、重装和升级。现在平台选择只验证 Linux glibc/musl、Darwin 和 Rosetta 的目标资源；目标解析单独验证默认/latest/stable/显式版本和非法输入；代表场景验证参数、退出码、更新禁用环境、重复安装 inode、失败升级保留旧入口及成功升级。
- 两个 100-reader 测试没有合并：`Test100SimultaneousFirstAcquisitionsCreateOneWriter` 验证首次获取竞争只创建一个 writer；`TestSharedStreaming100LateSlowAndCancelled` 验证已有 writer 的迟到、慢速与取消读者。

## 补强与修复

协议测试为每个负例重新构造有效 fixture，再只改变目标字段。Claude 的不安全 binary 不再携带前一例的版本不匹配；Claude/Codex 顶层和嵌套重复键均放在其余字段有效的材料中，重复值相同，普通 JSON 解析后仍能通过其余验证。签名测试独立保留。

在临时副本中分别停用 Claude/Codex 重复键检查、Claude 版本匹配及 binary 白名单检查，对应负例均失败；生产验证代码没有修改。这样可以确认失败来自目标契约，而不是空平台/空 assets 等无关条件。

Shell 测试要求实际安装的 curl、wget、jq。wget 场景的受控 PATH 中没有 curl，包装器只限制 loopback 目标并执行真实 wget；下载调用留有断言。curl/wget 与 jq/内置解析的每种组合均验证坏渠道、坏清单、摘要不符、下载错误，以及渠道/清单/制品三个阶段的跳转拒绝。失败后检查入口和已有版本未改动、临时目录清理、无跳转逃逸。平台为模拟值，下载并执行的是无害桩。

前端新增测试先在旧代码上复现以下错误，再加入最小 API 修复：

- 旧请求的响应头或 JSON 延迟至新登录成功后返回 401，导致新会话注销、CSRF 清空。请求现在捕获会话代次；登录、会话确认和注销更新代次，即使 token 文本相同，旧请求也不能影响新会话。
- fetch 已返回、JSON 尚未完成时取消，旧代码仍返回成功数据或调用 401 处理器。现在解析完成后先检查取消和会话归属，再返回数据或处理错误。
- 当前会话的 401 仍使管理内容和账号入口消失，清除 CSRF，进入登录页并停止后续轮询；原有注销后迟到 200 的丢弃检查保留。

## 验证与边界

本地验证全部通过，使用 Go 1.27.1、Node 24.19.0、锁定 npm 依赖及自行重建的嵌入资源，入口如下：

```sh
make build REVISION=v0.6.1-local-validation
make check test
cd frontend && npm run typecheck && TZ=America/Los_Angeles npm test
# 返回仓库根目录：
python3 scripts/test-data-cli.py
python3 scripts/test-http-cli.py
sh scripts/test-docker-local.sh
git diff --check
```

完整 Go race/vet 覆盖共享下载、应用隔离、签名、续传、GC、崩溃恢复及指标；安装器/更新器/维护回归覆盖失败保护。真实 CLI 验证显式配置、旧/未知目录原样保护、权限失败、canonical/deep 路由、会话、应用设置 CAS、PUBLIC_URL 持久化与重启。前端全量 DOM 在非 UTC 时区执行，包含历史图精确读数与取消、设置草稿及路由行为。隔离容器使用无网络、非 root、只读根和新命名卷，验证健康、实例锁、SIGKILL/重启及重建持久性。

本地容器入口仅重建离线 runtime 层。完整 Dockerfile 及原生 Linux amd64/arm64 在精确 main 提交的 CI 执行；Windows 上分别执行 PowerShell 7 和 5.1 的安装器/启动入口回归。只有全部成功后才创建 `v0.6.1`，发布流程再按 manifest 子 digest 分别运行两种架构并验证版本、健康、持久化、实例锁和恢复。实际远端状态以该提交与标签的 Actions 记录为准，不能沿用 v0.6.0 结果。

安装器 upstream、patch、generated、信任材料逐字节与基线一致。没有 GUI、截图、官方 Claude 二进制或用户生产部署操作；这些不包含在通过声明中。
