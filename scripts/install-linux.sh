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
    tag=v0.107.80%2Boperator.6
    dest=/opt/AdGuardHome
    [ "$(uname -s)" = Linux ] && [ -d /run/systemd/system ] ||
        fail '需要使用 systemd 的 Linux 服务器。'
    case "$(uname -m)" in
        x86_64) arch=amd64; digest=b2b1e163f26cc03ccf31263e01ed9304ecf0e770cfb3a6740eef4f4058d6e5c5 ;;
        arm64|aarch64) arch=arm64; digest=03d3876b7b6c54316615d4db49b7dd8e57aa704ac2e6a56bb237a989a49db60f ;;
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
    as_root "$dest/AdGuardHome" -s install
    systemctl is-active --quiet AdGuardHome.service || fail '请用 journalctl -u AdGuardHome 查看启动日志。'
    printf '%s\n' '安装成功。请访问 http://服务器IP:3000 完成首次设置。'
}

main
