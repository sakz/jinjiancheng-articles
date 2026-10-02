# Go 文章监控实施方案

目标：在 Debian 12 上定时发现天玑全集三个公众号的新文章，保存到现有仓库并推送，以触发 Vercel 更新。

架构：WeRSS Docker 服务负责扫码授权与文章发现，提供带全文的 RSS；独立 Go 程序读取订阅源，将 HTML 转为现有 Markdown 格式，按原文链接去重。systemd timer 每 30 分钟运行一次。运行配置和待提交记录位于仓库外，不写入公开 Git。

文件职责：`monitor/config.go` 配置校验；`feed.go` HTTP 与全文转换；`archive.go` 现有归档去重与新文件保存；`git.go` 同步、限定路径提交与推送；`main.go` 执行与进程锁；`deploy/monitor/` 数据源和 systemd 配置；`monitor/README.md` Debian 部署与扫码操作。

- [x] 实现配置、全文 RSS 读取、正文转换、链接规范化与文件名处理。
- [x] 实现仓库外待提交记录、重复运行去重和中断后恢复；只新增文章，不覆盖原文与人工留言。
- [x] 实现 Git fast-forward 同步、限定路径提交和失败后重试，保留远端冲突供人工处理。
- [x] 配套 WeRSS Docker 与 systemd timer，说明 SSH 隧道扫码、订阅 URL 填写、Deploy Key 与日志查看。
- [x] 本地编译 Go 和 Linux 静态二进制；使用本地 HTTP RSS 与临时裸 Git 仓库核对采集、去重、推送及恢复。真实微信登录和 VPS 安装需用户提供服务器及扫码操作后验收。

核对结果：本地 RSS/Git 全流程通过，包括 101 条分页、重复运行、正文补抓、推送失败重试、清单恢复、进程锁、状态路径限制与非监控提交拒绝。Go 编译与 vet 通过；两个 Linux 静态二进制已生成，Compose 配置解析通过。WeRSS 官方多架构镜像已锁定 digest。真实 VPS/扫码验收尚未执行。
