param([Parameter(Mandatory=$true)][string]$HerdrBin,[Parameter(Mandatory=$true)][string]$Archive)
$ErrorActionPreference = 'Stop'
$root = Join-Path $env:TEMP ('enhancer-plugin-test-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $root | Out-Null
$oldAppData=$env:APPDATA; $oldLocal=$env:LOCALAPPDATA; $oldSocket=$env:HERDR_SOCKET_PATH
$env:APPDATA=$root; $env:LOCALAPPDATA=$root; Remove-Item Env:HERDR_SOCKET_PATH -ErrorAction SilentlyContinue
Expand-Archive -LiteralPath $Archive -DestinationPath $root
$plugin = Get-ChildItem -LiteralPath $root -Directory | Where-Object { Test-Path (Join-Path $_.FullName 'herdr-plugin.toml') } | Select-Object -First 1
if (!$plugin) { throw 'Package has no plugin manifest' }
$session='enhancer-smoke-'+[guid]::NewGuid().ToString('N').Substring(0,8)
$server=Start-Process -FilePath $HerdrBin -ArgumentList @('--session',$session,'server') -PassThru -WindowStyle Hidden -RedirectStandardOutput (Join-Path $root 'server.out') -RedirectStandardError (Join-Path $root 'server.err')
function Invoke-Herdr([string[]]$Arguments) {
  $raw=& $HerdrBin --session $session @Arguments
  if ($LASTEXITCODE -ne 0) { throw ('Herdr failed: '+($Arguments -join ' ')) }
  return ($raw | ConvertFrom-Json).result
}
try {
  $ready=$false
  for($i=0;$i -lt 40;$i++) {
    Start-Sleep -Milliseconds 250
    if($server.HasExited){throw 'Test server exited'}
    $sessions=(& $HerdrBin session list --json | ConvertFrom-Json).sessions
    if($sessions | Where-Object {$_.name -eq $session -and $_.running}){$ready=$true;break}
  }
  if(!$ready){throw 'Test server not ready'}
  Invoke-Herdr @('plugin','link',$plugin.FullName) | Out-Null
  Invoke-Herdr @('plugin','action','invoke','workspace.enhancer.open-windows') | Out-Null
  $done=$false
  for($i=0;$i -lt 40;$i++) {
    Start-Sleep -Milliseconds 250
    $logs=(Invoke-Herdr @('plugin','log','list','--plugin','workspace.enhancer')).logs
    if($logs | Where-Object {$_.status -eq 'failed'}){throw ($logs | ConvertTo-Json -Depth 10)}
    if($logs | Where-Object {$_.status -eq 'succeeded'}){$done=$true;break}
  }
  if(!$done){throw 'Plugin action did not complete'}
  $panes=(Invoke-Herdr @('pane','list')).panes
  $manager=$panes | Where-Object {$_.label -eq 'Workspace Enhancer'} | Select-Object -First 1
  if(!$manager){throw 'Manager pane is missing'}
  Start-Sleep -Milliseconds 500
  $screen=& $HerdrBin --session $session pane read $manager.pane_id --source visible --lines 40
  if(($screen -join "`n") -notmatch 'Herdr Workspace Enhancer'){throw ('TUI heading missing: '+($screen -join "`n"))}
  Invoke-Herdr @('pane','send-text',$manager.pane_id,'q') | Out-Null
  Write-Output 'PASS: packaged Windows plugin linked, action completed, TUI rendered, quit key delivered'
} finally {
  & $HerdrBin --session $session server stop
  $server.WaitForExit(15000) | Out-Null
  $env:APPDATA=$oldAppData;$env:LOCALAPPDATA=$oldLocal;$env:HERDR_SOCKET_PATH=$oldSocket
  Write-Output ('Test logs: '+$root)
}
