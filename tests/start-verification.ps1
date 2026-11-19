param([switch]$ExistingData, [string]$FixturePath, [string]$ExecutablePath, [switch]$FileMove)
$ErrorActionPreference = 'Stop'
$repositoryPath = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
function Assert-ContainedPath([string]$Path, [string]$AllowedRoot) {
    $full = [IO.Path]::GetFullPath($Path)
    $prefix = [IO.Path]::GetFullPath($AllowedRoot) + [IO.Path]::DirectorySeparatorChar
    if (-not $full.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) { throw '검증 경로가 허용된 저장소 경로 밖입니다.' }
    $item = Get-Item -LiteralPath $full -Force
    while ($item -and $item.FullName.StartsWith($repositoryPath, [StringComparison]::OrdinalIgnoreCase)) {
        if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw '검증 경로의 링크/정션은 허용하지 않습니다.' }
        $item = if ($item -is [IO.DirectoryInfo]) { $item.Parent } else { $item.Directory }
    }
    return $full
}
if ($FileMove) {
    if ($ExistingData -or $FixturePath -or $ExecutablePath) { throw 'FileMove는 다른 데이터/실행 경로 옵션과 함께 사용하지 않습니다.' }
    $runRoot = Assert-ContainedPath ((Get-Content -LiteralPath (Join-Path $repositoryPath '.test-data/filemove-latest.txt')).Trim()) (Join-Path $repositoryPath '.test-data')
    $ExecutablePath = Join-Path $runRoot 'StorageBucket-final.exe'
    $FixturePath = (Get-Content -LiteralPath (Join-Path $runRoot 'native-fixture-path.txt')).Trim()
}
if (-not $ExecutablePath) { $ExecutablePath = Join-Path $repositoryPath 'dist/StorageBucket.exe' }
$executablePath = Assert-ContainedPath $ExecutablePath $repositoryPath
if (-not (Test-Path -LiteralPath $executablePath)) { throw 'dist/StorageBucket.exe를 먼저 빌드하세요.' }
$dataPath = $repositoryPath
if ($FixturePath) {
    $dataPath = Assert-ContainedPath $FixturePath (Join-Path $repositoryPath '.test-data')
    if (-not (Test-Path -LiteralPath (Join-Path $dataPath 'storagebucket.db'))) { throw '검증 DB가 없습니다.' }
} elseif (-not $ExistingData) {
    $dataPath = Join-Path $repositoryPath ('.test-data/ui-verification/' + [Guid]::NewGuid().ToString('N'))
    & python (Join-Path $PSScriptRoot 'ui/seed_wails.py') $dataPath
    if ($LASTEXITCODE -ne 0) { throw '검증 데이터 생성 실패' }
    $enginePath = Join-Path $dataPath 'bin'
    New-Item -ItemType Directory -Force -Path $enginePath | Out-Null
    foreach ($name in @('yt-dlp.exe', 'gallery-dl.exe', 'ffmpeg.exe')) {
        $sourcePath = Join-Path $repositoryPath "bin/$name"
        if (-not (Test-Path -LiteralPath $sourcePath)) { $sourcePath = Join-Path $repositoryPath "resources/bin/$name" }
        if (-not (Test-Path -LiteralPath $sourcePath)) {
            $engineCommand = Get-Command $name -ErrorAction SilentlyContinue
            if ($engineCommand) { $sourcePath = $engineCommand.Source }
        }
        if (Test-Path -LiteralPath $sourcePath) { Copy-Item -LiteralPath $sourcePath -Destination (Join-Path $enginePath $name) }
    }
    Set-Content -LiteralPath (Join-Path $repositoryPath '.test-data/ui-verification/latest-path.txt') -Value $dataPath
}
$previousDataPath = $env:STORAGEBUCKET_DATA_DIR
$previousTemp = $env:TEMP
$previousTmp = $env:TMP
$temporaryPath = Join-Path $dataPath 'test-temp'
New-Item -ItemType Directory -Force -Path $temporaryPath | Out-Null
try {
    $env:STORAGEBUCKET_DATA_DIR = $dataPath
    $env:TEMP = $temporaryPath
    $env:TMP = $temporaryPath
    Start-Process -FilePath $executablePath -WorkingDirectory $repositoryPath -WindowStyle Normal
} finally { $env:STORAGEBUCKET_DATA_DIR = $previousDataPath; $env:TEMP = $previousTemp; $env:TMP = $previousTmp }
Write-Host "검증 데이터: $dataPath"
