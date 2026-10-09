# 项目规划

## v0.8.1 — 管理界面、多分类与 Tag

见 [管理界面与分类标签](admin-taxonomy-v0.8.1.md)：置顶应用搜索选择、代理与 Logo 紧凑布局、“使用上级设置”、逐字段重置与增量提交（移除集中模板重置）、预置应用删除提示、厂商卡片图标背景；schema 11 多分类（编辑器内新增、同事务落库、空自建分类自动清理）、自由 Tag 编辑与搜索、`/all` 分类计数平铺；移除基于标签的关联推荐。

后续计划（本轮不实现）：基于 Tag 的推荐。

## v0.8.0 — 已发布（`c76fa299`）

七项实现与字段/API/限额见 [配置契约](configuration-v0.8.0.md)：

1. 1A：`presets/<vendor>.yaml`、`presets/<vendor>/<app>.yaml` 与 `_taxonomy.yaml`；嵌入可信数据取代手工 JSON 权威，普通导入不得携带 distribution/信任/可执行声明。
2. 1B：schema 10、template + 稀疏 overrides / 独立 spec，显式空值与等值自定义保留，语言叶独立、有序列表和 proxy/prewarm/retention 完整叶替换，prepare→CAS→提交→发布。
3. 1C：overlay 管理 UI 与全局→Vendor→App 三层代理；inherit 使用父级，direct 截断继承，URL 为完整自身设置；模板重置只取消 override，enabled/UID/Provider/notes 独立。
4. 2A：Codex/Claude 当前 source 的最新 N 完整缓存版本保留；有效渠道、reader/writer 和无法比较版本受保护，历史 source 不清理。
5. 2B：发布平台及 HTTP 路径/清单/目录主动预热，共用授权/下载链和 15 分钟维护循环，有界单 worker，取消共享等待者不终止其他请求。
6. 3A：双语分类/标签、公开 category 搜索分页，以及共享标签和七日热门排序的最多六项关联应用。
7. 3B：ZIP/单 YAML 配置交换与 App 复制；链接/独立模式，默认排除 notes/代理凭据，排除 Hosted 二进制及运行数据，显式预览/信任/CAS/整包回滚和短成功回执。

只接受新空目录或精确 schema 10。旧 schema 2–9/未知目录不迁移、不自动删除；保留旧目录可切回对应旧程序。升级使用新的独立数据卷。未提交推送或发布；本地及远端门禁状态见 [验收记录](acceptance.md)。Not planned/backlog 仍未实施。

## v0.4.1 — 站点设置、交互与流量口径

本轮实现双语站点标题/副标题及通用页脚声明、公共导航宽度对齐、公共/后台页脚分工、剪贴板能力检测、终端用户安装说明、统一可访问下拉框、准确标注并修正制品流量计数、固定项目源码链接。逐项约定与验收边界见 [v0.4.1 说明](frontend-v0.4.1.md)。本轮 CLI 与中英桌面/窄屏 Chromium 验证通过；发布须通过精确提交 CI 和双架构镜像运行门禁，不沿用前一版本结果。

## v0.4.0 — 已发布：统一前端与公共安装入口

完成匿名应用目录、Codex CLI 安装详情、公共页与后台整体重设计、完整中英文切换、统一导航和账号交互、版本/架构页脚、本地时间快照。保留全部既有管理、历史指标与安全能力。设计与验证范围见[前端设计](frontend-v0.4.0.md)。精确提交 `e9baf78573b812e2bd67f0b86a47c6df1550d114` 的 [CI](https://github.com/PMExtra/RedApp/actions/runs/36986735859) 和[发布](https://github.com/PMExtra/RedApp/actions/runs/36987307526) 已通过。

## v0.5.0 — Claude Code CLI

已实现本地双应用模块、Claude 签名清单与原生制品授权、共享下载引擎的应用隔离、SQLite 兼容迁移、中英文安装页和管理筛选。安装器保留官方原文及最小 patch，跳过二阶段 `claude install`，通过受管入口设置 `DISABLE_UPDATES=1`，不修改官方二进制。每日官方脚本检测已启用，首次真实检测四份脚本均未变化，未创建 PR；有变化时的容器与真实 PR 写权限仍待验证。真实制品运行、原生平台、许可与发布门禁仍独立保留，见 [v0.5.0 说明](claude-code-v0.5.0.md)及[验收记录](acceptance.md#v050-claude-code-与每日安装器维护)。


## Backlog — Not planned

### HTTP gzip/br 压缩支持

状态：**Not planned**。项目传输的大多是已压缩的安装包，再增加 HTTP 压缩的收益有限，却会增加缓存表示、验证器、Range 续传与流量计数的复杂性。保留现有 identity 回源行为，不实现 gzip/br 支持。界面采用简洁的回源流量、回源速率、分发流量、分发速率名称；技术统计定义继续见[指标口径](metrics-history.md#制品流量与压缩口径)。
