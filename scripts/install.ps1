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

Write-Msg -zh "正在安装/更新 PolarisAGI Hermes..." -en "Installing/Updating PolarisAGI Hermes..." -Color Cyan

$Repo = "polarisagi/hermes"
$BinName = "hermes.exe"
$InstallDir = "$env:USERPROFILE\.polarisagi\hermes\bin"

if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
}

$Process = Get-Process -Name "hermes" -ErrorAction SilentlyContinue
if ($Process) {
    Write-Msg -zh "正在停止旧进程..." -en "Stopping existing process..." -Color Cyan
    Stop-Process -Name "hermes" -Force -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 2
}

$Arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
$GithubUrl = "https://github.com/$Repo/releases/latest/download/hermes-windows-$Arch.zip"

Write-Msg -zh "🌐 正在检测 GitHub 网络连通性..." -en "🌐 Checking GitHub connectivity..." -Color Cyan
$UseProxy = $false
try {
    Invoke-WebRequest -Uri "https://github.com" -UseBasicParsing -TimeoutSec 5 -Method Head -ErrorAction Stop | Out-Null
} catch {
    Write-Msg -zh "⚠️ 访问 GitHub 超时，自动切换至国内加速节点 (ghproxy.net)..." -en "⚠️ GitHub timeout, switching to domestic proxy..." -Color Yellow
    $UseProxy = $true
}

$Sources = New-Object System.Collections.ArrayList
if ($UseProxy) {
    $Sources.Add("https://ghproxy.net/$GithubUrl") | Out-Null
    $Sources.Add("https://mirror.ghproxy.com/$GithubUrl") | Out-Null
    $Sources.Add($GithubUrl) | Out-Null
} else {
    $Sources.Add($GithubUrl) | Out-Null
    $Sources.Add("https://ghproxy.net/$GithubUrl") | Out-Null
    $Sources.Add("https://mirror.ghproxy.com/$GithubUrl") | Out-Null
}

Write-Msg -zh "正在下载程序... (文件大小约20MB，请耐心等待)" -en "Downloading binary... (Approx. 20MB, please wait)" -Color Cyan

$ProgressPreference = 'SilentlyContinue'
$ZipPath = "$InstallDir\hermes-temp.zip"

$Downloaded = $false
foreach ($src in $Sources) {
    Write-Msg -zh "  尝试: $src" -en "  Trying: $src" -Color Gray
    try {
        Invoke-WebRequest -Uri $src -OutFile $ZipPath -UseBasicParsing
        $Downloaded = $true
        break
    } catch {
        Write-Msg -zh "  此源失败，尝试下一个..." -en "  This source failed, trying next..." -Color Yellow
    }
}

if (-not $Downloaded) {
    Write-Msg -zh "所有下载源均失败，请检查网络连接。" -en "All download sources failed. Please check your network connection." -Color Red
    if ($IsZh) { Read-Host "按回车键退出" } else { Read-Host "Press Enter to exit" }
    exit 1
}

Write-Msg -zh "📦 正在解压..." -en "📦 Extracting..." -Color Cyan
Expand-Archive -Path $ZipPath -DestinationPath $InstallDir -Force
Remove-Item -Path $ZipPath -Force

$ExtractedExe = "$InstallDir\hermes-windows-$Arch.exe"
if (Test-Path $ExtractedExe) {
    Rename-Item -Path $ExtractedExe -NewName $BinName -Force
}

Unblock-File -Path "$InstallDir\$BinName" -ErrorAction SilentlyContinue

Write-Msg -zh "正在配置开机自启..." -en "Configuring startup on login..." -Color Cyan
Set-ItemProperty -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run" -Name "PolarisHermes" -Value "`"$InstallDir\$BinName`""

Write-Msg -zh "正在启动服务..." -en "Starting service..." -Color Cyan
Start-Process -FilePath "$InstallDir\$BinName"

Write-Msg -zh "安装完成！PolarisAGI Hermes 已在后台运行，下次登录将自动启动。" -en "Installation complete! PolarisAGI Hermes is running in the background and will auto-start on next login." -Color Green
Write-Msg -zh "请访问: http://127.0.0.1:27777" -en "Please visit: http://127.0.0.1:27777" -Color Yellow
if ($IsZh) { Read-Host "按回车键退出" } else { Read-Host "Press Enter to exit" }
