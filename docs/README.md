# RedApp 文档

当前运行与配置说明见 [English quick start](../README.md)、[中文快速开始](../README.zh-CN.md) 与 [运维说明](operations.md)。当前 0.8.0 本地候选的七阶段实现见 [配置契约](configuration-v0.8.0.md)，验证结果见 [验收记录](acceptance.md)。SQLite schema 10 只接受新空目录或精确当前目录，旧 schema 2–9/未知目录不迁移、不自动删除；保留旧目录可切回对应旧程序。带旧版本号的文档只描述历史版本。

- [多应用实施设计](multi-application-architecture-next.md)：规范身份、注册、协议、存储、配置、路由和实施边界。
- [v0.6.0 发布说明](multi-application-v0.6.0.md)：破坏性升级、部署配置和发布验证边界。
- [本地实施与验证](multi-application-validation.md)：已实现范围、本地整合测试和未验证事项。
- [公共前端](public-frontend.md)：公共目录、SPA、语言、站点初始化与表单生命周期。
- [Installer 维护](installers-maintenance.md)：官方原文、严格 patch、统一 descriptor、离线验证与每日 PR 边界。
- [指标历史](metrics-history.md)：采样、聚合、缺测与历史留存。
- [前端开发](../frontend/README.md)：类型检查、DOM 验证及构建。
- [第三方许可](../third_party/README.md)：依赖与分发脚本的独立许可。

[原设计](design.md)、[原验收记录](acceptance.md)、[v0.4.1 前端说明](frontend-v0.4.1.md) 与 [v0.5.0 Claude 说明](claude-code-v0.5.0.md)是旧版本资料；其中命令、路径和接口不能替代当前运维说明。新的实际验证结果应以当前分支测试日志和交付记录为准，不能用历史 PASS 代替新代码检查。

- [CI 与发布产物](ci-release.md)：可信产物复用、缓存、保留期限与失败恢复。
