# 正式版定制构建

基于官方 v0.107.80 标签，提交 003d6e1e15e04127f6d6d5e2f5c1aa843130752a。

- 使用经典 client 界面，Makefile 默认 CLIENT_DIR 已设为 client。
- 经典界面固定简体中文，移除其他语言资源与切换入口。保留原有翻译调用用于维护文案；不加载浏览器语言检测。
- DNS 重写支持默认、中国移动、中国联通、中国电信；按请求来源 IP 分类。
- 数据源 china-operator-ip，首次启用后下载，每日自动更新，失败保留上次有效库。
- 中间 DNS 转发或代理可能遮蔽真实客户端网络。第三方控制器必须保留 operator 字段。
- 未修改服务器、未部署；预览配置和示例 IP 仅用于本地验证。

构建：在 client 目录运行 npm ci 和 npm run build-prod，再在仓库根目录编译 Go。
版本应标记为 v0.107.80+operator.1，以区分定制构建与官方原版。

已有 client_v2 目录是上游未选用的前端源码，不参与本定制版编译。

证书托管（v0.107.80+operator.2，Linux）：

1. 在「设置 → 加密设置 → 自动申请与续签证书」填写单个域名或公网 IP、联系邮箱，再点「申请并启用自动续签」。
2. 使用内嵌的上游 acmesh-official/acme.sh 3.1.5（许可证位于 internal/home/acmebundle/LICENSE.md），固定 Let’s Encrypt，可选 HTTP-01 或 Cloudflare DNS-01 验证。系统需有 sh、openssl、curl 或 wget 及常用 POSIX 命令。HTTP 模式下，域名 A/AAAA 记录应指向本机；公网 TCP 80 可达，且端口空闲（进程有监听权限）或由 AdGuard HTTP 后台监听。无需 Nginx、宝塔或系统 cron。Cloudflare 模式使用同版本 dns_cf 插件和目标区域 Token，支持单个域名。
3. 签发成功后点击「填入下方 TLS 设置」，按需启用加密、配置端口并保存一次。之后 AdGuard 每小时检查，证书到续期窗口时调用 acme.sh；IP 使用 shortlived 配置。校验域名/IP、有效期及私钥后才切换证书对并调用 TLS 重载。
4. 设置、账户和私钥保存在工作目录 data/managed-acme，请限制访问，备份时妥善保护。最近一次 acme.sh 输出在 last-run.log。关闭托管不会删除或撤销现有证书。已有外部签发、DNS-01 或 Nginx 占用 80 的机器可保持原流程；本功能不接管已有 cron、代理、证书或防火墙。
