# Builds release zips of baton for Windows x64 and arm64 into dist\.
#   powershell -NoProfile -ExecutionPolicy Bypass -File scripts\build.ps1 [-Version 0.6.0]
# Everything in dist\ is replaced; keep nothing else there. Write the release
# notes first, in docs\releases\v<version>.md.
param([string]$Version = "0.6.0-dev")

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$saved = @{ GOOS = $env:GOOS; GOARCH = $env:GOARCH; CGO_ENABLED = $env:CGO_ENABLED }
Push-Location $root
try {
    # dist\ holds only what this script builds: the zips and checksums.txt
    # to upload as the release, and RELEASE_NOTES.md, the text for its page
    # (development builds go to bin\).
    $dist = Join-Path $root "dist"
    if (Test-Path $dist) { Remove-Item -Recurse -Force (Join-Path $dist "*") }
    New-Item -ItemType Directory -Force $dist | Out-Null
    $sums = Join-Path $dist "checksums.txt"

    # Icon, manifest and version information for both programs.
    go run ./tools/winres -version $Version
    if ($LASTEXITCODE -ne 0) { throw "winres failed" }

    $env:CGO_ENABLED = "0"
    $env:GOOS = "windows"
    foreach ($arch in @("amd64", "arm64")) {
        $env:GOARCH = $arch
        $out = Join-Path $dist "baton_${Version}_windows_$arch"
        New-Item -ItemType Directory -Force $out | Out-Null
        # baton.exe: the command line. baton-app.exe: the same program as a
        # windowed app (no console), which opens the baton window.
        go build -trimpath -ldflags "-s -w -X main.version=$Version" -o (Join-Path $out "baton.exe") ./cmd/baton
        if ($LASTEXITCODE -ne 0) { throw "go build failed for $arch" }
        go build -trimpath -ldflags "-s -w -H windowsgui -X main.guiBuild=1 -X main.version=$Version" -o (Join-Path $out "baton-app.exe") ./cmd/baton
        if ($LASTEXITCODE -ne 0) { throw "go build (app) failed for $arch" }
        Copy-Item README.md, README.ko.md, THIRD_PARTY_NOTICES.txt $out
        # The READMEs' pictures, at the paths they use.
        New-Item -ItemType Directory -Force (Join-Path $out "docs") | Out-Null
        Copy-Item -Recurse docs\images (Join-Path $out "docs\images")
        if (Test-Path LICENSE) { Copy-Item LICENSE $out }
        $zip = "$out.zip"
        Compress-Archive -Path (Join-Path $out "*") -DestinationPath $zip
        Remove-Item -Recurse -Force $out # only the zip is released
        $hash = (Get-FileHash $zip -Algorithm SHA256).Hash.ToLower()
        "$hash  $(Split-Path -Leaf $zip)" | Out-File -Append -Encoding ascii $sums
        Write-Host "built $zip"
    }

    # The text for the release page: docs\releases\v<version>.md with the
    # checksums appended. Every release has one (a -dev build need not).
    $notes = Join-Path $root "docs\releases\v$Version.md"
    if (Test-Path $notes) {
        $text = (Get-Content -Raw -Encoding UTF8 $notes).TrimEnd() + "`n`n## SHA-256`n`n" + '```' + "`n" +
            ((Get-Content $sums) -join "`n") + "`n" + '```' + "`n"
        [System.IO.File]::WriteAllText((Join-Path $dist "RELEASE_NOTES.md"), $text.Replace("`r`n", "`n"), (New-Object System.Text.UTF8Encoding $false))
        Write-Host "wrote $(Join-Path $dist 'RELEASE_NOTES.md')"
    }
    elseif ($Version -notmatch '-') {
        Write-Warning "no release notes for v$Version; write docs\releases\v$Version.md and build again"
    }
}
finally {
    # Leave the caller's session as it was.
    foreach ($k in $saved.Keys) {
        if ($null -eq $saved[$k]) { Remove-Item "Env:$k" -ErrorAction SilentlyContinue }
        else { Set-Item "Env:$k" $saved[$k] }
    }
    Pop-Location
}
