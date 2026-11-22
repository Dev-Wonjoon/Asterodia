param(
    [Parameter(Mandatory)][string]$InputDirectory,
    [Parameter(Mandatory)][string]$OutputDirectory,
    [Parameter(Mandatory)][string]$Tag,
    [ValidateSet('amd64', 'arm64')][string[]]$Architectures = @('amd64', 'arm64')
)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'release-files.ps1')
$version = (& node (Join-Path $PSScriptRoot 'release-version.mjs') --tag $Tag).Trim()
if ($LASTEXITCODE -ne 0) { throw 'Release tag and VERSION must match.' }
$architecturesToVerify = @($Architectures | Sort-Object -Unique)
if ($architecturesToVerify.Count -ne $Architectures.Count) { throw 'Duplicate release architectures.' }
$verified = @()
foreach ($arch in $architecturesToVerify) {
    $directory = Join-Path $InputDirectory $arch
    $name = "StorageBucket-windows-$arch.exe"
    $files = @(Get-ChildItem -LiteralPath $directory -Force)
    if ($files.Count -ne 2 -or @($files | Where-Object { $_.PSIsContainer -or ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -or $_.Name -cnotin @($name, 'SHA256SUMS') }).Count) {
        throw "Expected only $name and SHA256SUMS in $directory"
    }
    $file = Get-VerifiedReleaseFile -Path (Join-Path $directory $name) -Arch $arch -Version $version
    $sums = (Get-Content -LiteralPath (Join-Path $directory 'SHA256SUMS') -Raw).Trim()
    if ($sums -cne "$($file.Hash)  $name") { throw "Release checksum mismatch: $name" }
    $verified += [pscustomobject]@{ Name = $name; File = $file }
}
# Verify every payload before writing anything to the assembled release.
New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
$allowed = @($verified.Name) + @('SHA256SUMS')
if (@(Get-ChildItem -LiteralPath $OutputDirectory -Force | Where-Object { $_.PSIsContainer -or ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -or $_.Name -cnotin $allowed }).Count) {
    throw 'Release output contains unexpected files.'
}
foreach ($entry in $verified) { Copy-Item -LiteralPath $entry.File.Path -Destination (Join-Path $OutputDirectory $entry.Name) -Force }
$lines = @($verified | ForEach-Object { "$($_.File.Hash)  $($_.Name)" })
[IO.File]::WriteAllText((Join-Path $OutputDirectory 'SHA256SUMS'), ($lines -join "`n") + "`n", [Text.UTF8Encoding]::new($false))
Write-Output "Verified release $Tag for $($architecturesToVerify -join ', '): $OutputDirectory"
