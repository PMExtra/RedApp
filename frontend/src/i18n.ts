import { ref, watch } from "vue";
export type Language = "en" | "zh-CN";
export function resolveLanguage(value: unknown): Language | undefined {
  if (typeof value !== "string" || !/^(en|zh)(?:-[a-z0-9]{1,8})*$/i.test(value))
    return;
  return value.toLowerCase().startsWith("zh") ? "zh-CN" : "en";
}
export function initialLanguage(): Language {
  try {
    const saved = localStorage.getItem("redapp-language");
    if (saved === "en" || saved === "zh-CN") return saved;
  } catch {
    /* Storage may be disabled. */
  }
  try {
    const preferred = navigator.languages;
    if (preferred?.length) {
      for (const item of preferred) {
        const supported = resolveLanguage(item);
        if (supported) return supported;
      }
      return "en";
    }
  } catch {
    /* Fall back to the single browser language. */
  }
  try {
    return resolveLanguage(navigator.language) || "en";
  } catch {
    return "en";
  }
}
export const language = ref<Language>(initialLanguage());
export function setLanguage(value: Language) {
  language.value = value;
  try {
    localStorage.setItem("redapp-language", value);
  } catch {
    /* Keep the explicit in-memory choice. */
  }
}
watch(
  language,
  (value) => {
    document.documentElement.lang = value;
  },
  { immediate: true, flush: "sync" },
);
export const messages = {
  "Deletion is not complete. This application is blocked while its tasks stop. Retry deletion; restarting also resumes it.":
    "删除尚未完成。此应用已阻止新任务，正在等待现有任务退出。可重试删除；重启服务也会继续处理。",
  "Unexpected error. Reload and try again.": "发生意外错误，请重新加载后重试。",
  "Deleted. Some stored files are awaiting cleanup; restart the server to retry cleanup.":
    "已删除。部分存储文件等待清理，请重启服务重试清理。",
  Source: "来源",
  "File pages": "文件分页",
  "Delete this saved file? It cannot be downloaded again unless you add it.":
    "删除此文件？删除后需重新添加才能再次下载。",
  "Saved at": "保存时间",
  "No files saved yet.": "尚未保存文件。",
  "Saving file": "正在保存文件",
  "Save file": "保存文件",
  "Replace file": "替换文件",
  "HTTP(S) URL": "HTTP(S) 网址",
  "Cancel replacement": "取消替换",
  "This explicitly replaces the selected file. A newer change will be rejected.":
    "将明确替换所选文件。如果该文件已被更新，本次替换会被拒绝。",
  "Resource path": "资源相对路径",
  "Import from URL": "从网址导入",
  "Upload file": "上传文件",
  "Transfer cancellation requested. Refreshing saved files.":
    "已请求取消传输，正在刷新已保存文件。",
  "The file changed. Select the current file explicitly before replacing it.":
    "文件已变化，请重新选择当前文件后再明确替换。",
  "Transfer cancelled.": "传输已取消。",
  "File saved.": "文件已保存。",
  "Files stay available until you delete or explicitly replace them. Import URLs are used only once.":
    "文件会持续保留，直到手动删除或明确替换。导入网址仅用于一次性获取。",
  "Add a file": "添加文件",
  "Hosted files": "托管文件",
  "HTTP Cache": "HTTP 缓存",
  "Hosted Files": "文件托管",
  "App Info": "应用介绍",
  Copied: "已复制",
  "Sources and delivery": "上游源与分发",
  "Save instructions": "保存使用说明",
  "Plain text shown on the public application page. Generated installation commands are managed separately.":
    "以纯文本展示在公开应用页面。自动生成的安装命令单独维护。",
  "Usage instructions": "使用说明",
  "Application settings": "应用设置",
  "Cache management": "缓存管理",
  "Application sections": "应用功能",
  "Copy download URL": "复制下载地址",
  "Public application page": "公开应用页面",
  "General HTTP": "通用 HTTP",
  "Application location": "应用位置",
  "Add a vendor, then create an application and choose its provider. No applications are created automatically.":
    "添加厂商后，创建应用并选择 Provider。系统不会自动创建应用。",
  "Try another search or view.": "请尝试其他搜索词或筛选条件。",
  "Create your first vendor": "创建第一个厂商",
  "Search by ID or name": "按 ID 或名称搜索",
  "Search vendors and applications": "搜索厂商与应用",
  "Show all applications": "展开全部应用",
  Collapse: "收起",
  "{count} applications": "{count} 个应用",
  "Vendor pages": "厂商分页",
  "Application pages": "应用分页",
  "Enter a page from 1 to {pages}.": "请输入 1 到 {pages} 之间的页码。",
  Go: "跳转",
  "Page number": "页码",
  "Page {page} of {pages}": "第 {page} 页，共 {pages} 页",
  "{count} items": "{count} 项",
  "Building preview": "正在构建预览",
  Ready: "就绪",
  Running: "正在执行",
  Finished: "已结束",
  Pending: "待处理",
  Refreshed: "已刷新",
  "Not modified": "未变化",
  "Existing cached copy retained": "已保留原缓存副本",
  Skipped: "已跳过",
  Retired: "已退役",
  "Accessed since preview": "预览后被访问",
  "Generation changed": "代次已变化",
  "Selected files": "所选文件",
  "This page shows up to 25 files. Execution applies to the entire frozen selection, not only this page.":
    "每页最多显示 25 个文件；执行会处理整个冻结的选择集合，而不只是当前页。",
  Result: "结果",
  "No files on this page.": "当前页没有文件。",
  "Preview file pages": "预览文件分页",
  "Refresh cached files": "刷新缓存文件",
  "The refresh selection changed or is no longer available. Create a new preview and try again.":
    "刷新选择已变化或不可用，请重新预览后重试。",
  "Refresh checks the selected files with their current upstream source. Cached files remain available according to the stale fallback setting.":
    "刷新会向当前回源地址检查所选文件，已有缓存是否可用于失败回退由过期缓存开关决定。",
  "Preview refresh": "预览刷新",
  "Refresh selection": "刷新选择",
  "{completed} completed · {failed} failed":
    "已完成 {completed} 个 · 失败 {failed} 个",
  "{refreshed} refreshed · {unchanged} not modified · {stale} stale fallbacks · {failed} failed · {skipped} skipped":
    "已刷新 {refreshed} 个 · 未变化 {unchanged} 个 · 过期缓存回退 {stale} 个 · 失败 {failed} 个 · 跳过 {skipped} 个",
  "Refresh runs in the background. Leaving this page does not cancel the job.":
    "刷新任务在后台执行，离开此页不会取消任务。",
  "Confirm refresh for all selected files": "确认刷新全部所选文件",
  Actions: "操作",
  "Refresh file": "刷新文件",
  "Actual source": "实际来源",
  "This preview is no longer available. Create a new preview and try again.":
    "此预览已不可用，请重新预览后重试。",
  "Refresh is available only for the current enabled source. Historical files remain available for cleanup.":
    "仅可刷新当前启用的来源，历史文件仍可清理。",
  "Match type": "匹配类型",
  Glob: "Glob",
  "RE2 regular expression": "RE2 正则表达式",
  "Path pattern": "路径模式",
  "Paths start with / relative to this application, exclude query strings and are decoded once. Matching is case-sensitive.":
    "路径以 / 开头，相对于当前应用，不含查询参数并解码一次；匹配区分大小写。",
  "Glob matches a file or its parent directories: / matches all; /releases matches that file or subtree; /releases/ matches only the directory subtree; /releases/*/ matches immediate child-directory subtrees.":
    "Glob 匹配文件或其父目录：/ 匹配全部；/releases 匹配同名文件或目录子树；/releases/ 仅匹配目录子树；/releases/*/ 匹配其直接子目录下的子树。",
  "RE2 matches the complete path, not a substring. Matching is evaluated by the server.":
    "RE2 匹配完整路径，而非子字符串；匹配由服务器执行。",
  "Test a sample path": "测试示例路径",
  "Sample path": "示例路径",
  "Enter the decoded path, for example /releases/文件.zip, without URL encoding.":
    "输入已解码的路径，例如 /releases/文件.zip，不要使用 URL 编码。",
  "Test match": "测试匹配",
  Matches: "匹配",
  "Does not match": "不匹配",
  "Age to keep": "保留时长",
  "Time unit": "时间单位",
  Seconds: "秒",
  Minutes: "分钟",
  Hours: "小时",
  Days: "天",
  "Cache rules": "缓存规则",
  "Cache rules saved.": "缓存规则已保存。",
  "Path TTL rules": "路径 TTL 规则",
  "Rules run from top to bottom. The first matching path sets its TTL. Otherwise Cache-Control takes priority; the application default TTL applies only when Cache-Control is absent.":
    "规则自上而下匹配，首条匹配路径的规则决定 TTL；否则优先遵循 Cache-Control，仅当该头不存在时使用应用默认 TTL。",
  "TTL 0 checks the origin on every request and retains a complete copy. The stale fallback setting controls reuse on failure. Positive TTL rules can cache responses marked no-store or private by the origin.":
    "TTL 为 0 时每次请求先回源，并保留完整副本；失败时是否回退由过期缓存开关决定。正数 TTL 规则可缓存上游标为 no-store 或 private 的响应。",
  "Used only when no path rule matches and Cache-Control is absent. TTL 0 checks the origin every time and retains a complete copy; the stale fallback setting controls reuse on failure.":
    "仅在没有匹配路径规则且没有 Cache-Control 时使用；TTL 为 0 时每次先回源并保留完整副本，失败时是否回退由过期缓存开关决定。",
  "Use stale cache on origin failure": "回源失败时使用过期缓存",
  "Enabled by default for all paths, including TTL 0. Disabling fallback returns an error on origin failure and keeps stored files.":
    "默认开启，适用于所有路径，包括 TTL 为 0 的路径；关闭后回源失败会返回错误，但保留已存储文件。",
  "Rule {number}": "规则 {number}",
  Availability: "启停状态",
  s: "秒",
  min: "分钟",
  h: "小时",
  d: "天",
  "Reorder {name}, position {position} of {count}":
    "调整{name}顺序，第 {position} 项，共 {count} 项",
  "Moved {name} to position {position} of {count}.":
    "已将{name}移至第 {position} 项，共 {count} 项。",
  "Reordering cancelled.": "已取消排序。",
  "Drag the handle to reorder. With the handle focused, use Up/Down or Home/End. Escape cancels a drag.":
    "拖拽左侧手柄排序。聚焦手柄后可用上下方向键或 Home/End，按 Escape 取消拖拽。",
  "Remove rule": "移除规则",
  "TTL (seconds)": "TTL（秒）",
  "Cache-Control determines freshness; the application default TTL applies only when that header is absent.":
    "Cache-Control 决定时效；仅当该头不存在时使用应用默认 TTL。",
  "Patterns may contain at most 1,024 UTF-8 bytes.":
    "路径模式最多包含 1,024 个 UTF-8 字节。",
  "Default TTL without Cache-Control (seconds)":
    "无 Cache-Control 时的默认 TTL（秒）",
  "Upstream sources": "回源地址列表",
  "Add 1 to 16 HTTP or HTTPS directory URLs. Drag the left handle to reorder. Duplicate URLs are rejected.":
    "添加 1 至 16 个 HTTP 或 HTTPS 目录地址，拖拽左侧手柄排序；不允许重复地址。",
  "Source URL {number}": "回源地址 {number}",
  "Drag to reorder": "拖拽排序",
  "Remove source": "移除地址",
  "Add source": "添加地址",
  "Source selection": "回源选择策略",
  "In order": "按顺序",
  "Round robin": "轮询",
  Random: "随机",
  "Add TTL rule": "添加 TTL 规则",
  "Automatic cleanup rules": "自动清理规则",
  "The first matching path rule owns the file. If its age is not reached, later rules do not apply.":
    "文件由首条匹配路径的规则处理；尚未达到保留时长时，也不会继续匹配后续规则。",
  "Saved rules run every 15 minutes for current active sources only. Each application pass scans at most 1,000 files and retires at most 100.":
    "保存后的规则每 15 分钟执行一次，仅针对当前启用的来源；每轮每应用最多扫描 1,000 个文件、退役 100 个文件。",
  "Files accessed during cleanup are checked again and retained.":
    "清理期间再次被访问的文件会重新检查并保留。",
  "Automatic cleanup is disabled until rules are added and saved.":
    "添加并保存规则前，自动清理保持关闭。",
  "Add automatic cleanup rule": "添加自动清理规则",
  "Up to 32 rules per list. Save explicitly to apply these rules.":
    "每个列表最多 32 条规则，点击保存后才会生效。",
  "Save cache rules": "保存缓存规则",
  "Automatic cleanup service": "自动清理服务",
  "Service-wide last pass; includes all configured applications.":
    "服务最近一轮的状态，包含所有已配置应用。",
  "Cleanup running": "清理正在执行",
  "Cleanup idle": "清理空闲",
  "Last attempt": "最近尝试时间",
  "Last successful pass": "最近成功轮次",
  "Last failed pass": "最近失败轮次",
  "{apps} configured applications · {scanned} files scanned · {retired} files retired · {size}":
    "{apps} 个已配置应用 · 已扫描 {scanned} 个文件 · 已退役 {retired} 个文件 · {size}",
  "{passes} total passes · {failures} failed passes":
    "共 {passes} 轮 · 失败 {failures} 轮",
  "Cache source": "缓存来源",
  "Current source": "当前来源",
  "Historical source": "历史来源",
  "Source {epoch}": "来源代次 {epoch}",
  "Refresh sources": "刷新来源",
  "Historical sources retain cached files after the upstream URL changes. Cleanup affects only the selected source.":
    "回源地址更改后，历史来源仍保留缓存文件；清理仅影响所选来源。",
  "Cached files": "缓存文件",
  "Freshness determines revalidation. Cleanup retires stored files separately.":
    "时效决定何时重新验证；清理操作独立退役已存储文件。",
  File: "文件",
  Size: "大小",
  Generation: "代次",
  "Fetched at": "获取时间",
  "Last accessed": "最后访问时间",
  "Validated / fresh until": "验证时间 / 新鲜期截止",
  "No cached files yet. Files are fetched on demand.":
    "尚无缓存文件，文件会在请求时按需回源。",
  "Last access is stored conservatively in minute buckets. Times are shown in your local time zone.":
    "最后访问时间按分钟桶保守记录，所有时间以本地时区显示。",
  "Time-based cleanup": "按时间清理",
  "Select files by": "文件筛选依据",
  "Fetched-time cleanup can retire files that are still frequently accessed.":
    "按获取时间清理可能退役仍被频繁访问的文件。",
  "Files accessed after the preview are checked again and skipped when you confirm.":
    "确认时会再次检查访问时间，跳过预览后又被访问的文件。",
  "Before local time": "早于本地时间",
  "UTC cutoff": "UTC 截止时间",
  "{count} files · {size} logical bytes · {active} active":
    "{count} 个文件 · 逻辑大小 {size} · {active} 个活跃文件",
  "Preview expires": "预览过期时间",
  "Retired {count} of {selected} files · {size} logical bytes":
    "已退役 {selected} 个所选文件中的 {count} 个 · 逻辑大小 {size}",
  "Skipped: {accessed} accessed since preview; {changed} changed generations.":
    "跳过：{accessed} 个预览后被访问的文件；{changed} 个代次已变化的文件。",
  "Vendors and applications": "厂商与应用",
  "Manage application details, providers and availability.":
    "管理应用资料、Provider 与启用状态。",
  "Add vendor": "添加厂商",
  "Add application": "添加应用",
  "Vendor details": "厂商资料",
  "Application details": "应用资料",
  "IDs, vendor and provider are fixed after creation.":
    "创建后不能更改 ID、所属厂商与 Provider。",
  "Deleted. Stored data is retained; this record is read-only.":
    "已删除；保留存储数据，此记录仅供查看。",
  "Changes saved.": "更改已保存。",
  ID: "ID",
  Name: "名称",
  Description: "描述",
  Icon: "图标",
  Provider: "Provider",
  "Base URL": "回源基础地址",
  "Cache TTL (seconds)": "缓存时长（秒）",
  "Remove icon": "移除图标",
  "Uploading…": "正在上传…",
  "The uploaded file exceeds the size limit.": "上传文件超过大小限制。",
  "Use lowercase letters, numbers and single hyphens. This ID cannot be changed later.":
    "使用小写字母、数字和单个连字符，创建后不可更改。",
  "Files are fetched relative to this HTTP or HTTPS directory URL.":
    "文件路径相对于此 HTTP 或 HTTPS 目录地址。",
  "The provider supplies a default upstream URL. You can replace it for this application.":
    "Provider 提供默认回源地址，可为此应用覆盖。",
  Enabled: "启用",
  Disabled: "已禁用",
  Deleted: "已删除",
  Show: "显示",
  Current: "当前记录",
  "No vendors in this view.": "当前视图没有厂商。",
  "Disabling a vendor hides all its applications. Stored data is retained.":
    "禁用厂商会隐藏其下所有应用，保留存储数据。",
  "Disabled applications remain manageable here. Stored data is retained.":
    "禁用应用仍可在此管理，保留存储数据。",
  "Save changes": "保存更改",
  Delete: "删除",
  "Confirm deletion": "确认删除",
  "Deletion disables access and keeps stored data. The ID remains reserved.":
    "删除会停止访问并保留存储数据，ID 仍被保留。",
  "Delete applications under this vendor first.": "请先删除此厂商下的应用。",
  "Download files": "下载文件",
  "Download URL prefix": "下载地址前缀",
  "Append the relative file path to this address. Files are fetched and cached when requested.":
    "在此地址后添加文件的相对路径，文件会在请求时回源并缓存。",
  Resources: "资源",
  "Previous page": "上一页",
  "Next page": "下一页",
  "Page {page}": "第 {page} 页",
  "Version pages": "版本分页",
  "Resource pages": "资源分页",
  "Event pages": "事件分页",
  "A newer state conflicts with this request. Reload and try again.":
    "当前状态与请求冲突，请重新加载后重试。",
  "This cleanup preview is no longer valid. Create a new preview and confirm it again.":
    "清理预览已失效，请重新生成预览并再次确认。",
  "This request origin is not allowed.": "请求来源不被允许。",
  "Download capacity is busy. Try again later.": "下载容量已满，请稍后重试。",
  "Upstream metadata could not be verified.": "无法验证上游元数据。",
  "The artifact exceeds the configured size limit.": "文件超过配置的大小限制。",
  "The upstream service is unavailable. Try again later.":
    "上游服务暂不可用，请稍后重试。",
  "Local storage is unavailable. Try again later.":
    "本地存储暂不可用，请稍后重试。",
  "This operation is not supported.": "不支持此操作。",

  "Channel TTL (seconds)": "渠道缓存有效期（秒）",
  "Channel TTL saved": "渠道缓存有效期已保存",
  "Page not found": "页面不存在",
  "Discard unsaved changes?": "放弃尚未保存的更改？",
  "Settings changed elsewhere. Your draft is preserved. Reload before saving again.":
    "设置已在其他地方修改。草稿已保留，请重新加载后再保存。",
  "Public URL": "公开地址",
  "Public URL override": "公开地址覆盖值",
  "Effective URL": "生效地址",
  "Configuration source": "配置来源",
  "Administrator override": "后台覆盖",
  Environment: "环境变量",
  "Request origin": "请求来源",
  "Environment URL": "环境变量地址",
  "No environment URL": "未设置环境变量地址",
  "Clear override": "撤销覆盖",
  "Leave empty to use the environment URL or the safe request origin.":
    "留空以使用环境变量地址或安全请求来源。",
  "Public URL saved.": "公开地址已保存。",
  "Site information unavailable.": "站点信息暂不可用。",
  "This command downloads and runs an installer. Use a service you trust.":
    "此命令会下载并运行安装器，请使用可信的下载服务。",
  "Start {command} and follow your provider’s sign-in instructions.":
    "运行 {command}，并按照服务提供商的说明登录。",

  Application: "应用",
  "Install {name}": "安装 {name}",
  "Install Claude Code": "安装 Claude Code",
  "Anthropic’s coding agent for your terminal.": "Anthropic 的终端编程助手。",
  "Installs latest. Use stable or a version to pin your installation.":
    "默认安装 latest；可选择 stable 或指定版本。",
  "Start claude through its managed launcher and follow your organization’s sign-in instructions.":
    "通过受管理的启动入口运行 claude，并按组织要求登录。",
  "The service verifies Anthropic’s signed manifest; the installer verifies the binary hash. The managed launcher disables official updates. Signing in and using Claude Code still requires its provider’s services.":
    "服务验证 Anthropic 签名清单，安装器验证二进制摘要。受管理的启动入口会禁用官方更新；登录和使用 Claude Code 仍需访问其开发商服务。",
  "Trust this HTTPS service. Run claude through its managed launcher; executing the version binary directly bypasses update control. Upgrade by rerunning this installer.":
    "请使用可信的 HTTPS 服务，通过受管理的入口启动 claude；直接运行版本二进制会绕过更新控制。重新运行安装器即可升级。",
  "latest and stable channels expire automatically. Signed version manifests remain cached.":
    "latest 与 stable 渠道自动过期；签名版本清单持续缓存。",
  "Site appearance": "站点外观",
  "Public text for each language. Plain text only; the project link always points to RedApp.":
    "分别设置中英文公开文案。仅支持纯文本；项目来源链接固定指向 RedApp。",
  "Site settings saved.": "站点设置已保存。",
  "Site title": "站点标题",
  "Site subtitle": "站点副标题",
  "Footer notice": "页脚声明",
  "Save site settings": "保存站点设置",
  "This command downloads and runs an installer that verifies package hashes. Use a service you trust. Signing in and using Codex still requires its provider’s services.":
    "此命令会下载并运行安装器，并校验安装包摘要。请使用可信的下载服务；登录和使用 Codex 仍需要访问其开发商的服务。",

  "OpenAI brand mark": "OpenAI 品牌标志",
  Language: "语言",
  Applications: "应用",
  Administrator: "管理后台",
  Administration: "管理控制台",
  Account: "账号",
  "Account actions": "账号操作",
  Overview: "概览",
  Versions: "版本",
  Events: "事件",
  Settings: "设置",
  "Public installation page": "公共安装页面",
  "Skip to content": "跳到主要内容",
  Version: "版本",
  Architecture: "架构",
  "Version unavailable": "版本暂不可用",
  "Loading…": "加载中…",
  Close: "关闭",
  Cancel: "取消",
  Retry: "重试",
  Reload: "重新加载",
  Save: "保存",
  "Saving…": "保存中…",
  "Sign in": "登录",
  "Signing in…": "登录中…",
  "Sign in to RedApp": "登录 RedApp",
  "Administrator access": "管理员登录",
  "Use the password from the first initialization logs. Change it after signing in.":
    "使用首次初始化日志中的密码，登录后请及时修改。",
  Password: "密码",
  "Current password": "当前密码",
  "New password": "新密码",
  "Confirm new password": "确认新密码",
  "Change password": "修改密码",
  "Change password and sign out": "修改密码并退出",
  "Sign out": "退出登录",
  "Your new password must contain at least 12 UTF-8 bytes. All sessions will be signed out.":
    "新密码至少需要 12 个 UTF-8 字节。修改后所有会话都会退出登录。",
  "Passwords do not match.": "两次输入的新密码不一致。",
  "The new password is too short.": "新密码长度不足。",
  "Password changed. Sign in again.": "密码已修改，请重新登录。",
  "Signed out": "已退出登录",
  "Your session expired. Sign in again.": "会话已过期，请重新登录。",
  "Request failed. Try again.": "请求失败，请重试。",
  "Invalid server response. Reload and try again.":
    "服务器返回了无效数据，请重新加载后重试。",
  All: "全部",
  "Add a vendor, then create an application and choose its provider. Built-in applications start disabled.":
    "添加厂商后创建应用并选择提供方。内置应用默认禁用。",
  Breadcrumb: "导航路径",
  "About {count} download clients": "约 {count} 个下载客户端",
  "Search applications": "搜索应用",
  "Search suggestions": "搜索建议",
  Vendor: "厂商",
  "Search unavailable. Press Enter to open all applications.":
    "搜索暂不可用，按回车打开全部应用。",
  "No suggestions. Press Enter to search all applications.":
    "暂无建议，按回车搜索全部应用。",
  "Pinned applications": "置顶应用",
  "No pinned applications.": "暂无置顶应用。",
  "Popular downloads": "下载排行榜",
  "Approximate unique download clients over the last seven days, using hourly summaries.":
    "近七天下载客户端的近似去重数量，按小时汇总。",
  "No downloads yet.": "暂无下载记录。",
  "Choose application keys in display order. Disabled applications remain saved here and are hidden publicly.":
    "按展示顺序填写应用完整键。禁用的应用会保留在此列表中，公开页面不展示。",
  "Application key": "应用完整键",
  Remove: "移除",
  "Reset selected fields to template": "将所选字段重置为模板值",
  "Select fields, review the differences, then save. Identity and provider stay fixed; stored files and history are retained.":
    "选择字段、检查差异后再保存。标识和提供方保持不变，已存文件和历史记录保留。",
  "Name and description": "名称和描述",
  "Cache settings": "缓存设置",
  "English instructions": "英文说明",
  "Chinese instructions": "中文说明",
  "Template value": "模板值",
  "Review differences": "检查差异",
  "Save selected fields": "保存所选字段",
  "Permanently delete this record and its files, settings and history? This cannot be undone.":
    "永久删除此记录及其文件、设置和历史？此操作无法撤销。",
  "Permanently delete this record and its files, settings and history? Active downloads, uploads and background tasks of this application will be interrupted. This cannot be undone.":
    "永久删除此记录及其文件、设置和历史？此应用正在进行的下载、上传和后台任务将被中断。此操作无法撤销。",
  "Markdown, HTML, JavaScript and external resources are supported. Only administrators can edit these instructions.":
    "支持 Markdown、HTML、JavaScript 和外部资源，仅管理员可编辑说明。",
  "Disabled by vendor": "厂商已禁用",
  "Proxy URL": "代理 URL",
  "Enter one complete URL with percent-encoded credentials. The saved URL is visible to administrators. Empty means direct.":
    "输入包含百分号编码凭据的完整 URL。管理员可查看已保存的 URL。留空表示直连。",
  "Connection failed. Check your connection and retry.":
    "连接失败，请检查网络后重试。",
  "Request rejected. Check your input and retry.":
    "请求被拒绝，请检查输入后重试。",
  "Permission check failed. Refresh the page and sign in again.":
    "权限校验失败，请刷新页面并重新登录。",
  "Login failed or rate limit exceeded": "登录失败或请求过于频繁，请稍后重试。",
  "Service temporarily unavailable. Try again.": "服务暂时不可用，请稍后重试。",
  "Requested data is unavailable.": "请求的数据暂不可用。",
  Refresh: "刷新",
  "Refreshing…": "刷新中…",
  "Auto refresh": "自动刷新",
  On: "开启",
  Off: "关闭",
  "Refresh every 5 seconds": "每 5 秒刷新",
  Snapshot: "快照",
  "Local time": "本地时间",
  "Distribution overview": "分发概览",
  "Monitor downloads, storage and service activity.":
    "查看下载、存储及服务运行状况。",
  "Inspect cached versions and active resource generations.":
    "查看已缓存版本及资源代次。",
  "Review recent download and upstream failures.": "查看近期下载及回源失败。",
  "Configure upstream access and maintain your cache.":
    "配置上游访问并维护缓存。",
  "Loading service status…": "正在加载服务状态…",
  "Status unavailable. Retry to reconnect.": "服务状态暂不可用，请重试连接。",
  "Showing the last successful snapshot.": "当前显示最近一次成功获取的快照。",
  "Current value": "当前值",
  "Cumulative total": "累计值",
  "View history": "查看历史",
  "View {name} history": "查看{name}历史",
  "All metrics": "全部指标",
  "Common metrics": "常用指标",
  "Diagnostic metrics": "诊断指标",
  "Detailed troubleshooting metrics. Collection and history remain available.":
    "用于深入排障；继续采集并保留历史。",
  "Capacity, downloads and active transfers. Select a value to view history.":
    "容量、下载与活动传输；点击数值查看历史。",
  "Includes all applications; matching version names count separately.":
    "包含所有应用；不同应用的相同版本名分别计数。",
  "Includes only this application.": "仅包含当前应用。",
  "{count} metrics": "{count} 项指标",
  "Select a metric to explore its history.": "选择指标查看历史变化。",
  Disk: "磁盘",
  "Traffic and requests": "流量与请求",
  Speed: "速率",
  Runtime: "运行状态",
  "Resources and tasks": "资源与任务",
  "Service disk usage": "服务磁盘用量",
  "Total logical file bytes": "文件逻辑总量",
  "Allocated cache": "缓存实际占用",
  "Allocated temporary files": "临时文件实际占用",
  "Allocated pending deletion": "待删除实际占用",
  Cache: "缓存",
  Temporary: "临时文件",
  "Pending deletion": "待删除",
  "Other allocated disk usage": "其他磁盘占用",
  "Filesystem available": "文件系统可用空间",
  "Public requests": "公共下载请求",
  "Artifact requests": "制品请求",
  "Cache-hit requests": "缓存命中请求",
  "Shared-follower requests": "共享下载跟随请求",
  "Miss requests": "缓存未命中请求",
  "Reused requests": "复用请求",
  "Successful downloads": "成功下载",
  "Download errors": "下载错误",
  "Artifact upstream errors": "制品回源错误",
  "Artifact upstream traffic (HTTP payload)": "制品回源流量（HTTP 载荷）",
  "Artifact downstream traffic (before compression)": "制品分发流量（压缩前）",
  "Logical bytes reclaimed": "已回收逻辑空间",
  "Artifact upstream · recent (HTTP payload)": "近期回源速率（HTTP 载荷）",
  "Artifact downstream · recent (before compression)": "近期分发速率（压缩前）",
  "Memory allocated": "已分配内存",
  Goroutines: "Go 协程数",
  "Process uptime": "进程运行时间",
  "Resource generations": "资源代次总数",
  "Current generations": "当前代次",
  "Retired generations": "已退役代次",
  "Active readers": "活跃读取者",
  "Active writers": "活跃写入者",
  "Queued generations": "排队代次",
  "Downloading generations": "下载中代次",
  "Resuming generations": "续传中代次",
  "Retry-wait generations": "等待重试代次",
  "Verifying generations": "校验中代次",
  "Complete generations": "完成代次",
  "Failed generations": "失败代次",
  "Invalid generations": "无效代次",
  "Interrupted generations": "中断代次",
  "Discovered versions": "已发现版本",
  "Recent failures (up to 100)": "近期失败（最多 100 条）",
  "Install Codex": "安装 Codex",
  "Install Codex CLI": "安装 Codex CLI",
  Copy: "复制",
  "Copy {name} command": "复制 {name} 命令",
  "Command copied": "命令已复制",
  "Copy unavailable. Select and copy the command manually.":
    "无法访问剪贴板，请选择命令并手动复制。",
  "Uses CODEX_RELEASE if set, otherwise latest.":
    "优先使用 CODEX_RELEASE，否则使用 latest。",
  "Run in your terminal": "在终端中运行",
  "Installers verify hashes and suppress the automatic-update marker. The CLI binary is unchanged; runtime/API traffic requires your enterprise egress policy.":
    "安装器会校验摘要并抑制自动更新标记。CLI 二进制保持原样；运行期及 API 流量仍需遵循企业出口策略。",
  "OpenAI’s coding agent for your terminal.":
    "在终端中使用 OpenAI 编程智能体。",
  "Installation instructions": "安装说明",
  "All applications": "全部应用",
  "No applications are available.": "暂无可用应用。",
  "Unable to load applications": "无法加载应用",
  "Loading applications…": "正在加载应用…",
  "Getting started": "开始使用",
  "Open a terminal on Linux or macOS, or PowerShell on Windows.":
    "在 Linux 或 macOS 打开终端；在 Windows 打开 PowerShell。",
  "Copy and run the matching command. Review the prompts before confirming installation.":
    "复制并运行对应命令，确认安装前请阅读提示。",
  "Start codex and follow your organization’s sign-in instructions.":
    "运行 codex，并按照组织要求登录。",
  "Review before installing": "安装前审查",
  "These commands download and execute the installer immediately. Download and review the script first if your policy requires it.":
    "这些命令会立即下载并执行安装器。如果组织要求先行审查，请先下载并阅读脚本。",
  "Download Shell script": "下载 Shell 脚本",
  "Download PowerShell script": "下载 PowerShell 脚本",
  "Clipboard access requires HTTPS or localhost. You can also select the command manually.":
    "剪贴板访问需要 HTTPS 或 localhost，也可手动选择命令。",
  "Windows/macOS installation should be validated by your IT administrator before rollout.":
    "Windows/macOS 安装应由 IT 管理员在推广前完成验证。",
  "Independent distribution service. Not affiliated with or endorsed by OpenAI.":
    "独立分发服务，与 OpenAI 无隶属关系，也未获得其背书。",
  "Versions and resources": "版本与资源",
  "All versions": "全部版本",
  "First seen": "首次发现",
  "{count} artifact requests": "{count} 次制品请求",
  "No versions discovered yet. Downloads are fetched on demand.":
    "尚未发现版本，制品会在请求时按需下载。",
  "Average effective speed includes retries and excludes verification; recent speed is a five-second snapshot.":
    "平均有效速率包含重试耗时、不含校验；近期速率取最近五秒。",
  "Version / file": "版本 / 文件",
  "Generation / state": "代次 / 状态",
  "On disk / readers": "磁盘占用 / 读取者",
  "Average / recent": "平均 / 近期速率",
  "Resumes / verification": "续传 / 校验耗时",
  "Started / finished": "开始 / 完成",
  "Last error": "最近错误",
  "No error": "无错误",
  "No cached resources for this selection.": "当前筛选下没有缓存资源。",
  Queued: "排队中",
  Downloading: "下载中",
  Resuming: "续传中",
  "Waiting to retry": "等待重试",
  Verifying: "校验中",
  Complete: "已完成",
  Failed: "失败",
  Invalid: "无效",
  Interrupted: "已中断",
  "Recent failures": "近期失败",
  "No recent failures.": "近期没有失败记录。",
  Time: "时间",
  "Category / status": "类别 / 状态",
  Resource: "资源",
  Message: "消息",
  "Technical details": "技术详情",
  "Original diagnostic messages are shown as reported by the server.":
    "原始诊断消息按服务器返回内容显示。",
  "Metadata freshness": "元数据时效",
  "latest TTL (seconds)": "latest 缓存时长（秒）",
  "Save TTL": "保存缓存时长",
  "latest TTL saved": "latest 缓存时长已保存",
  "Only latest metadata expires automatically. Other valid versions remain cached.":
    "只有 latest 元数据会自动过期，其他有效版本持续保留缓存。",
  "Version cleanup": "版本清理",
  "Minimum version to keep": "最低保留版本",
  "Preview cleanup": "预览清理",
  "Confirm this preview": "确认清理预览",
  "Confirm cleanup": "确认清理",
  "Selected generations": "已选资源代次",
  "Unknown versions retained": "保留的未知版本",
  "{count} generations · {size} logical bytes · {active} active":
    "{count} 个代次 · 逻辑大小 {size} · {active} 个活跃代次",
  "Estimated reclaimable complete cache: {size}": "预计可回收完整缓存：{size}",
  "Only previewed generations are retired. Later generations remain available. Existing readers and writers drain before disk space is reclaimed.":
    "仅退役预览选中的代次，后续新代次不受影响。现有读取者和写入者结束后才回收磁盘空间。",
  "Cleanup executed; space is reclaimed after existing readers and writers finish":
    "清理已执行；现有读取者和写入者结束后回收空间",
  "Review the selected generations before confirming":
    "请检查选中的资源代次，再确认清理",
  "Upstream proxy": "回源代理",
  "Optional HTTP / HTTPS / SOCKS5 proxy for metadata, artifacts and resume requests. Empty means direct; environment proxy variables are ignored. SOCKS5 uses proxy-side DNS; the proxy must be trusted.":
    "可为元数据、制品和续传请求设置 HTTP / HTTPS / SOCKS5 代理。留空为直连，不读取环境代理变量。SOCKS5 在代理端解析 DNS，请仅使用可信代理。",
  "Loading proxy settings…": "正在加载代理配置…",
  "Proxy server": "代理地址",
  "Credentials saved (never displayed)": "已保存凭据（不回显）",
  "No saved credentials": "未保存凭据",
  "Saved credentials": "已保存的凭据",
  "Keep saved credentials": "保留已存凭据",
  "Replace credentials": "替换凭据",
  "Clear credentials": "清除凭据",
  Username: "用户名",
  "Changing the server requires clearing or replacing saved credentials. Credentials are stored in the local SQLite database under data-directory permissions; no extra storage encryption is applied.":
    "修改代理地址时需清除或替换凭据。凭据存于本地 SQLite，由数据目录权限保护，没有额外存储加密。",
  "Save proxy": "保存代理",
  "Proxy saved; new upstream requests use this configuration. Active transfers continue.":
    "代理已保存，新回源请求将使用此配置，正在进行的传输继续执行。",
  "Metric history · UTC": "指标历史 · UTC",
  "Close history": "关闭历史",
  "History window": "历史范围",
  "24 hours": "24 小时",
  "7 days": "7 天",
  "30 days": "30 天",
  "Counter view": "计数器视图",
  "Cumulative last value": "最近累计值",
  "Observed increment": "观测增量",
  "Last cumulative value": "最近累计值",
  "Observed average / value": "观测均值 / 值",
  "Observed minimum": "观测最小值",
  "Observed maximum": "观测最大值",
  "Observed increment (delta)": "观测增量（delta）",
  "Last cumulative value (last)": "最近累计值（last）",
  "Five-second observation (value)": "五秒窗口观测值（value）",
  "Observation (value)": "观测值（value）",
  "Observed weighted average (avg)": "观测窗口加权均值（avg）",
  "Observed average (avg)": "观测均值（avg）",
  "Observed minimum (min)": "观测最小值（min）",
  "Observed maximum (max)": "观测最大值（max）",
  "Hover or tap to inspect a bucket. Focus the chart and use Left/Right, Home/End; Escape clears the selection.":
    "悬停或轻触可查看时间桶。聚焦图表后可用左右方向键、Home/End 选点，Escape 清除选择。",
  "Bucket start · browser local time": "时间桶起点 · 浏览器本地时间",
  "Minute observation": "分钟观测",
  "Hourly aggregate": "小时汇总",
  "Valid intervals: {count}": "有效间隔：{count}",
  "No observation in this bucket. Missing values are not zero.":
    "此时间桶没有观测，缺失值不代表零。",
  "Increment unknown: no valid adjacent observation interval.":
    "增量未知：没有有效的相邻观测间隔。",
  "Select a chart point to see exact values.": "选择图表数据点以查看精确数值。",
  "Loading history…": "正在加载历史…",
  "Retry history": "重试加载历史",
  "Minute observations · last 24 hours": "分钟观测 · 最近 24 小时",
  "Hourly aggregates": "小时汇总",
  "UTC buckets. Missing observations remain gaps.":
    "按 UTC 分桶，缺失观测保留为空缺。",
  "No observations available for this window.": "当前时间范围内没有观测数据。",
  "{name} over {range}. Values and coverage are available in the table below.":
    "{name}在{range}内的变化；下方表格提供数值与覆盖情况。",
  "Cumulative counters are never averaged. Increments cover only adjacent valid samples; restart, reset and long gaps have unknown increments.":
    "累计计数器不取平均值。增量仅覆盖相邻有效样本；重启、归零或长时间缺测期间的增量未知。",
  "Each observation covers five seconds, sampled once per minute. Hourly average weights those observed windows; it is not the whole-hour transfer rate.":
    "每分钟采样一次，每次观测覆盖五秒。小时均值按这些观测窗口加权，不代表整小时传输速率。",
  "Green: observed average/value. Blue: minimum. Brown: maximum. Hourly values summarize available samples, not missing intervals.":
    "绿色：观测均值/值；蓝色：最小值；棕色：最大值。小时数据仅汇总有效样本，不填补缺测区间。",
  "Current bucket is partial: {count} observations.":
    "当前桶尚未结束：{count} 次观测。",
  "Observed duration: {seconds} seconds.": "观测时长：{seconds} 秒。",
  "Observation values and coverage ({count} buckets)":
    "观测值与覆盖情况（{count} 个桶）",
  "UTC bucket": "UTC 时间桶",
  "Value / last": "数值 / 最近值",
  "Min / max / average": "最小 / 最大 / 平均",
  Samples: "样本数",
  "Increment / valid intervals": "增量 / 有效间隔",
  "Observed seconds / coverage": "观测秒数 / 覆盖情况",
  "Partial current bucket": "当前桶未结束",
  "Incomplete observations": "观测不完整",
  "Complete observations": "观测完整",
} as const;
export type Message = keyof typeof messages;
export function t(
  key: Message,
  params: Record<string, string | number> = {},
): string {
  const value = language.value === "zh-CN" ? messages[key] : key;
  return value.replace(/\{(\w+)\}/g, (_, name: string) =>
    String(params[name] ?? `{${name}}`),
  );
}
export function label(value: string): string {
  return Object.hasOwn(messages, value) ? t(value as Message) : value;
}
const errorCodes: Record<string, Message> = {
  NETWORK_ERROR: "Connection failed. Check your connection and retry.",
  RESOURCE_CONFLICT:
    "The file changed. Select the current file explicitly before replacing it.",
  PREVIEW_INVALID:
    "This preview is no longer available. Create a new preview and try again.",
  REFRESH_INVALID:
    "The refresh selection changed or is no longer available. Create a new preview and try again.",
  PAYLOAD_TOO_LARGE: "The uploaded file exceeds the size limit.",
  DIRECTORY_DELETE_PENDING:
    "Deletion is not complete. This application is blocked while its tasks stop. Retry deletion; restarting also resumes it.",
  DIRECTORY_REVISION_CONFLICT:
    "Settings changed elsewhere. Your draft is preserved. Reload before saving again.",
  AUTH_REQUIRED: "Your session expired. Sign in again.",
  CSRF_REJECTED: "Permission check failed. Refresh the page and sign in again.",
  ORIGIN_REJECTED: "This request origin is not allowed.",
  SETTINGS_REVISION_CONFLICT:
    "Settings changed elsewhere. Your draft is preserved. Reload before saving again.",
  CLEANUP_INVALID:
    "This cleanup preview is no longer valid. Create a new preview and confirm it again.",
  INVALID_REQUEST: "Request rejected. Check your input and retry.",
  INVALID_QUERY: "Request rejected. Check your input and retry.",
  INVALID_RESPONSE: "Invalid server response. Reload and try again.",
  INVALID_PATH: "Request rejected. Check your input and retry.",
  APPLICATION_NOT_FOUND: "Requested data is unavailable.",
  RESOURCE_NOT_FOUND: "Requested data is unavailable.",
  LOGIN_RATE_LIMITED: "Login failed or rate limit exceeded",
  METHOD_NOT_ALLOWED: "This operation is not supported.",
  DOWNLOAD_CAPACITY_EXCEEDED: "Download capacity is busy. Try again later.",
  METADATA_UNTRUSTED: "Upstream metadata could not be verified.",
  ARTIFACT_TOO_LARGE: "The artifact exceeds the configured size limit.",
  UPSTREAM_UNAVAILABLE: "The upstream service is unavailable. Try again later.",
  LOCAL_STORAGE_UNAVAILABLE: "Local storage is unavailable. Try again later.",
};
export function errorText(reason: unknown): string {
  if (reason instanceof Error && reason.name === "AbortError") return "";
  const code =
    reason && typeof reason === "object" && "code" in reason
      ? reason.code
      : undefined;
  if (typeof code === "string" && Object.hasOwn(errorCodes, code))
    return t(errorCodes[code]!);
  const status =
    reason && typeof reason === "object" && "status" in reason
      ? Number(reason.status)
      : 0;
  if (status === 401) return t("Your session expired. Sign in again.");
  if (status === 403)
    return t("Permission check failed. Refresh the page and sign in again.");
  if (status === 429) return t("Login failed or rate limit exceeded");
  if (status === 400) return t("Request rejected. Check your input and retry.");
  if (status === 409)
    return t(
      "A newer state conflicts with this request. Reload and try again.",
    );
  if (status === 404) return t("Requested data is unavailable.");
  if (status >= 500) return t("Service temporarily unavailable. Try again.");
  return t("Unexpected error. Reload and try again.");
}
export function localDate(value: string | number | undefined): string {
  if (!value || String(value).startsWith("0001-")) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "—"
    : new Intl.DateTimeFormat(language.value, {
        year: "numeric",
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        timeZoneName: "short",
      }).format(date);
}
export function utcDate(value: number): string {
  return new Intl.DateTimeFormat(language.value, {
    timeZone: "UTC",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value * 1000));
}
const states: Record<string, Message> = {
  queued: "Queued",
  downloading: "Downloading",
  resuming: "Resuming",
  retry_wait: "Waiting to retry",
  verifying: "Verifying",
  complete: "Complete",
  failed: "Failed",
  invalid: "Invalid",
  interrupted: "Interrupted",
};
export function stateLabel(value: string): string {
  return states[value] ? t(states[value]) : value;
}
