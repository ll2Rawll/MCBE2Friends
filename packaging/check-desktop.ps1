param([Parameter(Mandatory=$true)][string]$Binary)
$ErrorActionPreference = "Stop"
$folder = Join-Path $env:RUNNER_TEMP ("Mcbe2Friends-window-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $folder | Out-Null
$exe = Join-Path $folder "Mcbe2Friends.exe"
$profile = Join-Path $folder "profile"
Copy-Item $Binary $exe
function Open-Window {
    $process = Start-Process -FilePath $exe -ArgumentList "-data-dir `"$profile`"" -PassThru
    for ($i = 0; $i -lt 120; $i++) {
        $process.Refresh()
        if ($process.HasExited) { throw "The standalone launcher exited before showing a window." }
        if ($process.MainWindowHandle -ne 0 -and $process.MainWindowTitle -eq "Mcbe2Friends") { return $process }
        Start-Sleep -Milliseconds 250
    }
    throw "The standalone EXE did not open its own window."
}
function Close-Window($process) {
    if (!$process.CloseMainWindow() -or !$process.WaitForExit(15000)) { throw "The window did not close." }
    $discovery = Join-Path $profile "launcher.json"
    for ($i = 0; $i -lt 80 -and (Test-Path $discovery); $i++) { Start-Sleep -Milliseconds 100 }
    if (Test-Path $discovery) { throw "The idle worker did not exit after the window closed." }
}
$window = Open-Window
$second = Start-Process -FilePath $exe -ArgumentList "-data-dir `"$profile`"" -PassThru
if (!$second.WaitForExit(15000)) { throw "Double-clicking again opened a second set of controls." }
$instance = Get-Content (Join-Path $profile "launcher.json") -Raw | ConvertFrom-Json
$api = "http://127.0.0.1:$($instance.port)/api"
$auth = @{Authorization = "Bearer $($instance.token)"}
Invoke-RestMethod -Method Post -Uri "$api/settings" -Headers $auth -ContentType "application/json" -Body '{"host":"play.example.net","port":19132,"name":"Desktop test"}' | Out-Null
Close-Window $window
$window = Open-Window
$instance = Get-Content (Join-Path $profile "launcher.json") -Raw | ConvertFrom-Json
$state = Invoke-RestMethod -Uri "http://127.0.0.1:$($instance.port)/api/state" -Headers @{Authorization="Bearer $($instance.token)"}
if ($state.settings.host -ne "play.example.net") { throw "Reopening the EXE lost the saved server." }
Close-Window $window
Write-Output "The shipping EXE opened a native window, reactivated it, closed, and reopened the saved profile."
