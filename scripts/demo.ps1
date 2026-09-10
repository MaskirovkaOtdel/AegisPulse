Param(
    [string]$GatewayURL = "http://localhost:8080"
)

Write-Host "===================================================================" -ForegroundColor Cyan
Write-Host "   🛡️  AEGISPULSE: 60-SECOND GUIDED END-TO-END DEMO SCENARIO  🛡️   " -ForegroundColor Cyan
Write-Host "   Target Gateway: $GatewayURL" -ForegroundColor Cyan
Write-Host "===================================================================" -ForegroundColor Cyan

if (Test-Path "./bin/chaos.exe") {
    & ./bin/chaos.exe -mode=demo -url="$GatewayURL"
} else {
    go run ./cmd/chaos -mode=demo -url="$GatewayURL"
}
