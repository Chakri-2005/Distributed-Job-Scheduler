# Run this script as Administrator to allow cluster traffic through Windows Firewall
# Usage: Right-click PowerShell -> Run as Administrator -> run this script

Write-Host "`n=== Distributed Job Scheduler - Firewall Setup ===" -ForegroundColor Cyan

# Remove old rules if they exist
$ruleNames = @("DJS-Master-8080", "DJS-Slave1-8081", "DJS-Slave2-8082", "DJS-Slave3-8083", "DJS-Slave4-8084", "DJS-Cluster-AllPorts")
foreach ($name in $ruleNames) {
    netsh advfirewall firewall delete rule name="$name" 2>$null | Out-Null
}
Write-Host "Cleaned old rules" -ForegroundColor Yellow

# Create inbound rules for each port (all profiles: Domain, Private, Public)
$ports = @(8080, 8081, 8082, 8083, 8084)
foreach ($port in $ports) {
    $name = "DJS-Port-$port"
    netsh advfirewall firewall delete rule name="$name" 2>$null | Out-Null
    netsh advfirewall firewall add rule name="$name" dir=in action=allow protocol=TCP localport=$port profile=any enable=yes
    Write-Host "  + Rule created: $name (TCP $port, all profiles)" -ForegroundColor Green
}

# Also allow the Go binary itself through the firewall
$binaryPath = Join-Path $PSScriptRoot "api-server\api-server.exe"
if (Test-Path $binaryPath) {
    netsh advfirewall firewall delete rule name="DJS-GoServer" 2>$null | Out-Null
    netsh advfirewall firewall add rule name="DJS-GoServer" dir=in action=allow program="$binaryPath" profile=any enable=yes
    Write-Host "  + Rule created: DJS-GoServer (binary allowed)" -ForegroundColor Green
}

Write-Host "`n=== Firewall rules configured ===" -ForegroundColor Cyan
Write-Host "Other devices on the same network can now access the cluster.`n" -ForegroundColor White

# Verify
Write-Host "Verification:" -ForegroundColor Yellow
netsh advfirewall firewall show rule name=all dir=in | Select-String -Pattern "DJS-" -Context 0,1

Write-Host "`nPress Enter to close..."
Read-Host
