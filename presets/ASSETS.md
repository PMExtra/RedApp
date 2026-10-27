# 预置展示图片来源

`presets/assets/` 只保存经审查的静态图片，作为构建嵌入资源；无运行时下载。既有资源的迁移来源列于下表；新增官方资源保留原始字节，不重绘或修改图形。

| 预置路径 | 原始文件及来源记录 |
| --- | --- |
| `assets/builtin/openai.svg` | `internal/apps/codex/assets/openai-symbol.svg`；[来源记录](../internal/apps/codex/assets/README.md) |
| `assets/openai/codex/icon.svg` | 同上 |
| `assets/builtin/anthropic.svg` | `internal/apps/builtin/assets/anthropic-light.svg`；[来源及许可记录](../internal/apps/builtin/assets/README.md) |
| `assets/anthropic/claude-code/icon.svg` | `internal/apps/builtin/assets/anthropic.svg`；同上，Apache-2.0 原文仍在 `third_party/dashboard-icons.LICENSE` |

新增资源须审查来源与许可。加载器校验全部图片和引用，但不以具体厂商名称列白名单。展示资源与安装器协议资源独立；新 inventory 的 icon 为展示 URL，其他安装器维护字段与白名单保持原值。语义 hash 按规范 spec 计算，可信分发摘要单独计算。

## JetBrains 官方展示资源

以下 SVG 来自官方产品网站导航引用的资源端点（[品牌资源页](https://www.jetbrains.com/company/brand/)）。仅用于标识相应厂商及产品，不作为 RedApp 的品牌或背书。资源按原始字节嵌入，无外链图像或运行时下载。

| 对象 | 预置路径 | 官方来源 | SHA256 |
| --- | --- | --- | --- |
| jetbrains | `assets/builtin/jetbrains.svg` | [SVG](https://resources.jetbrains.com/storage/logos/web/jetbrains/jetbrains.svg) | `bcd6f25ef7685642b95f6d69c3554c13e2fbfff44c6e32ef84b9fd87a015caa9` |
| idea | `assets/jetbrains/idea/icon.svg` | [SVG](https://resources.jetbrains.com/storage/logos/web/intellij-idea/intellij-idea.svg) | `666b5bcf7701b9637f23ac2ebcd161566bc53ea67cd381bb9685a302d837a3aa` |
| pycharm | `assets/jetbrains/pycharm/icon.svg` | [SVG](https://resources.jetbrains.com/storage/logos/web/pycharm/pycharm.svg) | `6f002030bb979b9b5a37495d1b973e935c72871ff5a0994e781bf56b74d620d0` |
| clion | `assets/jetbrains/clion/icon.svg` | [SVG](https://resources.jetbrains.com/storage/logos/web/clion/clion.svg) | `24cc46455f04f015d21400fbeb1ef899042c3d8cf4ebb9a0f58fa44153ccac72` |
| webstorm | `assets/jetbrains/webstorm/icon.svg` | [SVG](https://resources.jetbrains.com/storage/logos/web/webstorm/webstorm.svg) | `5f0328195a682aaccb1b8bb7e96f74b89aeea43b9b73760651fc5d0ad523e5db` |
| phpstorm | `assets/jetbrains/phpstorm/icon.svg` | [SVG](https://resources.jetbrains.com/storage/logos/web/phpstorm/phpstorm.svg) | `b50c317b81709ee76159df737b415852ca64bb86ec174547eacacd201253066b` |
| goland | `assets/jetbrains/goland/icon.svg` | [SVG](https://resources.jetbrains.com/storage/logos/web/goland/goland.svg) | `b44448bd9a0486c7f2a59b9ade0c7b8d907911dc1d5eec6ad040d76deb31eb9b` |
| rider | `assets/jetbrains/rider/icon.svg` | [SVG](https://resources.jetbrains.com/storage/logos/web/rider/rider.svg) | `fecc5c5841efe9d89424efa46c45b313312222fa4e422bf13e967bfed9c840ea` |
| datagrip | `assets/jetbrains/datagrip/icon.svg` | [SVG](https://resources.jetbrains.com/storage/logos/web/datagrip/datagrip.svg) | `d940d13d43aaeb0555359ecfd92c9857eda8ccbbdbf307fed3923ad81425bbd6` |
| rubymine | `assets/jetbrains/rubymine/icon.svg` | [SVG](https://resources.jetbrains.com/storage/logos/web/rubymine/rubymine.svg) | `fa07fcdf2fb57496fb008b1f010e649c91b3d4f7ad1fb4904bbae1cc3aa4d3ac` |
| rustrover | `assets/jetbrains/rustrover/icon.svg` | [SVG](https://resources.jetbrains.com/storage/logos/web/rustrover/rustrover.svg) | `e58446ae73e5a597f79eff555b4a3648bd559c015f85b44c6c108bb530315bf8` |

JetBrains 及上述产品的名称和标识属于 JetBrains s.r.o.；版权与商标权归原权利人，本项目不将商标资产重新许可为项目代码许可。官方品牌指南允许按指南介绍 JetBrains 及其产品和产品连接；遵循其要求，不更改形状或颜色、不暗示合作或背书、不比本项目品牌更突出；其他用途应重新核对官方指南。未接受额外协议，未取得单独的商标授权。

## 未嵌入的官方候选与缺口

- Microsoft：官方 [SVG](https://code.visualstudio.com/assets/icons/microsoft.svg) 可下载，但[通用商标指南](https://www.microsoft.com/en-us/legal/intellectualproperty/trademarks)要求 logo 使用获得明确许可；本项目未有此许可记录，因此厂商图标保留为空。

所有新增资源仅做 CLI HTTP、XML/格式、安全性与嵌入引用检查，未作 GUI 或视觉验收。未配置图标的实体按现有组件回退，不代表存在已验证的品牌图像。产品介绍依据对应官方产品页面；不包含免费许可或完整安装验证的承诺。

### dotUltimate 内联官方 SVG

`assets/jetbrains/dotultimate/icon.svg` 提取自[官方套件页面](https://www.jetbrains.com/dotnet/)实际使用的 `Hns` SVG 组件。页面 [dotnet JS](https://www.jetbrains.com/_assets/dotnet/index.entry.7205a0f98d3d3fb698e4.js) 在套件区使用该组件，定义位于[官方 common JS](https://www.jetbrains.com/_assets/common.9054d9112602432d86e0.js)。CLI 静态提取五个 path 的原始 `d`、`fill` 和原始 viewBox，补充 SVG 命名空间后序列化；未执行远端脚本、重绘、替换颜色或拼接其他产品图标。

SHA256：`cda0680d23fc88a313a593481bf62ba2780b4f55bb1d63dca3a6571f6791e41d`。同样遵循 JetBrains 品牌指南；原始下载目录路径 `storage/logos/web/dotultimate/dotultimate.svg` 返回 404，因此使用官网内联 SVG 的真实定义。

## 产品介绍依据

介绍只描述用途，不承诺免费许可、特定版本功能或安装验证结果。官方产品说明：

- [Microsoft](https://www.microsoft.com/)、[Visual Studio Code](https://code.visualstudio.com/)。
- [JetBrains](https://www.jetbrains.com/)、[IntelliJ IDEA](https://www.jetbrains.com/idea/)、[PyCharm](https://www.jetbrains.com/pycharm/)、[CLion](https://www.jetbrains.com/clion/)。
- [WebStorm](https://www.jetbrains.com/webstorm/)、[PhpStorm](https://www.jetbrains.com/phpstorm/)、[GoLand](https://www.jetbrains.com/go/)、[Rider](https://www.jetbrains.com/rider/)。
- [DataGrip](https://www.jetbrains.com/datagrip/)、[RubyMine](https://www.jetbrains.com/ruby/)、[RustRover](https://www.jetbrains.com/rust/)、[dotUltimate](https://www.jetbrains.com/dotnet/)。

## Visual Studio Code stable 图标

`assets/microsoft/vscode/icon.svg` 是[官方图标包](https://code.visualstudio.com/assets/branding/visual-studio-code-icons.zip)中的 `visual-studio-code-icons/vscode.svg`，原始字节不变。只用于目录中识别 Visual Studio Code 产品，独立于 RedApp 标识；没有合成 logo lock-up、重绘或添加颜色滤镜。前端沿用方形应用图标组件，保持比例，与普通标题文字分开。

[官方专属指南](https://code.visualstudio.com/brand)说明蓝色 stable 图标代表 Visual Studio Code，并列出介绍产品、链接官网等允许用途及品牌组合、修改图形等限制。目录未被逐项列举，不据此认定禁止；本项目不宣称获得单独商标授权或 Microsoft 背书。Microsoft 厂商 logo 的缺口独立保留。

官方 SVG 包含 alpha 遮罩与阴影；构建时静态资源校验仅允许有界的本地遮罩/滤镜子集，拒绝外部引用、脚本、图片嵌入和超大滤镜，用户上传与配置导入的 SVG 白名单不变。未进行 GUI 或视觉验收。

SHA256：`74ad401c6487a0dc5af1f6b2935556ab2d3553ae45effc7e1697b4d117d046b6`。
