# RedApp

RedApp（Redistribution Application）是 Go 实现的企业按需分发服务。MVP 仅分发 Codex CLI：单个 Linux 进程拥有本地 SQLite 和数据目录，管理页面、企业安装器及许可材料嵌入同一可执行文件，无独立数据库或其他运行服务。

设计基线与已核验的上游契约：[设计](docs/design.md)、[上游契约](docs/upstream-contract.md)。实现与验证记录见 [验收矩阵](docs/acceptance.md)，配置、备份和故障操作见 [运维说明](docs/operations.md)。

## 构建与启动

构建需要 Go 1.25.1、GCC 和 libc 静态开发库。SQLite 使用固定版本的 `go-sqlite3`，以 CGO 静态链接；运行时不依赖 libc 或 SQLite 动态库。禁用 SQLite 扩展加载，使用 Go DNS 和用户查询实现。

```sh
make build
./bin/redapp --data ./data --public-url http://localhost:8080
```

打开配置的 `/admin/` 管理入口。仅首次初始化输出随机管理员密码，数据库保存 bcrypt hash；请立即改密并保护初始化日志。正式企业访问使用反向代理终止 HTTPS。

```sh
docker build -t redapp:local .
docker run --rm --read-only -p 8080:8080 \
  -v redapp-data:/data \
  -e REDAPP_PUBLIC_URL=https://codex.example.internal \
  redapp:local
```

镜像以 UID/GID 65532 运行，只需持久卷可写。反代必须把请求 Host 设置成 public URL 中的 host，详情见运维说明。首版不支持 public URL 子路径。

## 客户端安装

```sh
curl -fsS https://codex.example.internal/install.sh -o install.sh
sh install.sh --release 0.159.2
# 无人值守安装：CODEX_NON_INTERACTIVE=1 sh install.sh
```

```powershell
Invoke-WebRequest https://codex.example.internal/install.ps1 -OutFile install.ps1
./install.ps1 -Release 0.159.2
```

企业脚本保留官方平台识别、安装目录、哈希校验和交互确认。所有安装器自身的元数据与制品请求仅使用配置的企业 origin，禁止公网 fallback 和 HTTP 重定向。latest 和固定版本安装均删除自动更新标记；CLI 二进制保持官方原样。交互确认启动 CLI 或使用既有包管理器处理旧安装属于官方子进程行为，仍须由企业出口控制约束。CLI 内置更新检查/API endpoint 不在本服务改写范围内。

## 验证与安装器维护

```sh
make test check
make build
python3 scripts/test-http-cli.py
sh scripts/test-docker-local.sh
# 固定身份、严格补丁、离线回归；默认仅检查
python3 scripts/update-installers.py --source installers/codex/upstream
# 从不可变官方 commit 获取；全部检查通过后原子替换本地目录
python3 scripts/update-installers.py --apply
```

更换上游 commit 时，维护者必须显式给出已核验的新 shell/PowerShell SHA-256，并审阅原文、patch、许可证和测试差异。摘要不符、patch 偏移/冲突、出口检查或测试失败，均不会替换已发布目录。详细来源和摘要保存在 `installers/codex/provenance.json`。

目前验证包括本地假上游、并发/故障/续传/清理、真实进程 SIGKILL、HTTP 管理 API、25 个 shell 离线安装场景、Docker 本地 runtime。Windows 实机、macOS 实机、生产 CDN 下载链与全 Docker builder 阶段仍未完整验证，不能据此宣称已完成生产兼容性认证。

## 许可

RedApp 原创代码采用 [MIT License](LICENSE)，Copyright (c) 2026 PMExtra。第三方材料保持各自原有许可，不受项目 MIT LICENSE 替代：Codex 官方安装器及其修改版本适用 `installers/codex/upstream/LICENSE` 和 `NOTICE`；构建依赖许可见 [third_party](third_party/README.md)。上游许可文件不是整个仓库的统一许可证。
