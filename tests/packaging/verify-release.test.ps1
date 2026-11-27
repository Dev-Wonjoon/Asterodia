$ErrorActionPreference = 'Stop'
$testRepository = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
. (Join-Path $testRepository 'tools/release-files.ps1')
$caseRoot = Join-Path $testRepository ('.test-data/release-tests/' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $caseRoot | Out-Null
$version = (& node (Join-Path $testRepository 'tools/release-version.mjs')).Trim()
if ($LASTEXITCODE -ne 0) { throw 'Could not read VERSION.' }
Set-Content -LiteralPath (Join-Path $caseRoot 'main.go') -Value 'package main; func main() {}' -Encoding utf8
$verify = Join-Path $testRepository 'tools/verify-release.ps1'
function Expect-Failure([scriptblock]$Action, [string]$Message) {
    $failed = $false
    try { & $Action | Out-Null } catch { $failed = $true }
    if (-not $failed) { throw "Expected rejection: $Message" }
}
$oldOS, $oldArch, $oldCGO = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
Push-Location $caseRoot
try {
    foreach ($arch in @('amd64', 'arm64')) {
        $resource = Join-Path $caseRoot "resource_windows_$arch.syso"
        & wails3 generate syso -arch $arch -icon (Join-Path $testRepository 'build/windows/icon.ico') -manifest (Join-Path $testRepository 'build/windows/wails.exe.manifest') -info (Join-Path $testRepository 'build/windows/info.json') -out $resource
        if ($LASTEXITCODE -ne 0) { throw "Resource generation failed: $arch" }
        $env:GOOS = 'windows'; $env:GOARCH = $arch; $env:CGO_ENABLED = '0'
        $directory = Join-Path $caseRoot "input/$arch"
        New-Item -ItemType Directory -Force -Path $directory | Out-Null
        $name = "Asterodia-windows-$arch.exe"
        $executable = Join-Path $directory $name
        & go build -buildvcs=false -o $executable .
        if ($LASTEXITCODE -ne 0) { throw "Fixture build failed: $arch" }
        $payload = Get-VerifiedReleaseFile -Path $executable -Arch $arch -Version $version
        [IO.File]::WriteAllText((Join-Path $directory 'SHA256SUMS'), "$($payload.Hash)  $name`n", [Text.UTF8Encoding]::new($false))
    }
    $amd = Join-Path $caseRoot 'input/amd64/Asterodia-windows-amd64.exe'
    Expect-Failure { Get-VerifiedReleaseFile -Path $amd -Arch arm64 -Version $version } 'architecture mismatch'
    Expect-Failure { Get-VerifiedReleaseFile -Path $amd -Arch amd64 -Version '0.0.0' } 'version mismatch'
    $invalid = Join-Path $caseRoot 'invalid.exe'; Set-Content -LiteralPath $invalid -Value 'not an executable'
    Expect-Failure { Get-VerifiedReleaseFile -Path $invalid -Arch amd64 -Version $version } 'invalid executable'
    $inputs = Join-Path $caseRoot 'input'
    $rejectedOutput = Join-Path $caseRoot 'rejected'
    Expect-Failure { & $verify -InputDirectory $inputs -OutputDirectory $rejectedOutput -Tag 'v0.0.0' } 'tag mismatch'
    if (Test-Path -LiteralPath $rejectedOutput) { throw 'Tag rejection wrote release output.' }
    $sumsPath = Join-Path $caseRoot 'input/amd64/SHA256SUMS'
    $originalSums = [IO.File]::ReadAllText($sumsPath)
    [IO.File]::WriteAllText($sumsPath, ('0' * 64) + '  Asterodia-windows-amd64.exe')
    Expect-Failure { & $verify -InputDirectory $inputs -OutputDirectory $rejectedOutput -Tag "v$version" } 'tampered checksum'
    if (Test-Path -LiteralPath $rejectedOutput) { throw 'Checksum rejection wrote release output.' }
    [IO.File]::WriteAllText($sumsPath, $originalSums)
    $extra = Join-Path $caseRoot 'input/amd64/unexpected.txt'; Set-Content -LiteralPath $extra -Value 'unexpected'
    Expect-Failure { & $verify -InputDirectory $inputs -OutputDirectory $rejectedOutput -Tag "v$version" } 'unexpected payload'
    # Delete only this exact fixture file; no recursive cleanup or source files.
    Remove-Item -LiteralPath $extra
    $output = Join-Path $caseRoot 'ready'
    & $verify -InputDirectory $inputs -OutputDirectory $output -Tag "v$version"
    $lines = @(Get-Content -LiteralPath (Join-Path $output 'SHA256SUMS'))
    if ($lines.Count -ne 4) { throw 'The combined release must contain four checksums including legacy aliases.' }
    foreach ($arch in @('amd64', 'arm64')) {
        $name = "Asterodia-windows-$arch.exe"
        $hash = (Get-FileHash -LiteralPath (Join-Path $output $name) -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($lines -cnotcontains "$hash  $name") { throw "Incorrect final checksum: $arch" }
        $legacy = "StorageBucket-windows-$arch.exe"
        $legacyHash = (Get-FileHash -LiteralPath (Join-Path $output $legacy) -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($legacyHash -cne $hash -or $lines -cnotcontains "$hash  $legacy") { throw "Legacy alias must contain identical verified bytes: $arch" }
    }
    foreach ($extendedVersion in @('2.2.6a', '2.2.6.1', '2.2.6.1a')) {
        $extendedRoot = Join-Path $caseRoot $extendedVersion
        New-Item -ItemType Directory -Force -Path $extendedRoot | Out-Null
        Copy-Item -LiteralPath (Join-Path $caseRoot 'main.go') -Destination (Join-Path $extendedRoot 'main.go')
        $numericVersion = (& node (Join-Path $testRepository 'tools/release-version.mjs') --windows-version $extendedVersion).Trim()
        if ($LASTEXITCODE -ne 0) { throw 'Extended version conversion failed.' }
        $extendedInfo = Get-Content -LiteralPath (Join-Path $testRepository 'build/windows/info.json') -Raw | ConvertFrom-Json
        $extendedInfo.fixed.file_version = $numericVersion
        $extendedInfo.fixed.product_version = $numericVersion
        foreach ($locale in $extendedInfo.info.PSObject.Properties.Value) {
            $locale.FileVersion = $extendedVersion
            $locale.ProductVersion = $extendedVersion
        }
        $infoPath = Join-Path $extendedRoot 'info.json'
        [IO.File]::WriteAllText($infoPath, ($extendedInfo | ConvertTo-Json -Depth 10), [Text.UTF8Encoding]::new($false))
        $manifest = Get-Content -LiteralPath (Join-Path $testRepository 'build/windows/wails.exe.manifest') -Raw
        $manifest = [regex]::Replace($manifest, '(<assemblyIdentity[^>]*version=")[^"]+', { param($match) $match.Groups[1].Value + $numericVersion })
        $manifestPath = Join-Path $extendedRoot 'app.manifest'
        [IO.File]::WriteAllText($manifestPath, $manifest, [Text.UTF8Encoding]::new($false))
        & wails3 generate syso -arch amd64 -icon (Join-Path $testRepository 'build/windows/icon.ico') -manifest $manifestPath -info $infoPath -out (Join-Path $extendedRoot 'resource_windows_amd64.syso')
        if ($LASTEXITCODE -ne 0) { throw 'Extended version resource generation failed.' }
        $env:GOARCH = 'amd64'
        $extendedExe = Join-Path $extendedRoot 'app.exe'
        Push-Location $extendedRoot
        try { & go build -buildvcs=false -o $extendedExe . } finally { Pop-Location }
        if ($LASTEXITCODE -ne 0) { throw 'Extended version fixture build failed.' }
        Get-VerifiedReleaseFile -Path $extendedExe -Arch amd64 -Version $extendedVersion | Out-Null
        Expect-Failure { Get-VerifiedReleaseFile -Path $extendedExe -Arch amd64 -Version $version } 'extended version mismatch'
    }
    Write-Output 'Release verification tests passed (both architectures, stable/letter/numeric versions, tag, tampering and unexpected files).'
} finally {
    Pop-Location
    $env:GOOS = $oldOS; $env:GOARCH = $oldArch; $env:CGO_ENABLED = $oldCGO
}
