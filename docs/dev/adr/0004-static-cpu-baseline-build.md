# 0004：静态链接并固定 CPU 基线的原生构建

## 背景

0.7.15 在 Ubuntu 宿主上静态链接，宿主预编译的静态 libc 要求 x86-64-v3。在不支持 AVX 的虚拟机上，glibc 的 CPU 初始化执行 `vmovd` 触发 SIGILL（退出码 132），且没有任何应用日志。只修改 Go 或 C 的编译参数无法降低预编译静态库的指令集要求。0.7.16 起改为下面的构建方式。

## 决定

- 发布二进制在固定摘要的 `golang:1.27.1-trixie` 容器中、在对应的原生架构上编译（`scripts/build-native-container.sh`），不交叉编译，也不用 QEMU 编译。
- CGO 静态链接，`GOAMD64=v1`、`GOARM64=v8.0`。
- `scripts/test-cpu-baseline.py` 从实际候选镜像取出 amd64 二进制，要求 ELF 只声明 x86-64 baseline，并在无 AVX 的 QEMU CPU（Nehalem）上验证启动、健康检查和正常退出。CI 上传产物前和发布推广前都会执行。

## 后果

- 宿主 `make binary` 的产物不能作为发布制品。
- 升级工具链镜像时，要同时更新 Dockerfile、构建脚本和 CI 缓存键里的摘要。
- QEMU 测试只是额外的 CPU 回归检查，不代表覆盖所有 CPU。
