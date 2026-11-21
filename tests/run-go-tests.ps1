param([switch]$Race, [switch]$Live, [string[]]$Groups = @())
$ErrorActionPreference = 'Stop'
$repositoryPath = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$runId = [Guid]::NewGuid().ToString('N')
$testFlags = @()
if ($Race) { $testFlags += '-race' }
function Invoke-TestGroup {
    param([string]$Category, [string]$Package, [string]$Pattern = '.', [string]$CoverPackage = '')
    if ($Groups.Count -gt 0 -and $Category -notin $Groups) { return }
    Write-Output "[$Category]"
    $env:STORAGEBUCKET_TEST_ROOT = Join-Path $repositoryPath ".test-data/$Category/$runId"
    New-Item -ItemType Directory -Force -Path $env:STORAGEBUCKET_TEST_ROOT | Out-Null
    $binaryPath = Join-Path $env:STORAGEBUCKET_TEST_ROOT 'suite.test.exe'
    $coverFlags = @()
    $runtimeFlags = @()
    if ($CoverPackage) {
        $coverFlags = @('-cover', "-coverpkg=$CoverPackage")
        $profilePath = Join-Path $env:STORAGEBUCKET_TEST_ROOT 'coverage.out'
        $runtimeFlags = @("-test.coverprofile=$profilePath")
    }
    & go test $Package '-c' '-o' $binaryPath @testFlags @coverFlags
    if ($LASTEXITCODE -ne 0) { throw "Test compilation failed: $Category" }
    Push-Location (Join-Path $repositoryPath $Package.Substring(2))
    try { & $binaryPath "-test.run=$Pattern" '-test.count=1' '-test.timeout=5m' @runtimeFlags } finally { Pop-Location }
    if ($LASTEXITCODE -ne 0) { throw "Test group failed: $Category" }
    if ($CoverPackage) {
        $coverage = (& go tool cover "-func=$profilePath" | Select-Object -Last 1)
        Write-Output $coverage
        if ($coverage -notmatch '([0-9]+(?:[.][0-9]+)?)%') { throw 'Coverage result missing' }
        if ([double]::Parse($Matches[1], [Globalization.CultureInfo]::InvariantCulture) -lt 75) { throw 'File-move statement coverage must be at least 75%' }
    }
}
Push-Location $repositoryPath
$oldRoot = $env:STORAGEBUCKET_TEST_ROOT
$oldLive = $env:STORAGEBUCKET_LIVE_TESTS
$oldYtdlp = $env:STORAGEBUCKET_TEST_YTDLP_EXE
$oldGallery = $env:STORAGEBUCKET_TEST_GALLERYDL_EXE
$oldProbe = $env:STORAGEBUCKET_TEST_FFPROBE_EXE
if (-not $oldYtdlp -and -not $oldProbe) {
    $probeCommand = Get-Command ffprobe -ErrorAction SilentlyContinue
    $localYtdlp = Join-Path $repositoryPath 'bin/yt-dlp.exe'
    if ($probeCommand -and (Test-Path -LiteralPath $localYtdlp)) {
        $env:STORAGEBUCKET_TEST_YTDLP_EXE = $localYtdlp
        $env:STORAGEBUCKET_TEST_FFPROBE_EXE = $probeCommand.Source
    }
}
if (-not $oldGallery -and (Test-Path -LiteralPath (Join-Path $repositoryPath 'bin/gallery-dl.exe'))) {
    $env:STORAGEBUCKET_TEST_GALLERYDL_EXE = Join-Path $repositoryPath 'bin/gallery-dl.exe'
}
if ($Live) { $env:STORAGEBUCKET_LIVE_TESTS = '1' } else { $env:STORAGEBUCKET_LIVE_TESTS = '0' }
try {
    Invoke-TestGroup 'licenses' './tests/licenses'
    Invoke-TestGroup 'engine-notices' './tests/engine' '^TestEngineNotices'
    Invoke-TestGroup 'database-open' './tests/database'
    Invoke-TestGroup 'mediaedit' './tests/mediaedit'
    Invoke-TestGroup 'local-import' './tests/localimport'
    Invoke-TestGroup 'download-storage' './tests/download'
    Invoke-TestGroup 'engine-process' './tests/engine' '^TestProcess'
    Invoke-TestGroup 'gallerydl-download' './tests/engine' '^TestGalleryDL(SaveImages|DownloadResults)$'
    Invoke-TestGroup 'gallerydl-discovery' './tests/engine' '^TestGalleryDLDiscoverBluesky$'
    Invoke-TestGroup 'ytdlp-download' './tests/engine' '^TestYtdlpDownload'
    Invoke-TestGroup 'engine-parsers' './tests/engine' '^Test(ParseGalleryDLDiscovery|GalleryDLInstagramTargetKey|GalleryDLCommonTargets|YtdlpTargetKeyCompatibility)$'
    Invoke-TestGroup 'integration' './tests/integration'
    Invoke-TestGroup 'thumbnail' './tests/thumbnail'
    Invoke-TestGroup 'webserver' './tests/webserver'
    Invoke-TestGroup 'filemove' './tests/filemove' -CoverPackage './internal/filemove'
    & go vet . ./internal/... ./tests/...
    if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }
} finally {
    $env:STORAGEBUCKET_TEST_ROOT = $oldRoot
    $env:STORAGEBUCKET_LIVE_TESTS = $oldLive
    $env:STORAGEBUCKET_TEST_YTDLP_EXE = $oldYtdlp
    $env:STORAGEBUCKET_TEST_GALLERYDL_EXE = $oldGallery
    $env:STORAGEBUCKET_TEST_FFPROBE_EXE = $oldProbe
    Pop-Location
}
