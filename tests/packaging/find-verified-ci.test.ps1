$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot '../../tools/find-verified-ci.ps1') -LibraryOnly
$repo = 'owner/StorageBucket'
$sha = 'a' * 40
function Assert-Equal($Actual, $Expected, [string]$Message) {
    if ($Actual -cne $Expected) { throw "$Message (expected '$Expected', got '$Actual')" }
}
function New-Run {
    @{
        id = 42; path = '.github/workflows/ci.yml'; head_sha = $sha
        head_branch = 'main'; event = 'push'; status = 'completed'; conclusion = 'success'
        head_repository = @{ full_name = $repo }
    }
}
function New-Artifact {
    @{
        name = 'ci-windows-amd64'; expired = $false; size_in_bytes = 1024
        expires_at = [DateTimeOffset]::UtcNow.AddDays(1).ToString('o')
        workflow_run = @{ id = 42; head_sha = $sha }
    }
}
function Invoke-Case($Runs, $Artifacts, [int]$Wait = 0, [scriptblock]$Sleep = {}) {
    $request = {
        param($Path)
        if ($Path.StartsWith('actions/workflows/ci.yml/runs?')) { return @{ workflow_runs = $Runs } }
        if ($Path -eq 'actions/runs/42/artifacts?per_page=100') { return @{ artifacts = $Artifacts } }
        throw "Unexpected API path: $Path"
    }.GetNewClosure()
    Find-VerifiedCIRun -Repository $repo -Commit $sha -Branch main -Request $request -WaitSeconds $Wait -PollSeconds 1 -Sleep $Sleep
}
Assert-Equal (Invoke-Case @((New-Run)) @((New-Artifact))) '42' 'Successful same-commit CI should be reused'
Assert-Equal (Invoke-Case @() @()) '' 'Missing CI must run full verification'
foreach ($change in @(
    @{ head_sha = ('b' * 40) }, @{ head_branch = 'feature' }, @{ event = 'pull_request' },
    @{ event = 'workflow_dispatch' }, @{ path = '.github/workflows/other.yml' },
    @{ head_repository = @{ full_name = 'fork/StorageBucket' } },
    @{ conclusion = 'failure' }, @{ conclusion = 'cancelled' }, @{ conclusion = 'skipped' },
    @{ status = 'in_progress'; conclusion = $null }, @{ id = 'invalid' }
)) {
    $run = New-Run
    foreach ($key in $change.Keys) { $run[$key] = $change[$key] }
    Assert-Equal (Invoke-Case @($run) @((New-Artifact))) '' "Unsafe CI must be rejected: $($change.Keys -join ', ')"
}
foreach ($change in @(
    @{ name = 'wrong-artifact' }, @{ expired = $true }, @{ expired = $null }, @{ size_in_bytes = 0 },
    @{ expires_at = [DateTimeOffset]::UtcNow.AddDays(-1).ToString('o') },
    @{ workflow_run = @{ id = 41; head_sha = $sha } },
    @{ workflow_run = @{ id = 42; head_sha = ('b' * 40) } }
)) {
    $artifact = New-Artifact
    foreach ($key in $change.Keys) { $artifact[$key] = $change[$key] }
    Assert-Equal (Invoke-Case @((New-Run)) @($artifact)) '' "Invalid artifact must be rejected: $($change.Keys -join ', ')"
}
Assert-Equal (Invoke-Case @((New-Run)) @()) '' 'Missing artifacts must run full verification'
Assert-Equal (Invoke-Case @((New-Run)) @((New-Artifact), (New-Artifact))) '' 'Ambiguous artifacts must be rejected'
$failedRequest = { param($Path) throw 'Simulated API failure' }
Assert-Equal (Find-VerifiedCIRun -Repository $repo -Commit $sha -Branch main -Request $failedRequest) '' 'API failure must run full verification'
$pending = New-Run
$pending.status = 'in_progress'; $pending.conclusion = $null
$clock = @{ Calls = 0; Seconds = 0 }
$completeCI = { param($Seconds) $clock.Calls++; $clock.Seconds += $Seconds; $pending.status = 'completed'; $pending.conclusion = 'success' }.GetNewClosure()
Assert-Equal (Invoke-Case @($pending) @((New-Artifact)) -Wait 2 -Sleep $completeCI) '42' 'An existing CI may complete while waiting'
Assert-Equal $clock.Calls 1 'Polling should stop after success'
$pending.status = 'in_progress'; $pending.conclusion = $null
$clock.Calls = 0; $clock.Seconds = 0
$keepWaiting = { param($Seconds) $clock.Calls++; $clock.Seconds += $Seconds }.GetNewClosure()
Assert-Equal (Invoke-Case @($pending) @((New-Artifact)) -Wait 2 -Sleep $keepWaiting) '' 'Polling timeout must run full verification'
Assert-Equal $clock.Seconds 2 'Polling must respect its time budget'
Write-Output 'CI reuse tests passed (commit, branch, workflow, repository, success, artifacts, API failure and bounded polling).'
