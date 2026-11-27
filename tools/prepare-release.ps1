param(
    [ValidateSet('amd64', 'arm64')][string]$Arch = 'amd64',
    [switch]$SkipBuild
)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'release-files.ps1')
$releaseRepository = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
Push-Location $releaseRepository
try {
    $releaseVersion = (& node (Join-Path $PSScriptRoot 'release-version.mjs') --sync).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Could not resolve release version.' }
    if (-not $SkipBuild) {
        & wails3 build "GOARCH=$Arch"
        if ($LASTEXITCODE -ne 0) { throw 'Release build failed.' }
    }
    $releaseBinary = Join-Path $releaseRepository 'dist/Asterodia.exe'
    $verified = Get-VerifiedReleaseFile -Path $releaseBinary -Arch $Arch -Version $releaseVersion
    $releaseOutput = Join-Path $releaseRepository "dist/releases/v$releaseVersion"
    New-Item -ItemType Directory -Force -Path $releaseOutput | Out-Null
    $releaseAsset = Join-Path $releaseOutput "Asterodia-windows-$Arch.exe"
    Copy-Item -LiteralPath $releaseBinary -Destination $releaseAsset
    $releaseSums = @(Get-ChildItem -LiteralPath $releaseOutput -Filter 'Asterodia-windows-*.exe' -File | Sort-Object Name | ForEach-Object {
        $releaseHash = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        "$releaseHash  $($_.Name)"
    })
    [IO.File]::WriteAllText((Join-Path $releaseOutput 'SHA256SUMS'), ($releaseSums -join "`n") + "`n", [Text.UTF8Encoding]::new($false))
    Write-Output "Release v$releaseVersion prepared in $releaseOutput"
    Write-Output 'Attach the executable(s) and SHA256SUMS to the same draft GitHub Release, then publish it as a full release.'
} finally { Pop-Location }
