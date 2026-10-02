# 公共安装页面

用户无需管理员账号即可打开 `/` 浏览应用，目前仅 Codex CLI。`/apps/codex` 显示安装说明、Shell/PowerShell 一行命令与复制按钮；页面右上角提供管理员入口。管理员认证、会话和管理 API 保持原有边界。

安装命令会立即下载并执行本服务的安装器，默认使用 `CODEX_RELEASE` 或 `latest`，不强制非交互，不修改安装器、patch 或 CLI 二进制。详情页也提供脚本下载链接供先行审查；具体指定版本、无人值守和安全限制见[安装详解](README.md#review-before-installing)。复制功能需要 HTTPS 或 localhost；无法访问剪贴板时可手动选择命令。

页脚的 `GET /api/info` 仅提供服务版本、OS 和架构，不含提交信息或管理状态。

`GET /api/apps` 返回固定 Codex 模块的公开 ID、名称、简介、本地图标路径和当前安全 origin，不读取管理员信息、缓存、版本、代理配置或凭据，也不触发 metadata 回源。公共页面不调用管理 API。origin 与安装器使用相同验证规则，支持显式固定地址或可信代理推导；页面与 JSON 均不缓存，避免不同 Host/代理请求互相污染。非法 origin、非规范路径及查询参数沿用拒绝规则。静态脚本、CSS 均由本进程提供，CSP 保持仅本站；无需外部运行服务。

## 品牌标志与许可

公共首页和详情页展示用户提供的 OpenAI 品牌标志，用于标识 Codex CLI 的提供方，不宣称其为 Codex 专属图标。SVG 原文保存在 Codex 模块内部，经 `/apps/codex/icon.svg` 从本服务提供，不依赖外部图片地址；视图仅调整容器与显示尺寸。

准确来源、用户更正、原件 SHA256 和许可状态见[模块素材说明](../internal/apps/codex/assets/README.md)。Commons 来源页标记 PD-textlogo 并注明商标限制；这是来源标签核验，未将用户 SVG 与远程文件逐字节比对，不将素材纳入 RedApp MIT 许可。

v0.4.0 的完整设计见[统一前端](frontend-v0.4.0.md)。CLI、浏览器交互和视觉审阅范围见[验收](acceptance.md)。
