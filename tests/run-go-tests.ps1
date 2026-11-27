param(
    [switch]$Race,
    [switch]$Live,
    [string[]]$Groups = @(),
    [ValidateRange(1, 4)][int]$Jobs = 1,
    [switch]$SkipVet
)
$ErrorActionPreference = 'Stop'
$repositoryPath = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$runId = [Guid]::NewGuid().ToString('N')
$testFlags = @()
if ($Race) { $testFlags += '-race' }
$testGroups = @(
    @{ Category = 'update'; Package = './tests/update' }
    @{ Category = 'licenses'; Package = './tests/licenses' }
    @{ Category = 'engine-notices'; Package = './internal/engine'; Pattern = '^TestEngineNotices' }
    @{ Category = 'database-open'; Package = './tests/database' }
    @{ Category = 'mediaedit'; Package = './tests/mediaedit' }
    @{ Category = 'local-import'; Package = './tests/localimport' }
    @{ Category = 'download-storage'; Package = './tests/download' }
    @{ Category = 'engine-process'; Package = './tests/engine'; Pattern = '^TestProcess' }
    @{ Category = 'gallerydl-download'; Package = './tests/engine'; Pattern = '^TestGalleryDL(SaveImages|DownloadResults)$' }
    @{ Category = 'gallerydl-discovery'; Package = './tests/engine'; Pattern = '^TestGalleryDLDiscoverBluesky$' }
    @{ Category = 'ytdlp-download'; Package = './tests/engine'; Pattern = '^TestYtdlpDownload' }
    @{ Category = 'engine-parsers'; Package = './tests/engine'; Pattern = '^Test(ParseGalleryDLDiscovery|GalleryDLInstagramTargetKey|GalleryDLCommonTargets|YtdlpTargetKeyCompatibility|Instagram.*|DiscoveryChain.*)$' }
    @{ Category = 'integration'; Package = './tests/integration' }
    @{ Category = 'thumbnail'; Package = './tests/thumbnail' }
    @{ Category = 'webserver'; Package = './tests/webserver' }
    @{ Category = 'filemove'; Package = './tests/filemove'; CoverPackage = './internal/filemove' }
)
foreach ($group in $Groups) {
    if ($group -notin $testGroups.Category) { throw "Unknown test group: $group" }
}
$selectedGroups = @($testGroups | Where-Object { $Groups.Count -eq 0 -or $_.Category -in $Groups })
function Invoke-TestGroup {
    param([string]$Category, [string]$Package, [string]$Pattern = '.', [string]$CoverPackage = '')
    $timer = [Diagnostics.Stopwatch]::StartNew()
    Write-Output "[$Category]"
    $env:ASTERODIA_TEST_ROOT = Join-Path $repositoryPath ".test-data/$Category/$runId"
    New-Item -ItemType Directory -Force -Path $env:ASTERODIA_TEST_ROOT | Out-Null
    $binaryPath = Join-Path $env:ASTERODIA_TEST_ROOT 'suite.test.exe'
    $coverFlags = @()
    $runtimeFlags = @()
    if ($CoverPackage) {
        $coverFlags = @('-cover', "-coverpkg=$CoverPackage")
        $profilePath = Join-Path $env:ASTERODIA_TEST_ROOT 'coverage.out'
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
    Write-Output ('[{0}] passed in {1:N1}s (including compilation)' -f $Category, $timer.Elapsed.TotalSeconds)
}
function Invoke-ParallelTestGroups {
    # Start-Job uses separate processes: cwd and test environment variables must
    # not be shared between groups. Every child also creates its own fixture root.
    $workers = [Collections.Generic.List[object]]::new()
    $pending = [Collections.Generic.Queue[object]]::new()
    $priority = @('integration', 'local-import', 'ytdlp-download', 'mediaedit')
    foreach ($group in ($selectedGroups | Sort-Object { $index = $priority.IndexOf($_.Category); if ($index -lt 0) { 100 } else { $index } })) {
        $pending.Enqueue($group)
    }
    try {
        while ($pending.Count -gt 0 -or $workers.Count -gt 0) {
            while ($pending.Count -gt 0 -and $workers.Count -lt $Jobs) {
                $group = $pending.Dequeue()
                $worker = Start-Job -Name $group.Category -ScriptBlock {
                    param($Runner, $Category, $RunRace, $RunLive)
                    $ErrorActionPreference = 'Stop'
                    [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
                    & $Runner -Groups @($Category) -Jobs 1 -SkipVet -Race:$RunRace -Live:$RunLive
                } -ArgumentList $PSCommandPath, $group.Category, ([bool]$Race), ([bool]$Live)
                $workers.Add($worker)
                Write-Output "[$($group.Category)] started"
            }
            $finished = Wait-Job -Job $workers.ToArray() -Any
            Write-Output "::group::$($finished.Name)"
            try {
                Receive-Job -Job $finished -ErrorAction Stop
                if ($finished.State -ne 'Completed') { throw "Test group failed: $($finished.Name)" }
            } finally { Write-Output '::endgroup::' }
            Remove-Job -Job $finished
            [void]$workers.Remove($finished)
        }
    } finally {
        foreach ($worker in $workers) {
            Stop-Job -Job $worker
            Remove-Job -Job $worker -Force
        }
    }
}
Push-Location $repositoryPath
$oldRoot = $env:ASTERODIA_TEST_ROOT
$oldLive = $env:ASTERODIA_LIVE_TESTS
$oldYtdlp = $env:ASTERODIA_TEST_YTDLP_EXE
$oldGallery = $env:ASTERODIA_TEST_GALLERYDL_EXE
$oldProbe = $env:ASTERODIA_TEST_FFPROBE_EXE
if (-not $oldYtdlp -and -not $oldProbe) {
    $probeCommand = Get-Command ffprobe -ErrorAction SilentlyContinue
    $localYtdlp = Join-Path $repositoryPath 'bin/yt-dlp.exe'
    if ($probeCommand -and (Test-Path -LiteralPath $localYtdlp)) {
        $env:ASTERODIA_TEST_YTDLP_EXE = $localYtdlp
        $env:ASTERODIA_TEST_FFPROBE_EXE = $probeCommand.Source
    }
}
if (-not $oldGallery -and (Test-Path -LiteralPath (Join-Path $repositoryPath 'bin/gallery-dl.exe'))) {
    $env:ASTERODIA_TEST_GALLERYDL_EXE = Join-Path $repositoryPath 'bin/gallery-dl.exe'
}
if ($Live) { $env:ASTERODIA_LIVE_TESTS = '1' } else { $env:ASTERODIA_LIVE_TESTS = '0' }
try {
    if ($Jobs -gt 1) {
        Invoke-ParallelTestGroups
    } else {
        foreach ($group in $selectedGroups) { Invoke-TestGroup @group }
    }
    if (-not $SkipVet) {
        $vetTimer = [Diagnostics.Stopwatch]::StartNew()
        & go vet . ./internal/... ./tests/...
        if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }
        Write-Output ('[go-vet] passed in {0:N1}s' -f $vetTimer.Elapsed.TotalSeconds)
    }
} finally {
    $env:ASTERODIA_TEST_ROOT = $oldRoot
    $env:ASTERODIA_LIVE_TESTS = $oldLive
    $env:ASTERODIA_TEST_YTDLP_EXE = $oldYtdlp
    $env:ASTERODIA_TEST_GALLERYDL_EXE = $oldGallery
    $env:ASTERODIA_TEST_FFPROBE_EXE = $oldProbe
    Pop-Location
}
