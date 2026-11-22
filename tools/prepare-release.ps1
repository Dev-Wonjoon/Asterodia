param(
    [ValidateSet('amd64', 'arm64')][string]$Arch = 'amd64',
    [switch]$SkipBuild
)
$ErrorActionPreference = 'Stop'
$releaseRepository = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
Push-Location $releaseRepository
try {
    $releaseVersion = (& node (Join-Path $PSScriptRoot 'release-version.mjs')).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Could not resolve release version.' }
    if (-not $SkipBuild) {
        & wails3 build "ARCH=$Arch"
        if ($LASTEXITCODE -ne 0) { throw 'Release build failed.' }
    }
    $releaseBinary = Join-Path $releaseRepository 'dist/StorageBucket.exe'
    if (-not (Test-Path -LiteralPath $releaseBinary -PathType Leaf)) { throw 'dist/StorageBucket.exe is missing.' }
    $releaseBytes = [IO.File]::ReadAllBytes($releaseBinary)
    if ($releaseBytes.Length -lt 64 -or $releaseBytes[0] -ne 0x4d -or $releaseBytes[1] -ne 0x5a) { throw 'Not a Windows executable.' }
    $releasePE = [BitConverter]::ToInt32($releaseBytes, 0x3c)
    if ($releasePE -lt 0 -or $releasePE + 6 -gt $releaseBytes.Length -or [BitConverter]::ToUInt32($releaseBytes, $releasePE) -ne 0x4550) { throw 'Invalid PE header.' }
    $releaseMachine = [BitConverter]::ToUInt16($releaseBytes, $releasePE + 4)
    $releaseExpectedMachine = if ($Arch -eq 'amd64') { 0x8664 } else { 0xaa64 }
    if ($releaseMachine -ne $releaseExpectedMachine) { throw "Executable architecture does not match $Arch." }
    $releaseInfo = [Diagnostics.FileVersionInfo]::GetVersionInfo($releaseBinary)
    $releaseProductVersion = '{0}.{1}.{2}' -f $releaseInfo.ProductMajorPart, $releaseInfo.ProductMinorPart, $releaseInfo.ProductBuildPart
    if ($releaseProductVersion -ne $releaseVersion -or $releaseInfo.ProductPrivatePart -ne 0) { throw "Executable version $releaseProductVersion does not match $releaseVersion." }
    $releaseOutput = Join-Path $releaseRepository "dist/releases/v$releaseVersion"
    New-Item -ItemType Directory -Force -Path $releaseOutput | Out-Null
    $releaseAsset = Join-Path $releaseOutput "StorageBucket-windows-$Arch.exe"
    Copy-Item -LiteralPath $releaseBinary -Destination $releaseAsset
    $releaseSums = @(Get-ChildItem -LiteralPath $releaseOutput -Filter 'StorageBucket-windows-*.exe' -File | Sort-Object Name | ForEach-Object {
        $releaseHash = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        "$releaseHash  $($_.Name)"
    })
    [IO.File]::WriteAllText((Join-Path $releaseOutput 'SHA256SUMS'), ($releaseSums -join "`n") + "`n", [Text.UTF8Encoding]::new($false))
    Write-Output "Release v$releaseVersion prepared in $releaseOutput"
    Write-Output 'Attach the executable(s) and SHA256SUMS to the same draft GitHub Release, then publish it as a full release.'
} finally { Pop-Location }
