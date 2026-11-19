param([switch]$ExistingData)
$ErrorActionPreference = 'Stop'
$repositoryPath = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$executablePath = Join-Path $repositoryPath 'dist/StorageBucket.exe'
if (-not (Test-Path -LiteralPath $executablePath)) { throw 'dist/StorageBucket.exe를 먼저 빌드하세요.' }
$dataPath = $repositoryPath
if (-not $ExistingData) {
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
try {
    $env:STORAGEBUCKET_DATA_DIR = $dataPath
    Start-Process -FilePath $executablePath -WorkingDirectory $repositoryPath -WindowStyle Normal
} finally { $env:STORAGEBUCKET_DATA_DIR = $previousDataPath }
Write-Host "검증 데이터: $dataPath"
