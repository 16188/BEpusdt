# Changelog

## 2026-09-10

- 修正 EVM 请求失败未完整计入成功率的问题；BSC 扫块成功率低于 95% 时，自动从 Chainlist 获取、安全过滤并切换到通过连续健康检查的公开 RPC。
- 新增 GitHub Actions，自动发布 `ghcr.io/16188/bepusdt:latest` 镜像。
