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

- `x-spa-routes.routes` 中的公开路径返回 `index.html`（`200 text/html`）。厂商和应用不存在或未发布时按规范返回 404 JSON。
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
      lib/                   # 偏好、标题、确认框、通知、分页、轮询等小工具
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
| `instructions` | 沙箱使用说明 iframe | 公开（后台预览可复用） |
| `session` | 会话 store、登录表单、会话过期对话框、修改密码、账户菜单、`safeReturnPath` | 后台 |
| `configuration` | 应用/厂商配置覆盖的查询与 PATCH、`useOverlayForm`、`FieldReset` | 后台（B、C 两个包共用） |
| `proxy` | `ProxyFields`（全局、厂商、应用代理） | 后台（B、D 共用） |
| `directory` | 厂商/应用查询与变更、列表参数（`useListQuery`）、标签页规则（`appTabs`）、厂商卡片、应用表格与各编辑表单 | 后台 |
| `taxonomy` | 分类查询与重命名、`CategoryPicker`、`TagEditor`、应用分类与标签表单 | 后台 |
| `notes` | 厂商/应用管理员备注（`NotesEditor`） | 后台 |
| `exchange` | 配置导出、导入（预览—决定—信任—执行）与复制应用 | 后台 |

## 新增页面、路由和功能

1. **路由**：在 `src/app/public/routes.ts` 或 `src/app/admin/routes.ts` 加一条带 `name` 的路由，组件用 `() => import(...)` 懒加载；静态标题写 `meta.titleKey`。
2. **服务端白名单**：同一 PR 把路径加入 `api/openapi.yaml` 的 `x-spa-routes.routes`，运行 `npm run codegen`。`src/app/spaRoutes.test.ts` 要求“有名字的前端路由”与 `x-spa-routes` 完全一致；不加会在刷新或深链时 404。
3. **页面**：放在 `src/pages/<entry>/...`。现在的占位页（`PagePlaceholder`）由各工作包替换；全部替换后删除 `PagePlaceholder` 和 `ui.placeholder`。
4. **功能代码**：放在 `src/features/<domain>/`，通过 `index.ts` 导出。查询和变更函数、领域组件、该领域的文本都在这里。
5. **文本**：在 feature 下建 `locales/en.ts` 与 `locales/zh-CN.ts`（后台）或 `locales/public/en.ts`、`locales/public/zh-CN.ts`（公开页也要用），见“国际化”。
6. **导航**：后台侧边栏在 `src/app/admin/navigation.ts`；应用和厂商的标签页在 `pages/admin/apps/AppLayout.vue`、`pages/admin/vendors/VendorLayout.vue`。
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
- 其他失败由全局处理：`MutationCache` 弹出错误通知，内容是本地化的错误码文本、服务端细节（如 `VALIDATION_FAILED` 指出的字段）和请求 ID。页面自行展示某些错误码时，在 `meta: { handledCodes: [...] }` 中声明；完全不弹用 `meta: { silent: true }`。
- `ifMatch(revision)` / `ifMatchHeader(resource)` / `revisionFromEtag(etag)` 处理 `"7"` 格式。
- 应用和厂商的配置覆盖是同一个 revision 资源，所有编辑区块必须通过 `@/features/configuration` 的 `useAppConfiguration` / `useAppConfigurationPatch` 读写，否则一个区块保存后其他区块会 409。
- 配置覆盖表单用 `useOverlayForm({ configuration, paths, schema })`：草稿只含 `paths` 中的字段，未修改时跟随服务端，有修改时（含 409 重新加载后）保留；`patch(values)` 只包含改过的字段，`reset(path)` 恢复模板值并在原值为覆盖时发送 `unset`；保存成功后调用 `load(响应)`。`resetBinding(path)` 直接绑定到 `FieldReset`。
- 应用设置页的 Provider 专属区块（缓存规则、渠道 TTL 等）登记在 `AppSettingsPage.vue` 的 `providerSections` 中，组件接收 `{ vendor, app, readOnly }`。
- 上传需要进度时用 `uploadWithProgress()`（XHR，自动加 CSRF，错误同样是 `ApiError`）。

### 会话

- CSRF：后台入口启动时 `configureApi({ csrfToken, onUnauthorized })`；客户端只给 `/admin/api/` 的 POST/PUT/PATCH/DELETE 加 `X-CSRF-Token`。令牌只存在内存（Pinia）。
- 首次导航读取 `GET /admin/api/session`；未登录跳转 `/admin/login?returnTo=...`，`safeReturnPath` 只接受同源 `/admin/...`。
- 任何后台请求返回 401 `AUTH_REQUIRED`（或到达 `expires_at`）时会话变为 `expired`：页面不跳转，弹出登录对话框，草稿保留，登录后所有查询失效重取。标签页重新可见时重新读取会话。
- 退出登录先通过 `confirmDiscardDrafts()` 询问未保存的草稿。修改密码成功后所有会话失效，回到登录页并提示。

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

  加载规则写在各入口的 `messages.ts`（`import.meta.glob`），新增模块不需要改它们。各工作包只改自己的 locale 文件，不会冲突。
- `src/app/locales.test.ts` 检查：每个 `en.ts` 都有同目录的 `zh-CN.ts`，key 集合与占位符完全一致且译文非空；同一入口内命名空间不重复；公开入口不含后台文本；规范错误码目录中的每个码都有 `errors.codes.*` 译文。测试中 i18n 为严格模式，缺 key 直接抛错。
- vue-i18n 的特殊字符 `@`、`|`、`{`、`}` 需要转义（`{'@'}`）。
- 语言：`localStorage["redapp-language"]`，否则 `navigator.languages` 中第一个 `en*`/`zh*`（任何 `zh*` 视为 zh-CN），否则英文。只有用户在语言菜单中选择时才写入存储。`<html lang>` 随之更新。
- 格式化用 `useFormat()`：`number`、`bytes`（IEC，两位小数，未知为 `—`）、`dateTime`（本地时区）、`relativeTime`、`duration`。本地化文本对象用 `useLocalized()(app.name)`，空值回退到另一种语言。

## 表单

- `useForm({ validationSchema: zodSchema(schema), initialValues })`；每次校验前按界面语言设置 zod 的内置消息。
- 自定义消息用 `formError("i18n.key", params)`，`FormField` 显示时翻译。
- `<FormField v-slot="{ field }" name="title" :label="t('...')"><Input v-bind="field" /></FormField>`：生成 label、描述、错误，并设置 `aria-invalid`、`aria-describedby`；字段被触碰或提交后才显示错误。无表单状态的场景用 `Field`。
- 服务端的字段错误用 `setFieldError(name, formError("errors.codes.X"))` 放到对应字段。
- `useDirtyGuard(() => meta.value.dirty)`：离开路由时用 ConfirmDialog 询问，关闭标签页时用浏览器提示。
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

从 `@/shared/ui` 引入（表单字段从 `@/shared/forms`）。组件把 `id`、`aria-*` 等属性传给真正获得焦点的元素，所以 `<Field v-slot="{ control }"><Select v-bind="control" /></Field>` 同样有效。

| 组件 | 用途与要点 |
| --- | --- |
| `Button`、`IconButton` | 变体 primary/secondary/ghost/danger，尺寸 sm/md，`loading`；`as-child` 包裹 `RouterLink`/`<a>`。`IconButton` 必须有 `label`（无障碍名称 + 提示） |
| `Input`、`Textarea`、`NumberInput` | `v-model`；`NumberInput` 为 `number \| null`，带加减按钮与 `unit` |
| `Select`、`Combobox` | `Select` 用 `options`；`Combobox` 用于异步建议：`v-model:search` 输入、服务端过滤，`@select` 选中，列表关闭或无高亮时回车触发 `@submit`；忽略输入法组字时的回车 |
| `Switch`、`Checkbox`、`RadioGroup` | `v-model`；`Checkbox` 支持 `indeterminate` |
| `Tabs`、`NavTabs` | 页内标签（面板为同名插槽）；`NavTabs` 是路由标签（`aria-current`） |
| `Dialog`、`ConfirmDialog`、`ConfirmHost` | `Dialog` 有 `title`、`footer` 插槽、`persistent`；确认框优先用 `confirm()`，`ConfirmHost` 由壳层挂载 |
| `Popover`、`Tooltip`、`DropdownMenu`、`DropdownMenuItem` | 菜单项 `@select`、`tone="danger"`；分隔线等用 Reka 的 `DropdownMenuSeparator` |
| `Toaster` | `toast()`、`notifyError()`（`@/shared/lib`）；错误通知带可复制的请求 ID |
| `DataTable` | `columns`、`rows`、`row-key`；`v-model:sort` 只发出排序，调用方重取；表头 `aria-sort`；`loading`/`error`/空状态；单元格插槽 `#cell-<key>` |
| `Pagination`、`CursorPagination` | 页码（`v-model:page`、`total`、`page-size`）与游标（上一页/下一页） |
| `Card`、`Badge`、`Tag`、`Skeleton`、`Spinner`、`EmptyState`、`Alert` | 基础展示；`Tag` 可移除；`Alert` 的 danger/warning 为 `role=alert` |
| `AsyncState` | 查询的加载/错误（本地化消息 + 请求 ID + 重试）/空状态外框 |
| `RevisionConflictAlert` | 409 冲突提示与“加载最新版本” |
| `Field`、`FormField` | 标签、描述、错误与控件关联；`FormField` 绑定 vee-validate |
| `FilePicker`、`ProgressBar` | 按钮或拖放选文件（`accept`、`multiple`）；确定/不确定进度 |
| `CodeBlock`、`CopyButton` | 代码或命令加复制按钮，复制结果对读屏播报 |
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
- `src/test/factories/`：由规范示例构造的类型化夹具（`bootstrap()`、`publicApp()`、`session()` …），传入要关心的字段覆盖。各工作包新增同目录文件（如 `directory.ts`）并直接引用，不改 `index.ts`。
- `src/test/render.ts`：`renderWithApp(component, { props, routes, path })` 带完整插件（严格 i18n、Query 不重试、内存路由、Toaster、ConfirmHost）；`renderEntry("admin" | "public", path)` 渲染整个入口（布局、守卫、会话）。
- 需要状态的组件测试写一个 fixture SFC 放在 `src/test/components/`（运行时不含模板编译器）。
- 按角色和可见文本查询（Testing Library），断言可观察行为；不依赖组件内部状态或 CSS 类名。
- Playwright：`e2e/smoke.spec.ts`，在嵌入新前端的真实 Go 服务上运行：

```sh
make build && bin/redapp &                  # 首次启动日志里有管理员密码
cd frontend && npx playwright install chromium
REDAPP_E2E_URL=http://127.0.0.1:8080 REDAPP_E2E_PASSWORD=... npm run e2e
```

  覆盖公开首页、后台 404 文档、登录—导航—退出，且要求控制台无错误（含 CSP 违规）。保存流程（站点设置）在相应页面完成后启用。

## 命令

| 命令 | 用途 |
| --- | --- |
| `npm run dev` | 本机开发服务器（可配 `REDAPP_DEV_BACKEND`） |
| `npm run codegen` / `codegen:check` | 由规范生成类型 / 检查生成物是否最新 |
| `npm run lint` / `format` / `format:check` | ESLint（0 警告）/ Prettier 写入 / 检查 |
| `npm run typecheck` | `vue-tsc` |
| `npm test` | Vitest |
| `npm run build` | 构建到 `internal/httpserver/web`（含公开包与第三方许可检查） |
| `npm run e2e` | Playwright 冒烟测试 |
