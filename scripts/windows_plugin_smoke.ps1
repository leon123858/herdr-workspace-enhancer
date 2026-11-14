param([Parameter(Mandatory=$true)][string]$HerdrBin,[Parameter(Mandatory=$true)][string]$PluginRoot)
$ErrorActionPreference = 'Stop'
# Python's standard sqlite3 module inspects the session database for this test.
$python = (Get-Command python -ErrorAction Stop).Source
$root = Join-Path $env:TEMP ('enhancer-plugin-test-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $root | Out-Null
$oldAppData=$env:APPDATA; $oldLocal=$env:LOCALAPPDATA; $oldSocket=$env:HERDR_SOCKET_PATH
$env:APPDATA=$root; $env:LOCALAPPDATA=$root; Remove-Item Env:HERDR_SOCKET_PATH -ErrorAction SilentlyContinue
$plugin = Get-Item -LiteralPath $PluginRoot
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
  for($invocation=1;$invocation -le 2;$invocation++) {
  Invoke-Herdr @('plugin','action','invoke','workspace.enhancer.open') | Out-Null
  $done=$false
  for($i=0;$i -lt 40;$i++) {
    Start-Sleep -Milliseconds 250
    $logs=(Invoke-Herdr @('plugin','log','list','--plugin','workspace.enhancer')).logs
    if($logs | Where-Object {$_.status -eq 'failed'}){throw ($logs | ConvertTo-Json -Depth 10)}
    if(@($logs | Where-Object {$_.status -eq 'succeeded' -and $_.action_id -eq 'open'}).Count -eq $invocation){$done=$true;break}
  }
  if(!$done){throw 'Plugin action did not complete'}
  }
  $workspaces=@((Invoke-Herdr @('workspace','list')).workspaces)
  if($workspaces.Count -ne 2 -or $workspaces[0].workspace_id -eq $workspaces[1].workspace_id){throw 'Expected two distinct manager workspaces'}
  $managers=@()
  foreach($workspace in $workspaces) {
  $tabs=@((Invoke-Herdr @('tab','list','--workspace',$workspace.workspace_id)).tabs)
  if($tabs.Count -ne 1 -or $tabs[0].number -ne 1){throw 'Manager must retain its first and only tab'}
  $panes=@((Invoke-Herdr @('pane','list','--workspace',$workspace.workspace_id)).panes)
  if($panes.Count -ne 1 -or $panes[0].tab_id -ne $tabs[0].tab_id){throw 'Expected only the manager pane in the first tab'}
  $manager=$panes | Where-Object {$_.label -eq 'Workspace Enhancer'} | Select-Object -First 1
  if(!$manager){throw 'Manager pane is missing'}
  $managers += $manager.pane_id
  Invoke-Herdr @('pane','send-text',$manager.pane_id,'r') | Out-Null
  Start-Sleep -Milliseconds 500
  $screen=& $HerdrBin --session $session pane read $manager.pane_id --source visible --lines 40
  if(($screen -join "`n") -notmatch 'Herdr Workspace Enhancer'){throw ('TUI heading missing: '+($screen -join "`n"))}
  if(($screen -join "`n") -match '\[active\].*Workspace Enhancer'){throw 'Manager workspace appeared in archive list'}
  }
  $project=(Invoke-Herdr @('workspace','create','--cwd',$root,'--label','event-created','--no-focus')).workspace
  function Wait-ManagerText([string]$Needle,[bool]$Present) {
    for($attempt=0;$attempt -lt 60;$attempt++) {
      $matched=$true
      foreach($paneID in $managers) {
        $screen=& $HerdrBin --session $session pane read $paneID --source visible --lines 40
        if((($screen -join "`n").Contains($Needle)) -ne $Present){$matched=$false}
      }
      if($matched){return}
      Start-Sleep -Milliseconds 200
    }
    throw ('Manager event update not visible: '+$Needle)
  }
  Wait-ManagerText 'event-created' $true
  Invoke-Herdr @('workspace','rename',$project.workspace_id,'event-renamed') | Out-Null
  Wait-ManagerText 'event-renamed' $true
  Invoke-Herdr @('workspace','close',$project.workspace_id) | Out-Null
  Wait-ManagerText 'event-renamed' $false
  $databases=@(Get-ChildItem -LiteralPath $root -Filter state.sqlite -Recurse)
  if($databases.Count -ne 1){throw 'Expected one session database'}
  $query = 'import json,sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.row_factory=sqlite3.Row; print(json.dumps([dict(r) for r in db.execute("SELECT * FROM workspaces WHERE is_manager = 0 ORDER BY rowid")]))'
  $recordJson = & $python -c $query $databases[0].FullName
  if($LASTEXITCODE -ne 0){throw 'Failed to inspect SQLite session database'}
  $records=@($recordJson | ConvertFrom-Json)
  if($records.Count -ne 1 -or $records[0].host_id -ne $project.workspace_id -or $records[0].status -ne 'closed'){throw 'Manager recorded or project close not reconciled'}
  $logs=(Invoke-Herdr @('plugin','log','list','--plugin','workspace.enhancer')).logs
  foreach($eventName in @('workspace.created','workspace.renamed','workspace.closed')) {
    if(!($logs | Where-Object {$_.event -eq $eventName -and $_.status -eq 'succeeded'})){throw ('Event hook missing: '+$eventName)}
  }
  if($logs | Where-Object {$_.status -eq 'failed'}){throw ($logs | ConvertTo-Json -Depth 10)}
  foreach($paneID in $managers){Invoke-Herdr @('pane','send-text',$paneID,'q') | Out-Null}
  Write-Output 'PASS: two Windows managers; create/rename/close hooks update both TUIs automatically; manager workspaces absent from database'
} finally {
  & $HerdrBin --session $session server stop
  $server.WaitForExit(15000) | Out-Null
  $env:APPDATA=$oldAppData;$env:LOCALAPPDATA=$oldLocal;$env:HERDR_SOCKET_PATH=$oldSocket
  Write-Output ('Test logs: '+$root)
}
