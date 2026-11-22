param([string]$ToolsDirectory = '')
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$ciRepository = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
if (-not $ToolsDirectory) { $ToolsDirectory = Join-Path $ciRepository '.test-data/ci-tools' }
$ToolsDirectory = [IO.Path]::GetFullPath($ToolsDirectory)
if (-not $ToolsDirectory.StartsWith($ciRepository + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'CI tools must be stored inside the repository workspace.'
}
$downloads = Join-Path $ToolsDirectory 'downloads'
New-Item -ItemType Directory -Force -Path $downloads | Out-Null
$manifest = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'ci-tools.json') -Raw | ConvertFrom-Json
foreach ($entry in $manifest.PSObject.Properties) {
    $spec = $entry.Value
    if ($spec.url -notmatch '^https://' -or $spec.sha256 -notmatch '^[0-9a-f]{64}$') { throw "Invalid tool definition: $($entry.Name)" }
    $asset = Join-Path $downloads ([IO.Path]::GetFileName(([Uri]$spec.url).AbsolutePath))
    if (-not (Test-Path -LiteralPath $asset -PathType Leaf)) {
        Write-Output "Downloading $($entry.Name)..."
        $partial = "$asset.partial"
        Invoke-WebRequest -Uri $spec.url -OutFile $partial -MaximumRetryCount 2 -RetryIntervalSec 3
        if ((Get-FileHash -LiteralPath $partial -Algorithm SHA256).Hash.ToLowerInvariant() -ne $spec.sha256) { throw "SHA-256 mismatch: $($entry.Name)" }
        Move-Item -LiteralPath $partial -Destination $asset
    }
    if ((Get-FileHash -LiteralPath $asset -Algorithm SHA256).Hash.ToLowerInvariant() -ne $spec.sha256) { throw "Cached tool SHA-256 mismatch: $($entry.Name)" }
    if ($spec.archive) {
        $destination = Join-Path $ToolsDirectory $entry.Name
        New-Item -ItemType Directory -Force -Path $destination | Out-Null
        # Windows ships bsdtar with support for both ZIP and 7z archives.
        & (Join-Path $env:SystemRoot 'System32/tar.exe') -xf $asset -C $destination
        if ($LASTEXITCODE -ne 0) { throw "Could not extract $($entry.Name)" }
    }
}
function Find-CITool([string]$Directory, [string]$Name) {
    $matches = @(Get-ChildItem -LiteralPath $Directory -Filter $Name -Recurse -File)
    if ($matches.Count -ne 1) { throw "Expected exactly one $Name in $Directory" }
    return $matches[0].FullName
}
$ffmpeg = Find-CITool (Join-Path $ToolsDirectory 'ffmpeg') 'ffmpeg.exe'
$probe = Find-CITool (Join-Path $ToolsDirectory 'ffmpeg') 'ffprobe.exe'
$image = Find-CITool (Join-Path $ToolsDirectory 'imagemagick') 'magick.exe'
$lint = Find-CITool (Join-Path $ToolsDirectory 'actionlint') 'actionlint.exe'
$variables = @{
    STORAGEBUCKET_TEST_FFMPEG_EXE = $ffmpeg
    STORAGEBUCKET_TEST_FFPROBE_EXE = $probe
    STORAGEBUCKET_TEST_IMAGE_EXE = $image
    STORAGEBUCKET_TEST_YTDLP_EXE = Join-Path $downloads 'yt-dlp.exe'
    STORAGEBUCKET_TEST_GALLERYDL_EXE = Join-Path $downloads 'gallery-dl.exe'
    STORAGEBUCKET_TEST_NODE_EXE = (Get-Command node -ErrorAction Stop).Source
}
foreach ($name in $variables.Keys) {
    [Environment]::SetEnvironmentVariable($name, $variables[$name], 'Process')
    if ($env:GITHUB_ENV) { Add-Content -LiteralPath $env:GITHUB_ENV -Value "$name=$($variables[$name])" -Encoding utf8 }
}
$directories = @((Split-Path $ffmpeg), (Split-Path $image), (Split-Path $lint))
$env:PATH = ($directories -join [IO.Path]::PathSeparator) + [IO.Path]::PathSeparator + $env:PATH
if ($env:GITHUB_PATH) { $directories | Add-Content -LiteralPath $env:GITHUB_PATH -Encoding utf8 }
& $ffmpeg -version | Select-Object -First 1
if ($LASTEXITCODE -ne 0) { throw 'FFmpeg failed to start.' }
& $image -version | Select-Object -First 1
if ($LASTEXITCODE -ne 0) { throw 'ImageMagick failed to start.' }
Write-Output 'Verified CI media tools are ready.'
