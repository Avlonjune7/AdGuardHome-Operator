# AdGuard Home 运营商定制版

基于 AdGuard Home 正式版 **v0.107.80**，使用简体中文管理界面。可以给同一域名分别设置中国移动、中国联通、中国电信和默认解析地址，也可以在查询日志里查看客户端 IP 所属的运营商。当前部署版本为 `v0.107.80+operator.5`。

## 在新 Linux 服务器上安装

**个人服务器一键安装（curl）。** 从部署者保管的私密命令文件复制整行，在新 Linux 服务器执行。命令使用专用下载密钥连接自有 HTTPS 下载服务，自动识别架构、校验安装包并启动服务，无需 GitHub CLI 或 GitHub 账号。命令持有人可以下载安装包，请勿公开转发。下载服务器的管理员可用 `sudo cat /root/agh-private-install.txt` 取回命令。

**通过私有 GitHub Release 安装（备选）。** 适用于使用 systemd 的 Linux amd64 / arm64。先安装 [GitHub CLI](https://cli.github.com/)，并用有私有仓库读取权限的账号执行一次 `gh auth login --hostname github.com`。然后复制下面这一行：

```bash
( f=$(mktemp) && trap 'rm -f "$f"' EXIT && gh release download 'v0.107.80+operator.5' -R Avlonjune7/AdGuardHome-Operator -p install-linux.sh -O - > "$f" && sh "$f" )
```

脚本自动识别架构、下载同一版本的安装包、校验 SHA-256，并安装到 `/opt/AdGuardHome`、启动系统服务。普通用户需有 sudo 权限。安装成功后访问 `http://服务器IP:3000` 完成设置。检测到已有服务或安装目录时，脚本会退出。已有实例请按文末升级步骤操作。

**手动下载安装**

适用于使用 systemd 的 Linux x86_64（amd64）和 ARM64（aarch64）。安装包包含中文管理界面，解压即可运行。已有实例请先下载并校验安装包，然后按下方「升级已有实例与回退」操作。

**1. 下载安装包。** 仓库为私有仓库。可以在浏览器登录 GitHub，从 [当前版本 Release](https://github.com/Avlonjune7/AdGuardHome-Operator/releases/tag/v0.107.80%2Boperator.5) 的 Assets 下载对应架构的 `.tar.gz` 和同名 `.sha256` 文件，再将两者传到服务器。也可以在装有 GitHub CLI 的服务器上运行：

```bash
set -e
case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "此版本没有适配当前架构的安装包"; exit 1 ;;
esac
pkg="AdGuardHome-linux-${arch}.tar.gz"
mkdir -p "$HOME/adguard-operator.5"
cd "$HOME/adguard-operator.5"
gh auth status --hostname github.com || gh auth login --hostname github.com
gh release download 'v0.107.80+operator.5' -R Avlonjune7/AdGuardHome-Operator \
  -p "$pkg" -p "$pkg.sha256"
```

**2. 校验并安装为系统服务。** 在两个下载文件所在目录执行。校验通过后才解压，运行时应显示 `v0.107.80+operator.5`。

```bash
set -e
case "$(uname -m)" in
  x86_64) pkg=AdGuardHome-linux-amd64.tar.gz ;;
  aarch64|arm64) pkg=AdGuardHome-linux-arm64.tar.gz ;;
  *) echo "暂不支持当前架构"; exit 1 ;;
esac
sha256sum -c "$pkg.sha256"
tar -xzf "$pkg"
cd AdGuardHome
./AdGuardHome --version
# 仅用于首次安装；升级请从当前目录执行文末的升级步骤。
sudo install -d /opt/AdGuardHome
sudo install -m 0755 ./AdGuardHome /opt/AdGuardHome/AdGuardHome
sudo /opt/AdGuardHome/AdGuardHome -s install
systemctl is-active AdGuardHome
```

**3. 完成首次设置。** 浏览器访问 `http://服务器IP:3000`，按向导设置管理账号、管理端口和 DNS 监听端口。首次配置时仅向自己的管理 IP 开放 TCP 3000。如果准备让面板管理证书，可选 8080 作为管理端口，并为证书验证预留 TCP 80。

## 配置加密 DNS 和证书续签

按服务器上已有的证书管理方式选择一项。

| 服务器环境 | 配置方式 |
| --- | --- |
| 新机，由 AdGuard Home 管理证书 | 安装 `sh`、`openssl` 和 `curl` 或 `wget`。在「设置 → 加密设置 → 自动申请与续签证书」填写客户端将要访问的域名或公网 IP 和联系邮箱，点击「申请并启用自动续签」。签发成功后点击「填入下方 TLS 设置」，勾选「启用加密」，设置 HTTPS 端口并保存。 |
| 已有 Nginx、acme.sh 等外部续签任务 | 保留现有续签任务，面板托管保持关闭。在「加密设置」填写外部任务实际更新的完整证书链和私钥路径。AdGuard Home 从这些路径加载更新后的证书。 |

面板使用 Let's Encrypt 的 HTTP-01 验证。申请和续签时公网 TCP 80 都要能到达本机，且该端口须空闲或由 AdGuard Home 的 HTTP 管理服务监听。使用域名时，指向该名称的 A/AAAA 记录应能到达本机。面板会每小时检查续签状态，服务需要持续运行。

如果 HTTPS 端口选 **8443**，在服务器防火墙和云安全组放行 TCP 8443，客户端的 DoH 地址填 `https://证书域名或IP:8443/dns-query`。通过 IP 连接时，要申请包含该公网 IP 的证书。普通 DNS、DoT 和 DoQ 可按实际使用的协议开放相应端口。

## 配置运营商 DNS 重写

打开「过滤器 → DNS 重写」，给业务域名添加一条默认记录，再按需为同一域名添加中国移动、中国联通和中国电信的目标 IP。只配置默认记录时，各运营商都会收到默认地址。

运营商识别使用 AdGuard Home 看到的客户端来源 IP。若查询经过公共递归 DNS 或代理转发，请在「查询日志 → 客户端」核对实际来源。IP 命中运营商库时会显示中文名称，未识别时只显示地址。运营商数据默认每 24 小时更新，下载失败会继续使用上次成功加载的数据。[运营商 IP 库](https://github.com/gaoyifan/china-operator-ip)

**验证解析。** 从实际客户端网络通过自己的 DoH 地址查询重写域名，核对返回 IP 和查询日志中的客户端运营商。配置了多条线路时，分别从对应运营商网络测试。装有支持 HTTPS 的 `kdig` 时，可用下面的命令（将示例域名换成实际证书名称和业务域名）。

```bash
kdig @dns.example.com -p 8443 +https=/dns-query +tls-ca app.example.com A
```

## 升级已有实例与回退

使用本仓库构建的程序升级。面板中的上游官方更新会安装官方构建，可能覆盖定制功能。以下示例假设服务名为 `AdGuardHome`，安装与工作目录均为 `/opt/AdGuardHome`，配置和数据位于该目录中。先核对本机服务路径，再从**新程序所在目录**执行。升级会短暂停止解析。

```bash
# 以下操作在同一个 Bash 会话内执行
set -e
systemctl show AdGuardHome -p ExecStart -p WorkingDirectory
app=/opt/AdGuardHome
backup="/opt/adguard-backups/$(date +%Y%m%d-%H%M%S)"
sudo install -d -m 0700 "$backup"
sudo install -m 0755 ./AdGuardHome "$app/AdGuardHome.new"
sudo "$app/AdGuardHome.new" --check-config -c "$app/AdGuardHome.yaml" -w "$app"
sudo systemctl stop AdGuardHome
if ! sudo cp -a "$app/AdGuardHome" "$app/AdGuardHome.yaml" "$app/data" "$backup/"; then
  sudo systemctl start AdGuardHome
  exit 1
fi
echo "本次备份目录 $backup"
sudo mv "$app/AdGuardHome.new" "$app/AdGuardHome"
sudo systemctl start AdGuardHome
systemctl is-active AdGuardHome
"$app/AdGuardHome" --version
```

升级后再查询一次业务域名。若需回退，在服务器上用本次备份目录替换下面的示例路径。以下命令恢复旧程序和配置，保留现有查询数据。外部证书和续签任务由原管理工具单独备份。

```bash
set -e
app=/opt/AdGuardHome
backup=/opt/adguard-backups/替换为本次备份目录名
sudo test -x "$backup/AdGuardHome"
sudo test -s "$backup/AdGuardHome.yaml"
sudo systemctl stop AdGuardHome
sudo cp -a "$backup/AdGuardHome" "$backup/AdGuardHome.yaml" "$app/"
sudo systemctl start AdGuardHome
systemctl is-active AdGuardHome
```

项目沿用上游 GPL-3.0 许可证。[许可证](LICENSE.txt) · [上游 AdGuard Home](https://github.com/AdguardTeam/AdGuardHome) · [上游使用文档](https://github.com/AdguardTeam/AdGuardHome/wiki)
