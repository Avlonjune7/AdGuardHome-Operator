#!/bin/sh
# Fresh Linux/systemd installation from the operator release.
set -eu

fail() {
    printf '%s\n' "$*" >&2
    exit 1
}

as_root() {
    if [ "$(id -u)" = 0 ]; then
        "$@"
    else
        sudo "$@"
    fi
}

main() {
    repo=Avlonjune7/AdGuardHome-Operator
    tag=v0.107.80%2Boperator.7
    dest=/opt/AdGuardHome
    [ "$(uname -s)" = Linux ] && [ -d /run/systemd/system ] ||
        fail '需要使用 systemd 的 Linux 服务器。'
    case "$(uname -m)" in
        x86_64) arch=amd64; digest=ee0c00c75d8262efcabf20aa90fea1f97e75786061ee1836bb3fc6e9501ea703 ;;
        arm64|aarch64) arch=arm64; digest=d2cc962d5fe2580eccbeea0c73a9a759b4494795e6d96a5b5d440f75b78773d8 ;;
        *) fail '支持的架构为 amd64 和 arm64。' ;;
    esac
    for tool in curl sha256sum tar systemctl mktemp install; do
        command -v "$tool" >/dev/null || fail "缺少命令：$tool"
    done
    if [ "$(id -u)" != 0 ]; then
        command -v sudo >/dev/null || fail '安装系统服务需要 sudo 或 root。'
    fi
    load_state=$(systemctl show AdGuardHome.service -p LoadState --value) || [ "$load_state" = not-found ] || fail '无法读取 systemd 状态。'
    if [ "$load_state" != not-found ] || [ -e "$dest" ] || [ -L "$dest" ]; then
        fail '检测到已有服务或安装目录。请按 README 的升级步骤操作。'
    fi

    tmp=$(mktemp -d)
    trap 'rm -rf -- "$tmp"' EXIT
    trap 'exit 1' HUP INT TERM
    pkg="AdGuardHome-linux-$arch.tar.gz"
    base="https://github.com/$repo/releases/download/$tag"
    curl -fsSL --retry 2 --proto '=https' --proto-redir '=https' "$base/$pkg" -o "$tmp/$pkg"
    (
        cd "$tmp"
        printf '%s  %s\n' "$digest" "$pkg" | sha256sum -c -
        tar -xzf "$pkg" AdGuardHome/AdGuardHome
        ./AdGuardHome/AdGuardHome --version
    )
    as_root mkdir -- "$dest"
    as_root install -m 0755 "$tmp/AdGuardHome/AdGuardHome" "$dest/AdGuardHome"
    as_root "$dest/AdGuardHome" -s install --no-check-update
    systemctl is-active --quiet AdGuardHome.service || fail '请用 journalctl -u AdGuardHome 查看启动日志。'
    printf '%s\n' '安装成功。请访问 http://服务器IP:3000 完成首次设置。'
}

main
