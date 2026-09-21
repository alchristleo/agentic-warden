# Registers aw-sync as a scheduled task that runs `aw-sync once` as SYSTEM
# every five minutes and at startup. Run from an elevated PowerShell.
$exe = 'C:\Program Files\AgentWrapper\aw-sync.exe'
$action = New-ScheduledTaskAction -Execute $exe -Argument 'once'
$interval = New-ScheduledTaskTrigger -Once -At (Get-Date) -RepetitionInterval (New-TimeSpan -Minutes 5)
$startup = New-ScheduledTaskTrigger -AtStartup
$principal = New-ScheduledTaskPrincipal -UserId 'SYSTEM' -LogonType ServiceAccount -RunLevel Highest
$settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -MultipleInstances IgnoreNew
Register-ScheduledTask -TaskName 'agent-wrapper\aw-sync' -Action $action -Trigger @($interval, $startup) -Principal $principal -Settings $settings -Force
