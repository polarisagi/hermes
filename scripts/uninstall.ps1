[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$Culture = [System.Globalization.CultureInfo]::InstalledUICulture.Name
$IsZh = ($Culture -match "zh")

function Write-Msg {
    param (
        [string]$zh,
        [string]$en,
        [ConsoleColor]$Color = 'White'
    )
    if ($IsZh) {
        Write-Host $zh -ForegroundColor $Color
    } else {
        Write-Host $en -ForegroundColor $Color
    }
}

Write-Msg -zh "正在卸载 PolarisAGI Hermes..." -en "Uninstalling PolarisAGI Hermes..." -Color Cyan

$InstallDir = "$env:USERPROFILE\.polarisagi\hermes\bin"
$StartupDir = "$env:APPDATA\Microsoft\Windows\Start Menu\Programs\Startup"
$VbsPath = "$StartupDir\polarisagi-hermes.vbs"

$Process = Get-Process -Name "hermes" -ErrorAction SilentlyContinue
if ($Process) {
    Write-Msg -zh "正在停止运行中的进程..." -en "Stopping running process..." -Color Cyan
    Stop-Process -Name "hermes" -Force -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 2
}

Write-Msg -zh "正在移除开机自启配置..." -en "Removing startup configuration..." -Color Cyan
Remove-ItemProperty -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run" -Name "PolarisHermes" -ErrorAction SilentlyContinue

if (Test-Path $VbsPath) {
    Remove-Item -Path $VbsPath -Force -ErrorAction SilentlyContinue
}

if (Test-Path $InstallDir) {
    Write-Msg -zh "正在删除程序文件..." -en "Removing program files..." -Color Cyan
    Remove-Item -Path $InstallDir -Recurse -Force
}

Write-Host ""
Write-Msg -zh "卸载完成！" -en "Uninstallation complete!" -Color Green
Write-Msg -zh "提示: 数据库和配置文件仍保留在 $env:USERPROFILE\.polarisagi\hermes 目录中。" -en "Notice: Database and config files remain in $env:USERPROFILE\.polarisagi\hermes directory." -Color Yellow
Write-Msg -zh "如需彻底清理，请手动执行: Remove-Item -Path `"$env:USERPROFILE\.polarisagi\hermes`" -Recurse -Force" -en "To completely clean up, run: Remove-Item -Path `"$env:USERPROFILE\.polarisagi\hermes`" -Recurse -Force" -Color Yellow
if ($IsZh) { Read-Host "按回车键退出" } else { Read-Host "Press Enter to exit" }
