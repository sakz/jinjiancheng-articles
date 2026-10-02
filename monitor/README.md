# 天玑全集 Go 定时归档程序

## 工作方式

```text
微信扫码授权 → WeRSS 发现文章、采集全文 → 本机 RSS
                                           ↓ 每 30 分钟
                                   Go 转换 Markdown、去重
                                           ↓
                      jinjiancheng-articles main → GitHub → Vercel
```

默认监控金渐成、天机奇谈、生玑伯伯。WeRSS 是独立数据源，Go 程序负责归档和 Git 同步，不持有微信 Cookie，不直接调用微信登录接口。

- `systemd timer` 每 30 分钟启动一次，启动后 2 分钟也检查；单次最长 10 分钟。通过文件锁禁止重叠运行。
- 只保存全文 RSS 的正文与图片链接，**不采集微信留言**。缺少全文不会将摘要当作正文，会报告错误并在后续任务重试。
- 文件名沿用 `md/天玑全集/<公众号>/[YYYY-MM-DD]标题.md`，原文链接与图片保留；不覆盖已有文章或人工补录留言。
- 长链接按 `__biz + mid + idx` 去重，忽略追踪参数；短链接按路径去重。旧归档的短链接与新长链接无法直接对应时，用同公众号、同日期、同标题避免重复；该回退可能合并同日同标题的两篇不同文章。
- 首次读取会导入 `start_date` 当日及之后、仓库中没有的条目。示例起点是当前归档最新日期 `2026-09-29`，可调整。WeRSS 订阅地址支持分页，程序每页读取 100 条直到起始日期；最多 100 页。此前的数据必须已经进入 WeRSS 数据库，不能从 RSS 恢复数据源未收录的文章。
- 每轮先 fetch，然后只做 fast-forward 同步；只提交本程序待提交清单中的新增文章。非监控改动、非监控本地提交或分叉会停止，不能强制推送。
- 推送失败保留本地提交，下轮先重试。写入中断时恢复仓库外清单，不提交半个文件。运行状态与登录配置不进入 Git。
- 新文章推送后由现有 Vercel Git 集成重新构建；四个理念总结栏目仍需阅读后更新。

## 1. 本地构建 Linux 二进制

需要 Go **1.26 或更高版本**（用于构建），VPS 运行二进制不需要安装 Go。Debian 12 自带的旧 Go 工具链不适合直接构建本项目。

在本仓库目录执行：

```bash
cd monitor
make build
```

生成 `bin/tianji-monitor-linux-amd64` 和 `bin/tianji-monitor-linux-arm64`，均为不依赖 libc 的静态程序；校验值在 `bin/SHA256SUMS`。这些生成文件不提交到 Git。

VPS 上 `uname -m` 返回 `x86_64` 时选 amd64；返回 `aarch64` 时选 arm64。下面以 amd64 为例，在本地仓库根目录上传（将 VPS 替换为服务器地址）：

```bash
scp monitor/bin/tianji-monitor-linux-amd64 root@VPS:/tmp/tianji-monitor
```

## 2. Debian 12 创建运行用户和 Git 凭证

以下命令在 VPS 上以 root 运行：

```bash
apt-get update
apt-get install -y ca-certificates git openssh-client openssl docker.io docker-compose nano
systemctl enable --now docker
useradd --system --create-home --home-dir /srv/tianji --shell /bin/bash tianji
install -d -o tianji -g tianji -m 0700 /srv/tianji/.ssh
runuser -u tianji -- ssh-keygen -t ed25519 -N '' -f /srv/tianji/.ssh/id_ed25519 -C tianji-article-monitor
cat /srv/tianji/.ssh/id_ed25519.pub
```

将显示的**公钥**加入 GitHub 仓库 `sakz/jinjiancheng-articles` → Settings → Deploy keys，并启用 **Allow write access**。然后以运行用户完成首次 SSH 连接和克隆：

```bash
runuser -u tianji -- ssh -T git@github.com
runuser -u tianji -- git clone git@github.com:sakz/jinjiancheng-articles.git /srv/tianji/articles
install -d -m 0755 /opt/tianji-monitor /etc/tianji-monitor
install -m 0755 /tmp/tianji-monitor /opt/tianji-monitor/tianji-monitor
install -d -o tianji -g tianji -m 0700 /var/lib/tianji-monitor
```

GitHub 的 `ssh -T` 成功提示不提供 shell，并可能返回退出码 1，这是正常行为。使用专门的服务器克隆，不在该目录手动开发或提交其他内容。

## 3. 启动微信扫码数据源 WeRSS

数据源使用 [WeRSS 官方容器](https://github.com/rachelos/we-mp-rss/blob/main/README.zh-CN.md)，SQLite 数据持久化，无需额外数据库。配套镜像固定了本次查询到的 digest，避免同一个部署配置随 `latest` 变化；需要升级时重新核对上游版本和配置再更新 digest。镜像支持 Linux amd64 和 arm64。

```bash
install -d -m 0700 /opt/tianji-werss
cp /srv/tianji/articles/deploy/monitor/compose.yaml /opt/tianji-werss/compose.yaml
cd /opt/tianji-werss
install -m 0600 /dev/null .env
printf 'WERSS_PASSWORD=%s\nWERSS_SECRET_KEY=%s\n' "$(openssl rand -hex 24)" "$(openssl rand -hex 32)" > .env
docker-compose -f compose.yaml up -d
docker-compose -f compose.yaml logs --tail=80
```

后续登录管理页面使用用户名 `admin`，密码是 `/opt/tianji-werss/.env` 中的 `WERSS_PASSWORD`。首次初始化使用这些值；以后仅修改环境变量未必会重置数据库中的密码。

容器端口只绑定 `127.0.0.1:8001`。在**本地电脑**建立 SSH 隧道：

```bash
ssh -N -L 8001:127.0.0.1:8001 root@VPS
```

浏览器打开 `http://localhost:8001`，按上游界面完成：

1. 使用 `admin` 与生成的密码登录管理界面。
2. 微信扫码完成账号授权。扫码步骤必须由账号本人操作。
3. 添加「金渐成」「天机奇谈」「生玑伯伯」三个公众号。可使用仓库中的微信原文链接确认账号，避免同名账号误选。
4. 配置并启用这些公众号的定时更新任务，建议每 30 分钟至 1 小时更新。`ENABLE_JOB=True` 只开启任务调度器，仍需在界面中配置采集任务。
5. 确认最新文章已经抓到**完整正文**，复制各自的 **RSS/XML** 订阅地址，形如 `http://127.0.0.1:8001/feed/<界面生成的ID>.rss`。不要使用公众号列表 RSS，也不要使用未遵循 JSON Feed 标准的自定义 JSON 输出。

Compose 已开启全文和正文自动补抓、使用微信原文链接而非 WeRSS 本地页面；这些开关依据上游 [配置文件](https://github.com/rachelos/we-mp-rss/blob/main/config.example.yaml) 和 [RSS 实现](https://github.com/rachelos/we-mp-rss/blob/main/apis/rss.py)。订阅 ID 由扫码后的服务产生，不能预先伪造。

## 4. 填写 Go 程序配置

```bash
install -o tianji -g tianji -m 0600 /srv/tianji/articles/monitor/config.example.json /etc/tianji-monitor/config.json
nano /etc/tianji-monitor/config.json
```

将三个空的 `feed_url` 替换成上一步的 RSS 地址，统一使用 **VPS 本机地址** `http://127.0.0.1:8001`。示例保留原文目录名作为 `account`，即使公众号后续改名也不要随便改变归档目录。

字段：

| 字段 | 含义 |
| --- | --- |
| `repo_path` | 专用 Git 克隆目录的绝对路径 |
| `state_dir` | 仓库外运行状态目录的绝对路径 |
| `branch` / `remote` | 默认 `main` / `origin` |
| `git_push` | `true` 自动提交并推送；`false` 仍提交，但暂不推送 |
| `start_date` | 包含起始当天；空字符串读取数据源中所有可见历史文章 |
| `sources` | 每个公众号对应一个全文 RSS 地址 |

先预览，预览不会写文章、提交、推送或同步远端：

```bash
runuser -u tianji -- /opt/tianji-monitor/tianji-monitor -config /etc/tianji-monitor/config.json -dry-run
```

确认正文已经就绪、来源正确后运行一次：

```bash
runuser -u tianji -- /opt/tianji-monitor/tianji-monitor -config /etc/tianji-monitor/config.json
```

日志中的错误不包含订阅 URL 的查询凭据；网络错误可在 VPS 本机进一步检查数据源和 Git SSH 连接。

## 5. 启用 systemd 定时任务

```bash
cp /srv/tianji/articles/deploy/monitor/tianji-monitor.service /etc/systemd/system/
cp /srv/tianji/articles/deploy/monitor/tianji-monitor.timer /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now tianji-monitor.timer
systemctl list-timers tianji-monitor.timer
journalctl -u tianji-monitor.service -n 100 --no-pager
```

手动再检查一次：

```bash
systemctl start tianji-monitor.service
journalctl -u tianji-monitor.service -f
```

改变频率时修改 timer 的 `OnUnitInactiveSec`，然后执行 `systemctl daemon-reload` 和 `systemctl restart tianji-monitor.timer`。停止任务：`systemctl disable --now tianji-monitor.timer`。

## 维护与故障恢复

- **授权过期**：重新通过 SSH 隧道进入 WeRSS 扫码授权。微信授权不是永久的；Go 任务不能代替真人扫码。WeRSS 如果继续输出旧缓存，Go 可能只显示“新增 0 篇”，因此也应查看 WeRSS 授权与采集任务状态。
- **RSS 正文为空**：确认 WeRSS 正文采集开启，等待正文补抓后再运行。程序未写入“已完成”记录，下轮可重试。
- **推送失败**：修复 Deploy Key 或网络后再运行，已提交的批次会先重推。可以使用 `runuser -u tianji -- git -C /srv/tianji/articles status` 与 `log -3 --oneline` 查看状态。
- **分支分叉**：先停止 timer，在专用克隆中人工处理 rebase/冲突，确认归档内容保留后恢复 timer；程序不会做 reset、覆盖已有文件或强推。
- **不要删除 pending.json 来消除报错**：它是中断恢复的待提交记录。未推送的文章与本地提交需要先核对和保留。
- **服务器长时间停机**：确认 WeRSS 能补齐停机期间的文章。Go 支持读取 WeRSS 分页，但无法发现上游未采集或已经删除的文章。
- **图片**：正文图片保留原始远程地址，不下载离线副本，与现有网站行为一致。

## 本次交付核对范围

已在本地以 HTTP 全文 RSS 和临时裸 Git 远端核对：完整正文/图片转换、去重、预览不写入、缺少正文后重试、推送失败恢复、无关改动拒绝、半文件恢复、101 条分页、进程锁及无关本地提交拒绝。已交叉编译 Linux amd64/arm64 静态程序。真实微信扫码、容器运行和 Debian VPS 定时执行需在目标服务器安装后核对，不能用本地模拟结果代替。
