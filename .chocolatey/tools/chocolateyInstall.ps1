$ErrorActionPreference = 'Stop'
$toolsDir = "$(Split-Path -parent $MyInvocation.MyCommand.Definition)"

$packageArgs = @{
  packageName    = 'mockzilla'
  fileFullPath   = Join-Path $toolsDir 'mockzilla.exe'
  url64bit       = 'https://github.com/mockzilla/mockzilla/releases/download/v$version$/mockzilla-v$version$-windows-amd64.exe'
  checksum64     = '$checksum_amd64$'
  checksumType64 = 'sha256'
}

Get-ChocolateyWebFile @packageArgs
