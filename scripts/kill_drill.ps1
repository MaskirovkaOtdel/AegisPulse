Param(
    [string]$GatewayURL = "http://localhost:8080"
)

Write-Host "===================================================================" -ForegroundColor Yellow
Write-Host "   🚨  AEGISPULSE: TWO-STEP SAFETY PANIC KILL-SWITCH DRILL  🚨   " -ForegroundColor Yellow
Write-Host "   Target Gateway: $GatewayURL" -ForegroundColor Yellow
Write-Host "===================================================================" -ForegroundColor Yellow

if (Test-Path "./bin/chaos.exe") {
    & ./bin/chaos.exe -mode=kill-drill -url="$GatewayURL"
} else {
    go run ./cmd/chaos -mode=kill-drill -url="$GatewayURL"
}
