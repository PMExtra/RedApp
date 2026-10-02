# RedApp 文档

当前运行与配置说明见 [English quick start](../README.md)、[中文快速开始](../README.zh-CN.md) 与 [运维说明](operations.md)。新架构要求显式 JSON 配置和全新数据目录；配置、缓存、历史均不从旧版导入，旧目录保留，不自动改删。

- [多应用实施设计](multi-application-architecture-next.md)：规范身份、注册、协议、存储、配置、路由和实施边界。
- [v0.6.0 发布说明](multi-application-v0.6.0.md)：破坏性升级、部署配置和发布验证边界。
- [本地实施与验证](multi-application-validation.md)：已实现范围、本地整合测试和未验证事项。
- [公共前端](public-frontend.md)：公共目录、SPA、语言、站点初始化与表单生命周期。
- [Installer 维护](installers-maintenance.md)：官方原文、严格 patch、统一 descriptor、离线验证与每日 PR 边界。
- [指标历史](metrics-history.md)：采样、聚合、缺测与历史留存。
- [前端开发](../frontend/README.md)：类型检查、DOM 验证及构建。
- [第三方许可](../third_party/README.md)：依赖与分发脚本的独立许可。

[原设计](design.md)、[原验收记录](acceptance.md)、[v0.4.1 前端说明](frontend-v0.4.1.md) 与 [v0.5.0 Claude 说明](claude-code-v0.5.0.md)是旧版本资料；其中命令、路径和接口不能替代当前运维说明。新的实际验证结果应以当前分支测试日志和交付记录为准，不能用历史 PASS 代替新代码检查。
