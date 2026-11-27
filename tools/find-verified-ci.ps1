param(
    [string]$Repository = $env:GITHUB_REPOSITORY,
    [string]$Commit = $env:GITHUB_SHA,
    [string]$Branch = 'main',
    [ValidateRange(0, 600)][int]$WaitSeconds = 300,
    [switch]$LibraryOnly
)

# Request and Sleep are injectable so the release gate can be tested offline.
function Find-VerifiedCIRun {
    param(
        [string]$Repository,
        [string]$Commit,
        [string]$Branch,
        [scriptblock]$Request,
        [int]$WaitSeconds = 0,
        [int]$PollSeconds = 15,
        [scriptblock]$Sleep = { param($Seconds) Start-Sleep -Seconds $Seconds }
    )
    if ($Repository -notmatch '^[\w.-]+/[\w.-]+$' -or $Commit -notmatch '^[0-9a-fA-F]{40}$' -or -not $Branch) {
        throw 'A repository, full commit SHA and branch are required.'
    }
    if ($WaitSeconds -lt 0 -or $PollSeconds -lt 1) { throw 'Invalid CI polling interval.' }
    $timer = [Diagnostics.Stopwatch]::StartNew()
    $remaining = $WaitSeconds
    $path = 'actions/workflows/ci.yml/runs?event=push&head_sha={0}&branch={1}&per_page=100' -f $Commit, [Uri]::EscapeDataString($Branch)
    try {
        while ($true) {
            $response = & $Request $path
            # Never reuse PR/fork/manual runs, another workflow or another commit.
            $runs = @($response.workflow_runs | Where-Object {
                $_.path -ceq '.github/workflows/ci.yml' -and
                $_.head_sha -eq $Commit -and $_.head_branch -ceq $Branch -and
                $_.event -ceq 'push' -and $_.head_repository.full_name -ieq $Repository -and
                [string]$_.id -match '^[1-9][0-9]*$'
            })
            foreach ($run in ($runs | Where-Object { $_.status -eq 'completed' -and $_.conclusion -eq 'success' })) {
                $assets = & $Request "actions/runs/$($run.id)/artifacts?per_page=100"
                $artifacts = @($assets.artifacts | Where-Object {
                    $_.name -ceq 'ci-windows-amd64' -and $_.expired -eq $false -and
                    $_.size_in_bytes -gt 0 -and $_.workflow_run.id -eq $run.id -and
                    $_.workflow_run.head_sha -eq $Commit -and
                    [DateTimeOffset]::Parse($_.expires_at) -gt [DateTimeOffset]::UtcNow
                })
                if ($artifacts.Count -eq 1) {
                    Write-Host "Reusing successful CI run $($run.id) for commit $Commit."
                    return [string]$run.id
                }
            }
            $pending = @($runs | Where-Object { $_.status -in @('queued', 'in_progress', 'waiting', 'pending', 'requested') })
            # API calls count toward the wait budget as well as polling sleeps.
            $remaining = [Math]::Min($remaining, [Math]::Max(0, $WaitSeconds - [Math]::Floor($timer.Elapsed.TotalSeconds)))
            if ($pending.Count -eq 0 -or $remaining -le 0) { break }
            $delay = [Math]::Min($PollSeconds, $remaining)
            Write-Host "Waiting ${delay}s for existing CI on $Commit (${remaining}s remaining)."
            & $Sleep $delay
            $remaining -= $delay
        }
    } catch {
        # An unavailable API or artifact must select full verification, not skip it.
        Write-Warning 'CI reuse lookup failed; running full verification instead.'
    }
    Write-Host 'No reusable CI artifact found; running full verification.'
    return ''
}

if ($LibraryOnly) { return }
$ErrorActionPreference = 'Stop'
$apiRoot = if ($env:GITHUB_API_URL) { $env:GITHUB_API_URL.TrimEnd('/') } else { 'https://api.github.com' }
$headers = @{ Accept = 'application/vnd.github+json'; 'User-Agent' = 'Asterodia-release' }
if ($env:GH_TOKEN) { $headers.Authorization = "Bearer $env:GH_TOKEN" }
$request = {
    param($Path)
    Invoke-RestMethod -Uri "$apiRoot/repos/$Repository/$Path" -Headers $headers -TimeoutSec 20
}.GetNewClosure()
$verifiedRun = Find-VerifiedCIRun -Repository $Repository -Commit $Commit -Branch $Branch -Request $request -WaitSeconds $WaitSeconds
if ($env:GITHUB_OUTPUT) { "run-id=$verifiedRun" >> $env:GITHUB_OUTPUT }
Write-Output $verifiedRun
