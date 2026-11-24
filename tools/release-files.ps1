function Get-VerifiedReleaseFile {
    param([string]$Path, [ValidateSet('amd64', 'arm64')][string]$Arch, [string]$Version)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { throw "Release executable is missing: $Path" }
    $stream = [IO.File]::Open($Path, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
    try {
        if ($stream.Length -lt 64 -or $stream.Length -gt 512MB) { throw "Invalid executable size: $Path" }
        $header = [byte[]]::new(64)
        if ($stream.Read($header, 0, 64) -ne 64 -or $header[0] -ne 0x4d -or $header[1] -ne 0x5a) { throw "Not a Windows executable: $Path" }
        $peOffset = [BitConverter]::ToInt32($header, 0x3c)
        if ($peOffset -lt 64 -or $peOffset + 6 -gt $stream.Length) { throw "Invalid PE offset: $Path" }
        $stream.Position = $peOffset
        $pe = [byte[]]::new(6)
        if ($stream.Read($pe, 0, 6) -ne 6 -or [BitConverter]::ToUInt32($pe, 0) -ne 0x4550) { throw "Invalid PE header: $Path" }
        $machine = if ($Arch -eq 'amd64') { 0x8664 } else { 0xaa64 }
        if ([BitConverter]::ToUInt16($pe, 4) -ne $machine) { throw "Executable architecture does not match $Arch." }
        $info = [Diagnostics.FileVersionInfo]::GetVersionInfo($Path)
        $numericVersion = (& node (Join-Path $PSScriptRoot 'release-version.mjs') --windows-version $Version).Trim()
        if ($LASTEXITCODE -ne 0) { throw 'Invalid release version.' }
        $productVersion = '{0}.{1}.{2}.{3}' -f $info.ProductMajorPart, $info.ProductMinorPart, $info.ProductBuildPart, $info.ProductPrivatePart
        $fileVersion = '{0}.{1}.{2}.{3}' -f $info.FileMajorPart, $info.FileMinorPart, $info.FileBuildPart, $info.FilePrivatePart
        if ($productVersion -cne $numericVersion -or $fileVersion -cne $numericVersion -or $info.ProductVersion -cne $Version -or $info.FileVersion -cne $Version) {
            throw "Executable version $productVersion / $fileVersion does not match VERSION $Version."
        }
        $stream.Position = 0
        $digest = (Get-FileHash -InputStream $stream -Algorithm SHA256).Hash.ToLowerInvariant()
        return [pscustomobject]@{ Path = [IO.Path]::GetFullPath($Path); Hash = $digest; Length = $stream.Length }
    } finally { $stream.Dispose() }
}
