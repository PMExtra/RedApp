# CI 与发布产物

`CI` 的 frontend job 执行类型/DOM 回归并构建一次，上传 `frontend-<SHA>`（web.tar.gz 与 SHA256SUMS）。两个原生源码 test job 并行保留 vet/race/安装器检查；两个 runtime job 消费本次 run 的前端，在各自原生架构的固定 Debian 工具链容器内调用共享 `scripts/build-binary.sh`，通过 `make runtime-test` 执行真实 HTTP/数据/文档、retention/prewarm/taxonomy/exchange CLI，并执行 Docker 测试。Windows PowerShell 7/5.1 独立并行。

可信 PMExtra/RedApp main push 的 runtime job 才上传 `runtime-<SHA>-<arch>`，包含 image.tar 与 metadata.json。归档为已测试的 scratch 镜像，保留 3 天。PR 运行完整验证但不会产生可发布运行产物。源码测试 Go setup 缓存按 runner OS/架构、Go 版本和 go.sum 分键；发布编译模块/构建缓存单独按 Linux 架构、Debian 工具链镜像摘要、Go 版本和 go.sum 分键，不复用 Ubuntu 宿主编译缓存。npm 下载缓存由 setup-node 按 OS/锁文件分键。缓存失效或冷启动不改变门禁；不能将缓存当作可信镜像。

先等待准确 main commit 的完整 CI 成功，再创建新的 annotated `v<版本>` 标签。发布脚本按 SHA、main、push、ci.yml、completed/success 选择准确 run ID，要求两个唯一未过期产物。下载明确 run ID 和含 SHA/架构的名称，验证 SHA256、元数据、实际镜像平台与 labels。不会用最新的任意产物，也不会回退另一提交。

发布只 load/push 已测试镜像，构造 candidate manifest，通过不可变摘要运行双架构验收后推广。版本标签已存在且摘要不同会失败；候选验收失败不更新版本/0.8/latest。推广前再次要求 main 等于该 SHA，加上单一发布 concurrency 防止旧提交覆盖新 rolling 标签。注册表多标签写入不能原子提交；验收后的推广若遭遇网络失败，应核对全部四个标签并重跑同一任务，不重编译或更换产物。

产物过期/缺失须为仍处于 main 的同一提交重跑 CI，再重跑原发布任务。若 main 已前进，使用新的版本与提交；不能移动旧 tag。候选/ci-SHA 架构标签便于恢复，尚无自动删除策略；后续按仓库保留政策清理已被正式索引引用的候选，避免删除仍需引用的镜像。

本地 `make build` 及 `docker build` 仍从源码构建；均通过共享原生 CGO/static 编译脚本。`make binary` 仅编译，调用前须准备正确前端。正式 CI runtime 使用 `.github/runtime/Dockerfile`，不含 npm/Go 编译。

从 0.7.16 起，`scripts/build-native-container.sh` 和源码 Dockerfile 固定使用 `golang:1.27.1-trixie@sha256:8f58fd67ea075142d947a60e0caa4317746a55118d312f027793d382c7741734`；amd64、arm64 都在对应原生主机编译，不依赖 QEMU 编译。Go 基线分别为 GOAMD64=v1、GOARM64=v8.0。宿主 `make binary` 的 C/libc 依赖宿主环境，不作为发布制品。

0.7.15 Ubuntu 宿主的静态 libc 带来 x86-64-v3 强制需求，导致无 AVX guest 在 glibc CPU 初始化执行 vmovd 时 SIGILL，退出 132 且尚无应用日志；仅修改 Go 或 C 编译 flags 不能降低预编译静态库的 ISA。现在 `test-cpu-baseline.py` 从实际候选镜像提取二进制，要求静态 amd64 ELF 只声明 x86-64-baseline，再使用 Nehalem（无 AVX）验证版本、镜像默认 serve、隔离空数据目录、健康和正常停止。CI 上传前及不可变候选推广前都执行，原生双架构运行门禁仍保留。QEMU 是额外 CPU 回归检查，不代表所有 CPU 或用户部署已验收。

发布 runner 必须先安装 qemu-user/binutils，再注册 Docker ARM 的 binfmt 解释器：系统 QEMU 包的安装脚本会重置 binfmt 注册，顺序颠倒会令 ARM 容器在验收时 exec format error。依赖安装和低 ISA 检查均不得跳过候选门禁。

官方 Claude 联网预热脚本不纳入强制离线门禁。0.8.0 时本地出口对 downloads.claude.ai 的 CONNECT 返回 403；0.8.1 复查时出口可达，官方二进制完整下载并通过最终摘要校验和零读取缓存命中。该 224 MB 下载在低带宽下会超过原先固定的五分钟上限，脚本改为两分钟无进展才失败、总上限 30 分钟；离线证据由仅 `_test.go` 的真实 RSA 签名→清单→Catalog→授权→下载组件链提供，生产签名根与验证保持不变。原生 Windows PS7/5.1、amd64/arm64 及无 AVX 门禁不降低。
