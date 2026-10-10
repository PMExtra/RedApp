# 前端

前端在 `frontend/`，构建产物嵌入 Go 二进制（[ADR 0007](adr/0007-committed-frontend-bundle.md)）。技术栈见 [ADR 0008](adr/0008-frontend-stack.md)，编码约定见 [conventions.md](conventions.md#前端)，HTTP 契约见 [api.md](api.md)。本文说明架构和扩展方式。

## 技术栈

| 关注点 | 选择 | 版本 |
| --- | --- | --- |
| 框架与构建 | Vue、Vite（rolldown）、TypeScript（strict） | 3.5 / 8.3 / 6.0 |
| 路由 | vue-router | 5.4 |
| 服务端状态 | TanStack Query（Vue） | 5.104 |
| 客户端状态 | Pinia（只放会话与界面偏好） | 4.0 |
| 国际化 | vue-i18n（composition API，JIT 编译，无 eval） | 11.4 |
| 组件与样式 | Reka UI（无样式、可访问）+ Tailwind CSS v4 + 设计令牌 | 2.11 / 4.3 |
| 表单 | vee-validate + zod（自带适配器 `zodSchema`） | 4.15 / 4.6 |
| API 类型与客户端 | openapi-typescript 生成类型，openapi-fetch 调用 | 7.13 / 0.17 |
| 测试 | Vitest + happy-dom + Testing Library + MSW；Playwright 冒烟 | 5.0 / 20 / 8.1 / 3.0 / 1.64 |
| 规范 | ESLint（flat，typescript-eslint strict type-checked，eslint-plugin-vue）+ Prettier | 10 / 3.9 |

确切版本以 `package-lock.json` 为准。随产物分发的包及许可见 [third_party/README.md](../../third_party/README.md)，构建会检查是否遗漏。

## 两个入口与 SPA 服务契约

公开访客不能下载后台代码，所以有两个 Vite 入口：

| 文档 | 入口 | 路由 |
| --- | --- | --- |
| `index.html` | `src/app/public/main.ts` | `/`、`/all`、`/{vendor}`、`/{vendor}/{app}` |
| `admin.html` | `src/app/admin/main.ts` | `/admin/...` 全部页面 |

服务端必须这样提供（规范 `x-spa-routes`，含 `documents` 字段）：

- `x-spa-routes.routes` 中的公开路径返回 `index.html`（`200 text/html`）。厂商和应用不存在或未发布时返回同一个 `index.html`，状态为 404；页面的数据请求（`getPublicVendor`、`listCatalog`、`getPublicApp`）同样返回 404，SPA 据此显示“页面不存在”。
- `x-spa-routes.routes` 中的 `/admin/...` 路径返回 `admin.html`（`200`）；其余 `/admin/...`（不含 `/admin/api/`）返回 `admin.html` 且状态 404，由 SPA 显示“页面不存在”。`/admin/...` 不能回退到 `index.html`。
- `/admin` 重定向 `/admin/overview`（307）。
- 两个文档都带 `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; frame-ancestors 'none'; base-uri 'none'`。产物里没有内联脚本、内联 `<style>` 或 `eval`。
- `/assets/` 下只有扁平的 `*.js`、`*.css`、`*.woff2` 和字体许可 `*.txt`（`assetsInlineLimit: 0`，不产生其他类型）。`frontend/public/assets/` 中的 JetBrains Mono 字体同时被使用说明文档引用，文件名不能改。
- 文件都在嵌入目录 `internal/httpserver/web/` 的根部：`index.html`、`admin.html`、`assets/`。

两个入口共享的模块（Vue、Reka UI、`shared/`）打成公共 chunk。`vite build` 的 `publicBundleGuard` 检查公开入口能到达的模块中没有 `src/app/admin/` 和 `src/pages/admin/`，否则构建失败。

本地开发：`npm run dev` 把 `/admin`、`/admin/...` 改写到 `admin.html`。设置 `REDAPP_DEV_BACKEND=http://127.0.0.1:8080` 会把 `/api`、`/admin/api`、`/assets/icons`、`/assets/presets` 代理到本机 Go 服务（保留 `Host`，Origin 检查与 Cookie 可用）。

## 目录与依赖规则

```text
frontend/
  index.html, admin.html     # 两个入口文档
  scripts/codegen.mjs        # 由 ../api/openapi.yaml 生成 src/shared/api/*.gen.ts
  e2e/                       # Playwright 冒烟测试
  src/
    app/
      core/                  # installCore（Pinia、i18n、Query、路由行为）、AppRoot
      public/                # 公开入口：main、routes、PublicLayout、messages、locales
      admin/                 # 后台入口：main、routes、guard、AdminLayout、navigation、locales
    pages/public/, pages/admin/   # 路由页面
    features/<domain>/       # 领域功能：查询、变更、领域组件、locales；index.ts 是唯一出口
    shared/
      api/                   # 生成类型、客户端、错误、revision、查询约定
      ui/                    # 设计系统组件（Reka UI + Tailwind）
      forms/                 # vee-validate + zod、FormField、离开保护（单独出口）
      i18n/                  # vue-i18n 初始化、语言检测、格式化
      lib/                   # 偏好、标题、确认框、通知、分页（`useCursorPagination`、`parsePage`）、轮询、客户端 ID（`randomId`）等小工具
      styles/                # tokens.css、main.css（Tailwind 主题）
    test/                    # 测试基础设施：setup、msw、render、factories、fixture 组件
```

由 ESLint（`no-restricted-imports`）强制：

- `shared/` 不引用 `features/`、`pages/`、`app/`；`features/` 不引用 `pages/`、`app/`。
- 跨目录引用 feature 只能通过 `@/features/<domain>`（即其 `index.ts`），不能引用其内部文件。
- `pages/` 只组合 features 与 shared，不直接调用 `api`。页面需要的查询写在对应 feature 里。
- 表单栈只从 `@/shared/forms` 引入，公开站点因此不加载 vee-validate 与 zod。

已有 feature：

| feature | 内容 | 使用方 |
| --- | --- | --- |
| `bootstrap` | `getBootstrap` 查询、站点文本、`SiteFooter` | 两个入口 |
| `search` | 头部搜索框（`searchCatalog`、`getHome`） | 公开 |
| `catalog` | 首页、目录、厂商与应用详情的查询，应用卡片、分类筛选、托管下载列表、下载地址前缀、头部后台链接（`useAdminLink`） | 公开 |
| `instructions` | 沙箱使用说明 iframe | 公开（后台预览可复用） |
| `session` | 会话 store、登录表单、会话过期对话框、修改密码、账户菜单、`safeReturnPath` | 后台 |
| `configuration` | 应用/厂商配置覆盖的查询与 PATCH、`useOverlayForm`（字段表单）、`useOverlayDraft`（规则与策略编辑器）、`FieldReset`、`OverlayFormActions` | 后台设置表单与运行时策略面板 |
| `proxy` | `ProxyFields`（全局、厂商、应用代理） | 厂商/应用设置、全局代理页 |
| `directory` | 厂商/应用查询与变更（`useApp` 的 key 为 `["getApp", { vendor, app }]`，应用壳层与所有标签页共用）、列表参数（`useListQuery`）、标签页规则（`appTabs`、`defaultAppTab`、`appRoute`）、厂商卡片、应用表格与各编辑表单 | 后台 |
| `taxonomy` | 分类查询与重命名、`CategoryPicker`、`TagEditor`、应用分类与标签表单 | 后台 |
| `notes` | 厂商/应用管理员备注（`NotesEditor`） | 后台 |
| `exchange` | 配置导出、导入（预览—决定—信任—执行）与复制应用 | 后台 |
| `metrics` | 全局/应用指标查询、`MetricCards`、`HistoryChart`、`MetricHistoryDialog`，见下文 | 后台概览、应用版本页 |
| `events` | `listEvents` 游标分页查询、`EventsTable` | 后台事件页 |
| `settings` | 站点文本、公开地址、首页置顶、全局代理的查询与保存，各区块表单、`AppPicker`（`listApps` 搜索） | 后台设置页 |
| `releases` | 版本与制品清单（`ReleaseInventory`，游标分页、按版本筛选）、缓存来源选择（`SourceEpochSelect`、`useSources`）、版本清理（预览—执行）、预览过期（`useExpired`） | 应用版本页、缓存页 |
| `retention` | 版本保留策略（配置覆盖）、状态与预览—执行（`RetentionPanel`） | 发布类应用缓存页 |
| `prewarm` | 自动预热策略、手动预热任务的创建、轮询、重试与取消（`PrewarmPanel`；任务 ID 存在 `localStorage`，刷新后继续跟踪） | 应用缓存页 |
| `http-cache` | 缓存条目列表与单文件刷新、按规则后台刷新、按时间清理（预览—执行）、自动清理状态 | `http-cache` 应用缓存页 |
| `hosted` | 托管文件列表、上传/按 URL 导入（进度、取消）、替换与删除（`HostedFilesPanel`） | `hosted` 应用文件页 |
| `cache-policy` | 设置页区块 `CachePolicySection`（`http_policy`：过期回退、路径 TTL 规则、自动清理规则）与 `ChannelTtlSection`（发布类应用的元数据有效期），`PathMatchInput`（服务端试匹配）、`DurationInput` | 应用设置页、缓存维护面板 |

## 新增页面、路由和功能

1. **路由**：在 `src/app/public/routes.ts` 或 `src/app/admin/routes.ts` 加一条带 `name` 的路由，组件用 `() => import(...)` 懒加载；静态标题写 `meta.titleKey`。
2. **服务端白名单**：同一 PR 把路径加入 `api/openapi.yaml` 的 `x-spa-routes.routes`，运行 `npm run codegen`。`src/app/spaRoutes.test.ts` 要求“有名字的前端路由”与 `x-spa-routes` 完全一致；不加会在刷新或深链时 404。
3. **页面**：放在 `src/pages/<entry>/...`。`x-spa-routes` 中的每条路由都渲染真实页面，不保留占位页。
4. **功能代码**：放在 `src/features/<domain>/`，通过 `index.ts` 导出。查询和变更函数、领域组件、该领域的文本都在这里。
5. **文本**：在 feature 下建 `locales/en.ts` 与 `locales/zh-CN.ts`（后台）或 `locales/public/en.ts`、`locales/public/zh-CN.ts`（公开页也要用），见“国际化”。
6. **导航**：后台侧边栏在 `src/app/admin/navigation.ts`；应用和厂商的标签页在 `pages/admin/apps/AppLayout.vue`、`pages/admin/vendors/VendorLayout.vue`。应用有哪些标签由 `@/features/directory` 的 `appTabs` 决定，壳层对不可用的标签显示“不适用”，对已删除的应用显示只读提示，标签页本身不再重复判断。`/admin/vendors/{vendor}/apps/{app}`（不带标签，路由名 `admin-app`，在 `x-spa-routes` 中）加载应用后替换为 `defaultAppTab`：发布类应用为版本，`http-cache` 为缓存，`hosted` 为文件，其余及已删除的应用为设置。站内链接（如事件列表）可以直接指向它。
7. **焦点与标题**：路由切换（路径变化）后焦点自动移到 `<main id="main-content">`；只改查询参数时不移动焦点。动态标题用 `useDocumentTitle(() => name)`，壳层自动拼接站点标题。

## API 用法

### 生成

`npm run codegen` 读取 `../api/openapi.yaml`，生成并提交两个文件：

- `src/shared/api/schema.gen.ts`：openapi-typescript 的 `paths`、`components`、`operations`。
- `src/shared/api/spec.gen.ts`：`spaRoutes`、`spaAllowedQuery`、`spaDocuments`、`errorCatalog`（错误码 → 状态、`retryable`）。

`npm run codegen:check` 在内存中重新生成并与提交的文件逐字节比较，由 `make frontend-test` 和 CI 执行，规范改了却没重新生成会失败。生成文件不经 Prettier/ESLint。用 `Schema<"PublicApp">` 引用模型，不手写 DTO。

### 查询

```ts
// features/<domain>/queries.ts
export function usePublicApp(vendor: MaybeRefOrGetter<string>, app: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => queryKey("getPublicApp", { vendor: toValue(vendor), app: toValue(app) })),
    queryFn: ({ signal }) =>
      unwrap(api.GET("/api/apps/{vendor}/{app}", {
        params: { path: { vendor: toValue(vendor), app: toValue(app) } },
        signal,
      })),
  });
}
```

- `unwrap()` 返回数据，失败时抛 `ApiError`（`code`、`message`、`requestId`、`retryable`、`status`）；网络失败是 `NETWORK_ERROR`，非规范错误体是 `UNEXPECTED_RESPONSE`；取消原样抛出 `AbortError`。只按 `code` 分支。
- **key 约定**：`[operationId]` 或 `[operationId, { 路径参数, 查询参数 }]`，用 `queryKey()` 生成。同一操作在不同组件里用相同的 key 共享缓存（例如头部搜索和首页都用 `["getHome"]`）。失效时可以按操作或按应用部分匹配：`invalidateQueries({ queryKey: ["listVersions", { vendor, app }] })`。
- 过期请求由 TanStack Query 处理：key 变化后旧响应不会写入新 key，`signal` 取消被替换的请求，相同 key 的并发请求合并。不要自己写“ticket”。
- 默认：`staleTime` 15 秒，窗口重新可见时刷新，只重试 `retryable` 的错误（最多 2 次）。
- 翻页列表把 `page` 或 `cursor` 放进 key，用 `placeholderData: keepPreviousData` 避免闪烁。游标列表用 `useCursorPagination()` + `CursorPagination`，页码列表用 `Pagination`。
- 轮询：`const auto = useAutoRefresh()`，`useQuery({ ..., refetchInterval: auto.refetchInterval })`，配合 `AutoRefreshToggle`；隐藏标签页时自动暂停。

### 写操作与 revision

- 可编辑资源的写操作一律用 `useRevisionedMutation`：

```ts
const save = useRevisionedMutation({
  revision: () => settings.data.value?.revision,      // 草稿的基线
  queryKey: queryKey("getSiteSettings"),               // 成功后用响应替换缓存
  mutationFn: (body: SiteSettings, ifMatch) =>
    unwrap(api.PUT("/admin/api/settings/site", { params: { header: { "If-Match": ifMatch } }, body })),
});
```

- 409 `REVISION_CONFLICT` 不弹通知，而是设置 `save.conflict`；页面保留草稿并显示 `<RevisionConflictAlert @reload="save.reload()" />`。`reload()` 重新读取基线，页面自己决定是否重置草稿。
- 其他失败由全局处理：`MutationCache` 弹出错误通知，内容是本地化的错误码文本、服务端细节（如 `VALIDATION_FAILED` 指出的字段）和请求 ID。`ENTITY_DELETED`、`APPLICATION_DELETE_PENDING` 与 `APPLICATION_DISABLED` 另外让 `getApp`、`getVendor` 重新读取，页面随之显示只读或“请先启用应用”提示。页面自行展示某些错误码时，在 `meta: { handledCodes: [...] }`（`useRevisionedMutation` 用 `handledCodes` 选项）中声明；完全不弹用 `meta: { silent: true }`。
- 以某个 revision 为条件、但响应不是该资源的写操作（如保留策略的预览以配置 revision 为 `If-Match`）同样用 `useRevisionedMutation`，不传 `queryKey`；冲突时显示 `RevisionConflictAlert`，重新加载由页面自己读取基线并调用 `dismissConflict()`。
- `ifMatch(revision)` / `ifMatchHeader(resource)` / `revisionFromEtag(etag)` 处理 `"7"` 格式。
- 应用和厂商的配置覆盖是同一个 revision 资源，所有编辑区块必须通过 `@/features/configuration` 的 `useAppConfiguration` / `useAppConfigurationPatch` 读写，否则一个区块保存后其他区块会 409。
- 配置覆盖表单用 `useOverlayForm({ configuration, paths, schema })`：草稿只含 `paths` 中的字段，未修改时跟随服务端，有修改时（含 409 重新加载后）保留；`patch(values)` 只包含改过的字段，`reset(path)` 恢复模板值并在原值为覆盖时发送 `unset`；保存成功后调用 `load(响应)`。`resetBinding(path)` 直接绑定到 `FieldReset`。
- 不是字段表单的编辑器（规则列表、保留与预热策略）用 `useOverlayDraft(configuration, read)`：`read(spec)` 把配置映射为各路径的草稿值，`patch` 给出 `set`/`unset`，`restore(path)` 恢复模板值；调用方自己做校验并安装 `useDirtyGuard(draft.dirty)`。
- 上传需要进度时用 `uploadWithProgress()`（XHR，自动加 CSRF，错误同样是 `ApiError`）。

### 会话

- CSRF：后台入口启动时 `configureApi({ csrfToken, onUnauthorized })`；客户端只给 `/admin/api/` 的 POST/PUT/PATCH/DELETE 加 `X-CSRF-Token`。令牌只存在内存（Pinia）。
- 首次导航读取 `GET /admin/api/session`；未登录跳转 `/admin/login?returnTo=...`，`safeReturnPath` 只接受同源 `/admin/...`。
- 任何后台请求返回 401 `AUTH_REQUIRED`（或到达 `expires_at`）时会话变为 `expired`：页面不跳转，弹出登录对话框，草稿保留，登录后所有查询失效重取。标签页重新可见时重新读取会话。
- 退出登录和修改密码（打开对话框前）先通过 `confirmDiscardDrafts()` 询问未保存的草稿；修改密码成功后所有会话失效。会话结束后壳层用 `leaveDiscardingDrafts()` 跳到登录页并提示，离开保护不再询问。退出失败时什么都不记住，之后的导航照常询问。

## 扩展点

| 位置 | 用途 |
| --- | --- |
| `pages/admin/apps/AppSettingsPage.vue` 的 `providerSections` | Provider 专属设置区块：`{ id, providers, component }`，按顺序显示在使用说明之后，已删除的应用不显示。组件接收 `{ vendor, app, readOnly }`，通过 `@/features/configuration` 读写同一个配置覆盖，自带草稿、冲突提示与离开保护。现有：`CachePolicySection`（`http-cache`）、`ChannelTtlSection`（`codex`、`claude-code`）；`http-cache` 的 `cache_ttl_seconds` 在 `AppGeneralForm` 的上游区块中编辑 |
| `features/directory/links.ts` 的 `appTabs`、`defaultAppTab` | 应用标签与默认标签；新增标签同时在 `admin/routes.ts` 加子路由、在 `AppLayout.vue` 加标签名、在 `x-spa-routes` 加路径 |
| `app/admin/navigation.ts` | 后台侧边栏 |
| `MetricCards` 的 `primary` | 页面自己的常用指标集合（见下文） |

## 指标与历史图表

`@/features/metrics` 同时服务全局概览和单个应用（`http-cache`、`codex`、`claude-code`）：

| 出口 | 用途 |
| --- | --- |
| `useGlobalStatus({ refetchInterval })`、`useAppStatus(vendor, app, { refetchInterval })` | `getStatus` / `getAppStatus`；配合 `useAutoRefresh()` 每 5 秒轮询。刷新失败时 `data` 保留上一次快照，页面同时显示 `error` 作为警告 |
| `MetricScope`、`GLOBAL_SCOPE`、`appScope(vendor, app)` | 指标归属：`{ kind: "global" }` 或 `{ kind: "app", vendor, app }` |
| `<MetricCards :metrics :primary :scope @select>` | 按规范的 `group`（disk、traffic、speed、runtime、resources）分组；`primary` 中的键为常用指标，其余为折叠的诊断指标。默认 `GLOBAL_COMMON_METRICS`（16 项），应用页传 `APP_COMMON_METRICS`。`level` 设置标题层级 |
| `<MetricHistoryDialog v-model:metric :scope>` | 设置 `metric` 打开对话框，关闭时置为 `undefined`；在对话框间保持所选范围 |
| `<HistoryChart :metric :scope v-model:range>` | 不带对话框的历史图（uPlot）：24h/7d/30d（默认 7d），计数器可切换累计值与每段增量；缺失样本保持空缺；键盘（左右、Page Up/Down、Home/End、Esc）与触摸读数，所选时间段在后台刷新后保留，触摸后鼠标移入恢复悬停读数；摘要句与数据表（`HistoryReadout`、`HistoryTable`）作为无图替代 |
| `useMetricHistory(scope, metric, range)` | `getHistory` / `getAppHistory` |
| `useMetricLabels()`、`useMetricFormat()`、`formatMetricValue()` | 本地化名称（`metrics.labels.<key>`，缺失时回退到服务端英文 `label`）与按 `unit` 格式化（IEC 字节、字节/秒、计数、时长；未知为 `—`） |

应用页示例：

```vue
<MetricCards :metrics="status.data.value.metrics" :primary="APP_COMMON_METRICS" :scope="appScope(vendor, app)" :level="3" @select="selected = $event" />
<MetricHistoryDialog v-model:metric="selected" :scope="appScope(vendor, app)" />
```

`sampled`、`stale` 两个文本（`metrics.sampled`、`metrics.stale`）供页面显示采样时间与刷新失败警告。图表颜色在绘制时从设计令牌（`--rd-primary`、`--rd-text-subtle`、`--rd-border`）读取，主题或语言切换后重绘。uPlot 只通过 CSSOM 设置样式，不违反 CSP。happy-dom 没有 canvas，打开历史图的测试用 `vi.mock("uplot", () => import("@/test/uplot"))` 替身（每个时间段 10px，鼠标在绘图区移动时像 uPlot 一样触发 `setCursor` 钩子）。

## 国际化

- 文本 key 是语义化的嵌套路径：`session.login.submit`、`errors.codes.REVISION_CONFLICT`。不要把英文原文当 key。
- 每个模块有自己的 locale 文件，顶层命名空间归该文件独有（重复会在启动和测试时报错）：

| 位置 | 加载的入口 |
| --- | --- |
| `src/shared/**/locales/{en,zh-CN}.ts`（`common`、`ui`、`errors`） | 两者 |
| `src/app/public/locales/`、`src/pages/public/**/locales/` | 公开 |
| `src/app/admin/locales/`、`src/pages/admin/**/locales/` | 后台 |
| `src/features/<x>/locales/public/` | 两者 |
| `src/features/<x>/locales/`（直接位于该目录的文件） | 后台 |

  加载规则写在各入口的 `messages.ts`（`import.meta.glob`），新增模块不需要改它们。各模块只改自己的 locale 文件。
- `src/app/locales.test.ts` 检查：每个 `en.ts` 都有同目录的 `zh-CN.ts`，key 集合与占位符完全一致且译文非空；同一入口内命名空间不重复；公开入口不含后台文本；规范错误码目录中的每个码都有 `errors.codes.*` 译文。测试中 i18n 为严格模式，缺 key 直接抛错。
- vue-i18n 的特殊字符 `@`、`|`、`{`、`}` 需要转义（`{'@'}`）。
- 语言：`localStorage["redapp-language"]`，否则 `navigator.languages` 中第一个 `en*`/`zh*`（任何 `zh*` 视为 zh-CN），否则英文。只有用户在语言菜单中选择时才写入存储。`<html lang>` 随之更新。
- 格式化用 `useFormat()`：`number`、`bytes`（IEC，两位小数，未知为 `—`）、`dateTime`（本地时区）、`relativeTime`、`duration`。本地化文本对象用 `useLocalized()(app.name)`，空值回退到另一种语言；厂商图标用 `vendorLogo(vendor, locale)`（该语言的图标，否则默认图标）。

## 表单

- `useForm({ validationSchema: zodSchema(schema), initialValues })`；每次校验前按界面语言设置 zod 的内置消息。
- 自定义消息用 `formError("i18n.key", params)`，`FormField` 显示时翻译。
- `<FormField v-slot="{ field }" name="title" :label="t('...')"><Input v-bind="field" /></FormField>`：生成 label、描述、错误，并设置 `aria-invalid`、`aria-describedby`；字段被触碰或提交后才显示错误。无表单状态的场景用 `Field`。
- 服务端的字段错误用 `setFieldError(name, formError("errors.codes.X"))` 放到对应字段。
- `useDirtyGuard(() => meta.value.dirty)`：离开路由时用 ConfirmDialog 询问，关闭标签页时用浏览器提示。一个页面有多个未保存区块时，同一次导航只询问一次。
- 草稿已确认或已无意义的跳转（退出登录、修改密码后到登录页，删除厂商或应用后回到列表）写成 `await leaveDiscardingDrafts(() => router.push(...))`：只有回调中的导航跳过离开保护，导航结束（成功或失败）后恢复。不要用全局标志记住“已确认”。
- 不用 `window.confirm`；需要确认时 `await confirm({ title, description, tone: "danger" })`。

## 设计令牌

令牌在 `src/shared/styles/tokens.css`（`--rd-*` 原始值，浅色与 `[data-theme=dark]` 深色两套），`main.css` 用 `@theme inline` 映射为 Tailwind 工具类。组件只使用这些类，不写颜色字面量。

| 类别 | 工具类 |
| --- | --- |
| 颜色 | `bg-bg`、`bg-surface`、`bg-surface-raised`、`bg-surface-sunken`、`bg-surface-hover`、`border-border`、`border-border-strong`、`text-fg`、`text-muted`、`text-subtle`、`text-inverse`；`primary`、`danger` 带 `-hover`/`-soft`/`-fg`；`warning`、`success`、`info` 带 `-soft`；`focus`、`overlay` |
| 字体 | `font-sans`（系统字体，含中文字体回退）、`font-mono`（JetBrains Mono，`/assets/` 中的字体文件） |
| 字号 | `text-xs` … `text-3xl`（带行高） |
| 间距 | Tailwind 默认刻度（`--spacing: 0.25rem`） |
| 圆角 | `rounded-sm`、`rounded-md`、`rounded-lg`、`rounded-xl` |
| 阴影 | `shadow-sm`、`shadow-md`、`shadow-overlay` |
| 层级 | `z-sticky` < `z-dropdown` < `z-overlay` < `z-modal` < `z-popover` < `z-toast` < `z-tooltip` |
| 焦点 | `focus-ring`（`:focus-visible` 时显示 `--rd-focus` 轮廓） |

主题：偏好存在 `localStorage["redapp-theme"]`（`system`/`light`/`dark`），`system` 跟随 `prefers-color-scheme`；解析后的主题写在 `<html data-theme>`，`dark:` 变体按它生效。

## 组件清单

从 `@/shared/ui` 引入（表单字段从 `@/shared/forms`）。组件把 `id`、`aria-*` 等属性传给真正获得焦点的元素，所以 `<Field v-slot="{ control }"><Select v-bind="control" /></Field>` 同样有效。`control` 含指向字段标签的 `aria-labelledby`，`<label for>` 无法命名的控件（`RadioGroup`、`FilePicker`）也因此有名称。

| 组件 | 用途与要点 |
| --- | --- |
| `Button`、`IconButton` | 变体 primary/secondary/ghost/danger，尺寸 sm/md，`loading`；`as-child` 包裹 `RouterLink`/`<a>`。`IconButton` 必须有 `label`（无障碍名称 + 提示），属性与监听器（`@click`、`class`）落在按钮上而不是提示框外层 |
| `Input`、`Textarea`、`NumberInput` | `v-model`；`NumberInput` 为 `number \| null`，带加减按钮与 `unit` |
| `Select`、`Combobox` | `Select` 用 `options`；`Combobox` 用于异步建议：`v-model:search` 输入、服务端过滤，`@select` 选中，列表关闭或没有高亮可用选项（包括列表在打开时清空）时回车触发 `@submit`；忽略输入法组字时的回车 |
| `Switch`、`Checkbox`、`RadioGroup` | `v-model`；`Checkbox` 支持 `indeterminate` |
| `Tabs`、`NavTabs` | 页内标签（面板为同名插槽），隐藏的面板默认卸载，面板里有进行中的工作（预览、运行中的任务）时加 `keep-mounted`；`NavTabs` 是路由标签（`aria-current`） |
| `Dialog`、`ConfirmDialog`、`ConfirmHost` | `Dialog` 有 `title`、`footer` 插槽、`persistent`；确认框优先用 `confirm()`，`ConfirmHost` 由壳层挂载 |
| `Popover`、`Tooltip`、`DropdownMenu`、`DropdownMenuItem` | 菜单项 `@select`、`tone="danger"`；分隔线等用 Reka 的 `DropdownMenuSeparator` |
| `Toaster` | `toast()`、`notifyError()`（`@/shared/lib`）；错误通知带可复制的请求 ID |
| `DataTable` | `columns`、`rows`、`row-key`；`v-model:sort` 只发出排序，调用方重取；表头 `aria-sort`；`loading`/`error`/空状态；单元格插槽 `#cell-<key>` |
| `Pagination`、`CursorPagination` | 页码（`v-model:page`、`total`、`page-size`；超过 7 页时附带“跳至页码”输入，不用 `<form>`，可放在表单内）与游标（上一页/下一页）。URL 中的页码用 `@/shared/lib` 的 `parsePage()` 解析 |
| `Card`、`Badge`、`Tag`、`Skeleton`、`Spinner`、`EmptyState`、`Alert` | 基础展示；`Tag` 可移除；`Alert` 的 danger/warning 为 `role=alert` |
| `AsyncState` | 查询的加载/错误（本地化消息 + 请求 ID + 重试）/空状态外框 |
| `RevisionConflictAlert` | 409 冲突提示与“加载最新版本” |
| `Field`、`FormField` | 标签、描述、错误与控件关联；`FormField` 绑定 vee-validate |
| `FilePicker`、`ProgressBar` | 按钮或拖放选文件（`accept`、`multiple`），`aria-labelledby`/`aria-describedby`/`aria-invalid` 落在按钮上，`id` 留在隐藏的文件输入上；确定/不确定进度 |
| `CodeBlock`、`CopyButton` | 代码或命令加复制按钮，复制结果在按钮外的状态区域播报，重复复制会再次播报 |
| `Breadcrumbs`、`PageHeader` | 页面 h1、描述、面包屑、操作区 |
| `SideNav`、`TopNav`、`SkipLink` | 壳层导航；`SkipLink` 跳到 `#main-content` |
| `LanguageSwitcher`、`ThemeToggle` | 写入偏好 store |
| `EntityIcon`、`RelativeTime`、`SortableList`、`AutoRefreshToggle` | 图标（失败回退、`variant="logo"`）；相对时间（30 秒刷新，悬停显示精确时间）；拖动或键盘排序并播报；自动刷新开关 |

Reka UI 的 `SelectViewport`/`ComboboxViewport`/`ScrollAreaViewport` 会插入内联 `<style>`，被 SPA 的 CSP 拦截，不要使用；列表滚动直接写在内容元素上。

## 使用说明文档

`@/features/instructions` 的 `InstructionsDocument`（[ADR 0006](adr/0006-sandboxed-usage-instructions.md)）：

- `src` 为 `/api/apps/{vendor}/{app}/instructions/document?lang=<界面语言>`，`sandbox="allow-scripts allow-popups allow-popups-to-escape-sandbox allow-top-navigation-by-user-activation"`（没有 `allow-same-origin`），`allow="clipboard-write"`。
- 高度来自 `postMessage({type: "redapp-instructions-height", height})`，只接受来源是本 iframe `contentWindow` 的有限数字，加 20px 后限制在 120–50000px。
- `revision`（`PublicApp.revision`）或语言变化时重建 iframe。

## 测试

- `npm test`（Vitest + happy-dom）。测试文件与被测代码放在一起，命名 `*.test.ts`。
- `src/test/msw.ts`：所有测试共享的 MSW 服务器，未处理的请求让测试失败。`mockApi(method, specPath, resolver)` 按规范路径注册，返回值类型来自生成的成功响应；`apiError(code)` 按错误码目录生成状态与 `retryable`；`noContent()`。在测试中用 `useHandlers(...)` 安装，测试结束自动重置。
- `src/test/factories/`：由规范示例构造的类型化夹具，传入要关心的字段覆盖。按领域分文件并直接引用：`index.ts`（引导、会话、公开应用）、`catalog.ts`（目录、托管文件）、`directory.ts`（厂商、应用 `app()`、列表项、页 `page()`、配置覆盖、分类、导入）、`metrics.ts`（`metric(key)`、`globalStatus()`、`appStatus()`、`historySeries()`）、`settings.ts`、`runtime.ts`（`codexApp()`、`httpCacheApp()`、`codexConfiguration()`、版本、制品、保留、预热、缓存维护）。同一模型只保留一个工厂，特定变体在它之上构造。
- `src/test/render.ts`：`renderWithApp(component, { props, routes, path })` 带完整插件（严格 i18n、Query 不重试、内存路由、Toaster、ConfirmHost）；`renderEntry("admin" | "public", path)` 渲染整个入口（布局、守卫、会话）。
- `src/test/directory.ts`：`renderAdminPage(path)` 以已登录管理员渲染后台入口（引导、会话、Provider 列表已应答）；`recorder()` 收集请求体、`If-Match` 和 URL。
- `src/test/appPage.ts`：`renderAppPage(component, { key, tab, props })` 把面板作为 `/admin/vendors/:vendor/apps/:app/:tab` 的路由组件渲染，`route.params` 与离开保护和真实页面一致。
- `src/test/uplot.ts`：uPlot 替身，见“指标与历史图表”。
- 需要状态的组件测试写一个 fixture SFC 放在 `src/test/components/`（运行时不含模板编译器）。
- 按角色和可见文本查询（Testing Library），断言可观察行为；不依赖组件内部状态或 CSS 类名。
- Playwright：`e2e/*.spec.ts`，在嵌入前端的真实 Go 服务上运行。`make e2e`（`scripts/test-e2e.py`）用现有 `bin/redapp` 和全新数据目录启动服务，从首次启动日志读取初始管理员密码，经管理 API 发布一个 `info` 应用（`e2e/guide`，不访问任何上游），再以 `REDAPP_E2E_URL`、`REDAPP_E2E_PASSWORD`、`REDAPP_E2E_INFO_APP` 运行 `npm run e2e`；额外参数传给 Playwright。失败时打印服务日志（密码已遮盖）。

```sh
make build
(cd frontend && npx playwright install --with-deps chromium)   # 一次即可
make e2e                                                       # 或 python3 scripts/test-e2e.py e2e/smoke.spec.ts
```

  `smoke.spec.ts` 覆盖公开首页、后台 404 文档、登录—导航—退出、一次完整的保存流程（修改站点副标题、刷新后确认、再恢复原值）以及不带标签的应用深链（登录后进入默认标签）；`public.spec.ts` 覆盖首页—目录搜索—应用页与公开 404 文档（需要至少一个已发布应用）。都用 `e2e/console.ts` 的 `collectConsoleErrors` 要求控制台无错误（含 CSP 违规）；未登录时会话探测按契约返回的 401 不算错误。也可以对任意运行中的服务直接运行 `npm run e2e`，自己设置上述环境变量。

## 命令

| 命令 | 用途 |
| --- | --- |
| `npm run dev` | 本机开发服务器（可配 `REDAPP_DEV_BACKEND`） |
| `npm run codegen` / `codegen:check` | 由规范生成类型 / 检查生成物是否最新 |
| `npm run lint` / `format` / `format:check` | ESLint（0 警告）/ Prettier 写入 / 检查 |
| `npm run typecheck` | `vue-tsc` |
| `npm test` | Vitest |
| `npm run build` | 构建到 `internal/httpserver/web`（含公开包与第三方许可检查） |
| `npm run e2e` | Playwright 测试（对 `REDAPP_E2E_URL` 的服务；完整流程用仓库根目录的 `make e2e`） |
