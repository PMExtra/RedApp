# v0.8.1 管理界面、多分类与 Tag

本文记录 v0.8.1 在 0.8.0（提交 `c76fa299`）之上的变更：后台八项界面改进、多分类、自由 Tag 及其搜索，并移除基于 Tag 的关联推荐。0.8.0 其余契约（模板覆盖、三级代理、预热、保留最新 N、配置交换与复制）仍以 [0.8.0 配置契约](configuration-v0.8.0.md)为准，其中“3A 分类、标签与公开关联”被本文取代。验证结果见[验收记录](acceptance.md)。

## 升级边界

SQLite schema 升为 **11**，只接受新空目录或精确 schema 11。包括已发布 0.8.0（schema 10）在内的旧库与未知目录在任何写入前只读拒绝，不迁移、不改写、不删除，也不另建空库继续运行。升级须停止旧实例、保留旧目录原样，并用新的空数据目录/卷启动；回退时用 0.8.0 程序打开保留的旧目录。部署配置文件仍为 schema_version 1。

1.0 前的破坏性变更：

- App 字段 `category`（单值）改为 `categories`（ID 集合）；`tags` 改为自由文本，不再引用字典。管理 API、公开 DTO、预置 YAML 与交换文档同步变更，旧 0.8.0 导出包中的 `category` 或 Tag 字典无法导入。
- `presets/_taxonomy.yaml` 只含 `spec.categories`；`tags` 字典已删除。
- 删除 `/admin/api/taxonomy*`、`/api/apps/<vendor>/<app>/related`、`GET/POST /admin/api/apps/<vendor>/<app>/template` 与 `/admin/api/vendors/<vendor>/template`。
- 新增依赖 `golang.org/x/text`（NFC 规范化与 Unicode 大小写折叠），许可见 [third_party](../third_party/README.md)。

## 后台界面

- 置顶应用：搜索应用（跨厂商，名称/完整键/Tag）后从候选项添加；已置顶项不可重复选择，保留拖动排序和移除。
- 代理：模式下拉框与完整 URL 输入框同行紧凑排列，窄容器自动换行，URL 始终可完整编辑。“使用上级设置”（Use parent setting）明确跟随上级，包括上级为直连的情况。
- Logo：默认 Logo 与英文/中文 Logo 并排为独立卡片，各自显示预览、上传、移除和重置；未设置的语言 Logo 显示“使用默认 Logo”，回退语义不变。
- 逐字段重置：移除成片的“模板/自定义”开关与集中模板重置。绑定模板的字段仅在已有自定义覆盖或草稿已修改时显示“重置”。重置把草稿恢复为模板默认值；若该字段原有覆盖，则在待提交变更中记录 `unset`，否则只撤销草稿修改。保存只提交被编辑（`set`）或重置（`unset`）的字段，带配置 revision；未修改字段保持继承，显式空值、等值自定义与继承状态互不混同。409 保留草稿。
- 预置模板应用在厂商应用表中仍不可删除；禁用的删除按钮由可聚焦外层承载悬停/聚焦提示“预置模板不可删除，可以禁用”。
- 应用管理首页厂商卡片中的 App 图标在浅色下无独立底色；仅 `prefers-color-scheme: dark` 时使用保护性背景，不涉及整站深色模式。

## 多分类

App 的 `categories` 是分类 ID 集合，作为一个完整字段覆盖和重置；保存时排序去重，最多 32 个。`application_categories(app_uid, category_id)` 以复合主键防止重复关联。分类有双语名称；内置分类来自 `_taxonomy.yaml`，名称支持逐语言覆盖与重置。

新分类在 App 编辑器中输入名称后按 Enter 加入草稿，随 App 保存统一落库，取消或重新加载不会留下分类。配置 PATCH 示例：

```json
{"revision": 7, "set": {"categories": ["tools"]}, "unset": [], "new_categories": ["效率工具"]}
```

`new_categories` 要求同一请求设置 `categories`。服务端在同一配置事务中解析：名称经 NFC、去首尾空白后按大小写不敏感方式与已有分类的任一语言名称比较；唯一命中则复用其 ID，多个命中返回 409 `CATEGORY_AMBIGUOUS` 且不绑定，未命中则新建（两种语言名称相同，最长 64 字符）。新 ID 创建后不变：纯 ASCII 名称使用可读 slug（冲突加序号），否则为 `category-<随机>`。创建、绑定与配置更新同一事务提交；revision 冲突、校验失败或任一错误整体回滚。

每次配置事务（保存、删除 App、导入、复制、模板协调）结束前清理自建分类：没有任何未删除 App（包括禁用 App）使用、也没有当前模板引用的非内置分类被删除；内置分类及模板定义保留，字段重置可恢复模板分类。公开分类修订随关联或清理推进，App 运行版本与 source epoch 不变。

管理 API（登录、Origin、CSRF 边界不变）：`GET /admin/api/categories?q=&page=&limit=` 返回分类及使用该分类的未删除 App 数；`PATCH /admin/api/categories/<id>` 只接受 `name.en`/`name.zh-CN` 的 set/unset 与 revision CAS。没有单独创建或删除接口；后台“管理分类”页（`/admin/categories`）只用于改名。

## 自由 Tag

Tag 是每个 App 自己的文本集合，不需要全局字典。`#` 只是界面前缀：保存时去除前导 `#` 与首尾空白并做 NFC，拒绝空值和控制字符；大小写不敏感去重（保留首次写法与顺序）；每个 Tag 最多 64 字符，每个 App 最多 50 个。Tag 作为一个完整字段参与模板覆盖、重置、复制和导入导出。

编辑器把所有 Tag 放在一个输入框样式容器中：点击加号新增带固定 `#` 的可编辑项，失焦或 Enter 后变为只读并带删除按钮，Escape 取消；中文输入法组合期间的 Enter 不提交。保存表单前先提交仍在编辑的 Tag；重新加载、重置或切换 App 时丢弃未提交内容。

现有搜索（后台应用搜索、置顶候选、公开 `/api/catalog` 与 `/api/search`）新增 Tag 匹配：查询去掉前导 `#` 并折叠后，对任一 Tag 做字面子串匹配，不解释正则或通配符。公开 DTO 与页面不包含 Tag，也没有 Tag 筛选入口；Tag 推荐属于后续计划，原关联推荐接口与界面已删除。

## 公开“全部应用”

`/api/catalog` 的 `categories` 为 `{id,name,count}`，`count` 是全站公开可用（App 与 Vendor 均启用且未删除）应用数，不随搜索文本、页码或厂商筛选变化。`/all` 平铺显示“分类：效率工具(5) 网络工具(2) …”及“全部分类”，链接保留 `q` 并重置页码，URL 可分享与后退恢复。分类筛选以 `EXISTS` 过滤后分页，多分类不会造成重复、总数错误或跨页重复。厂商页不显示分类条。

## 交换与复制

导出时 Tag 随 App 的 spec/overrides 写出；被引用分类的有效名称写入 `presets/_taxonomy.yaml`（仅 `categories`）。导入按 ID 处理分类：缺失则创建，已有默认保留名称，`dictionary_update` 显式更新；结果中没有 App 或模板使用的包内新分类在预览中标为 skip 且不创建。复制保留分类与 Tag，新 App 默认禁用，继承代理在目标 Vendor 下解析。
