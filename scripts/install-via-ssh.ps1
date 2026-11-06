param([Parameter(Mandatory,Position=0)][ValidatePattern('^[a-zA-Z0-9_][a-zA-Z0-9_.@-]*$')][string]$Server)
$ErrorActionPreference = 'Stop'
$repo = 'Avlonjune7/AdGuardHome-Operator'
$tag = 'v0.107.80+operator.5'
$temp = New-Item -ItemType Directory -Path (Join-Path ([IO.Path]::GetTempPath()) ([guid]::NewGuid()))
Push-Location $temp.FullName
try {
    foreach ($arch in 'amd64','arm64') {
        $pkg = "AdGuardHome-linux-$arch.tar.gz"
        gh release download $tag -R $repo -p $pkg -p "$pkg.sha256"
        if ($LASTEXITCODE) { throw 'GitHub download failed' }
    }
    Copy-Item (Join-Path $PSScriptRoot 'install-linux.sh') .
    tar -cf payload.tar install-linux.sh AdGuardHome-linux-amd64.tar.gz AdGuardHome-linux-amd64.tar.gz.sha256 AdGuardHome-linux-arm64.tar.gz AdGuardHome-linux-arm64.tar.gz.sha256
    if ($LASTEXITCODE) { throw 'Packaging failed' }
    # Stream the archive through one SSH connection. cmd redirects bytes unchanged.
    $remote = 'set -eu; d=$(mktemp -d); trap ''rm -rf -- $d'' EXIT; cd $d; tar -xf -; sh ./install-linux.sh .'
    cmd.exe /d /c "ssh -T $Server `"$remote`" < payload.tar"
    if ($LASTEXITCODE) { throw 'Remote installation failed' }
} finally {
    Pop-Location
    foreach ($name in 'payload.tar','install-linux.sh','AdGuardHome-linux-amd64.tar.gz','AdGuardHome-linux-amd64.tar.gz.sha256','AdGuardHome-linux-arm64.tar.gz','AdGuardHome-linux-arm64.tar.gz.sha256') {
        $file = Join-Path $temp.FullName $name
        if (Test-Path -LiteralPath $file) { Remove-Item -LiteralPath $file }
    }
    $temp.Delete()
}
