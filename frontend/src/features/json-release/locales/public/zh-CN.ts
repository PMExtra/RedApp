import type { LocaleModule } from "@/shared/i18n";
export default { releaseDownloads: { title:"下载", choice:"平台 · 架构 · 包型", choose:"请选择安装包", hint:"环境识别仅供推荐，请在下载前确认架构与包型。", download:"下载 {package}", version:"版本 {version}", retry:"重试", unavailable:"暂无可用安装包。", os:{windows:"Windows",macos:"macOS",linux:"Linux",unknown:"未知系统"} } } satisfies LocaleModule;
