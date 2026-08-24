<#
.SYNOPSIS
  Resolves the M365Bridge master passphrase from Windows DPAPI (or a
  generated plaintext fallback file if DPAPI is unavailable), then runs the
  given command with M365_MASTER_PASSPHRASE_VALUE set for that process alone.

.EXAMPLE
  scripts\with-passphrase.ps1 docker compose up -d
.EXAMPLE
  scripts\with-passphrase.ps1 .\bin\m365-bridge.exe serve

  The passphrase never touches a persistent environment variable or
  docker-compose.yml. DPAPI ties the stored blob to the current Windows user
  account, matching the trust boundary of macOS Keychain / Linux Secret
  Service on the other platforms this project supports.
#>

$ErrorActionPreference = "Stop"

# A declarative param() block with a [Parameter(...)] attribute turns this
# script into an "advanced" script, which gains PowerShell's implicit
# CommonParameters (-Debug, -Confirm, -Verbose, ...). PowerShell then binds
# any unrecognized -x token against a unique prefix of those before it ever
# reaches our parameter - so `docker compose up -d` silently loses -d to
# -Debug. Reading the raw $args array sidesteps parameter binding entirely.
$Command = $args
if ($Command.Length -eq 0) {
	Write-Error "usage: $($MyInvocation.MyCommand.Name) <command> [args...]"
	exit 64
}

# APPDATA is Windows-only, which is where DPAPI applies anyway; pwsh on
# macOS/Linux has no APPDATA, so this falls back to the same base the bash
# script uses, purely so the plaintext fallback below is reachable there too.
$ConfigBase   = if ($env:APPDATA) { $env:APPDATA } else { Join-Path $HOME ".config" }
$FallbackDir  = Join-Path $ConfigBase "m365bridge"
$DpapiFile    = Join-Path $FallbackDir "passphrase.dat"
$PlainFile    = Join-Path $FallbackDir "passphrase.txt"

function New-Passphrase {
	$bytes = New-Object byte[] 32
	[System.Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
	[Convert]::ToBase64String($bytes)
}

function Get-DpapiPassphrase {
	# ConvertTo-SecureString/ConvertFrom-SecureString without -Key silently
	# succeed on pwsh for macOS/Linux, but without DPAPI backing they just
	# reverse a UTF-16LE encoding - not encryption. Gate on $IsWindows so the
	# absence of real DPAPI is treated as unavailable, not as success.
	if (-not $IsWindows) {
		throw "DPAPI is only available on Windows"
	}
	New-Item -ItemType Directory -Force -Path $FallbackDir | Out-Null
	if (Test-Path $DpapiFile) {
		$secure = Get-Content $DpapiFile | ConvertTo-SecureString
		$bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
		try {
			return [Runtime.InteropServices.Marshal]::PtrToStringAuto($bstr)
		} finally {
			[Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)
		}
	}
	$plain = New-Passphrase
	($plain | ConvertTo-SecureString -AsPlainText -Force | ConvertFrom-SecureString) | Set-Content $DpapiFile
	Protect-FileToOwnerOnly $DpapiFile
	return $plain
}

function Protect-FileToOwnerOnly {
	param([string]$Path)
	if ($IsWindows) {
		icacls $Path /inheritance:r /grant:r "$($env:USERNAME):(R,W)" | Out-Null
	} elseif (Get-Command chmod -ErrorAction SilentlyContinue) {
		& chmod 600 $Path
	}
}

function Get-PlaintextFallback {
	New-Item -ItemType Directory -Force -Path $FallbackDir | Out-Null
	if (-not (Test-Path $PlainFile)) {
		New-Passphrase | Set-Content $PlainFile
		Protect-FileToOwnerOnly $PlainFile
		Write-Warning "DPAPI unavailable; the master passphrase was generated and saved in the clear at:"
		Write-Warning "  $PlainFile"
		Write-Warning "This is a last-resort fallback with no real protection."
	}
	return (Get-Content $PlainFile -Raw).Trim()
}

try {
	$Passphrase = Get-DpapiPassphrase
} catch {
	$Passphrase = Get-PlaintextFallback
}

$env:M365_MASTER_PASSPHRASE_VALUE = $Passphrase
try {
	$exe = $Command[0]
	# @() in an if/else branch produces no pipeline output, so assigning it
	# through if/else yields $null rather than an empty array, and splatting
	# @$null injects a bogus argument. Select-Object -Skip always returns an
	# array (empty when there's nothing left), avoiding that and the separate
	# 1..0 range-operator pitfall (it counts downward instead of being empty).
	$exeArgs = @($Command | Select-Object -Skip 1)
	# Piping through Out-Host forces PowerShell cmdlets/aliases (e.g. `pwd`,
	# as opposed to a native executable) to flush their formatted output
	# before exit runs; without it, an immediately following exit can
	# discard output that was still in the formatting pipeline.
	& $exe @exeArgs | Out-Host
	exit $LASTEXITCODE
} finally {
	Remove-Item Env:\M365_MASTER_PASSPHRASE_VALUE -ErrorAction SilentlyContinue
}
