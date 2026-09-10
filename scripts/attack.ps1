Param(
    [string]$GatewayURL = "http://localhost:8080"
)

Write-Host "===================================================================" -ForegroundColor Red
Write-Host "   🔥  AEGISPULSE: AUTOMATED CHAOS LOAD AND REPLAY TEST SUITE  🔥   " -ForegroundColor Red
Write-Host "   Target Gateway: $GatewayURL" -ForegroundColor Red
Write-Host "===================================================================" -ForegroundColor Red

if (Test-Path "./bin/chaos.exe") {
    & ./bin/chaos.exe -mode=attack -url="$GatewayURL"
} else {
    go run ./cmd/chaos -mode=attack -url="$GatewayURL"
}
