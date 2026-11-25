$ErrorActionPreference = 'Stop'
$approved = $false
try {
    $parent = [System.Diagnostics.Process]::GetProcessById([int]$env:YOURPET_UNINSTALL_PARENT)
    try {
        $null = $parent.Handle
        Add-Type -AssemblyName System.Windows.Forms
        [Console]::WriteLine('ready')
        if ([Console]::ReadLine() -cne 'remove') { exit 0 }
        $approved = $true
        if (-not $parent.WaitForExit(30000)) { throw 'YourPet is still running.' }
    } finally {
        $parent.Dispose()
    }

    $runKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($env:YOURPET_UNINSTALL_STARTUP_KEY, $true)
    if ($null -ne $runKey) {
        try { $runKey.DeleteValue($env:YOURPET_UNINSTALL_STARTUP_NAME, $false) }
        finally { $runKey.Dispose() }
    }

    $exe = $env:YOURPET_UNINSTALL_EXE
    for ($attempt = 0; $attempt -lt 8; $attempt++) {
        try {
            [System.IO.File]::Delete($exe)
            break
        } catch [System.IO.IOException] {
            if ($attempt -eq 7) { throw }
            Start-Sleep -Milliseconds 250
        }
    }
    $data = $env:YOURPET_UNINSTALL_DATA
    if ($data) {
        foreach ($name in @('settings.json', 'current.pet')) {
            $file = Join-Path $data $name
            if (Test-Path -LiteralPath $file -PathType Leaf) {
                Remove-Item -LiteralPath $file -Force
            }
        }
        if ((Test-Path -LiteralPath $data -PathType Container) -and
                -not (Get-ChildItem -LiteralPath $data -Force | Select-Object -First 1)) {
            [System.IO.Directory]::Delete($data)
        }
    }
} catch {
    if ($approved -and $env:YOURPET_UNINSTALL_ERROR) {
        $null = [System.Windows.Forms.MessageBox]::Show(
            ($env:YOURPET_UNINSTALL_ERROR + "`r`n`r`n" + $_.Exception.Message), 'YourPet', 'OK', 'Error')
    } else {
        [Console]::Error.WriteLine($_.Exception.Message)
    }
    exit 1
}
