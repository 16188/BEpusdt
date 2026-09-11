# Changelog

## 2026-09-11

- 修复自动切换 RPC 后内存配置未立即生效、需要重启进程的问题。

## 2026-09-10

- 修正 EVM 请求失败未完整计入成功率的问题；BSC 扫块成功率低于 95% 时，自动从 Chainlist 获取、安全过滤并切换到通过连续健康检查的公开 RPC。
- 新增 GitHub Actions，自动发布 `ghcr.io/16188/bepusdt:latest` 镜像。
