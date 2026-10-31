; The Matinee installer for Windows. scripts/package-windows.sh builds it:
;   makensis -DVERSION=0.1.8 -DNUMVER=0.1.8.0 -DARCH=x64 -DSTAGE=<folder> -DOUTFILE=<exe> [-DSIGN=<command>] matinee.nsi
; STAGE holds matinee.exe, matinee-tray.exe, ffmpeg\ and LICENSE.txt.
;
; Flags: /S installs without questions, /update (with /S) starts Matinee
; again afterwards, which is how the tray app updates itself.

Unicode true
ManifestDPIAware true
ManifestSupportedOS Win10
RequestExecutionLevel admin
SetCompressor /SOLID lzma

!include MUI2.nsh
!include LogicLib.nsh
!include x64.nsh
!include WinVer.nsh
!include FileFunc.nsh

!define NAME "Matinee"
!define PUBLISHER "Yeix"
!define UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\Matinee"
!define FIREWALL_RULE "Matinee"

; Each signs what makensis writes, when signing is set up (scripts/sign-windows.sh).
!ifdef SIGN
  !finalize '${SIGN} "%1"'
  !uninstfinalize '${SIGN} "%1"'
!endif

Name "${NAME}"
OutFile "${OUTFILE}"
InstallDir "$PROGRAMFILES64\Matinee"
InstallDirRegKey HKLM "${UNINSTALL_KEY}" "InstallLocation"
BrandingText "Matinee ${VERSION}"

VIProductVersion "${NUMVER}"
VIAddVersionKey "ProductName" "Matinee"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "FileDescription" "Matinee ${VERSION} setup"
VIAddVersionKey "CompanyName" "${PUBLISHER}"
VIAddVersionKey "LegalCopyright" "AGPL-3.0-or-later"

!define MUI_ICON "matinee.ico"
!define MUI_UNICON "matinee.ico"
!define MUI_ABORTWARNING
!define MUI_WELCOMEPAGE_TEXT "This installs Matinee ${VERSION}, the media server for your movies, shows, music and audiobooks.$\r$\n$\r$\nIt runs in the background from an icon next to the clock, and you use it from your browser and the Matinee apps."
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "Start Matinee"
!define MUI_FINISHPAGE_RUN_FUNCTION StartMatinee

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Function .onInit
  ${IfNot} ${AtLeastWin10}
    MessageBox MB_ICONSTOP "Matinee needs Windows 10 or newer." /SD IDOK
    Abort
  ${EndIf}
!if ${ARCH} == "arm64"
  ${IfNot} ${IsNativeARM64}
    MessageBox MB_ICONSTOP "This is Matinee for Arm PCs. Download the x64 installer for this PC." /SD IDOK
    Abort
  ${EndIf}
!else
  ; Arm PCs run x64 programs too, but get the faster arm64 installer from the site.
  ${IfNot} ${IsNativeAMD64}
  ${AndIfNot} ${IsNativeARM64}
    MessageBox MB_ICONSTOP "Matinee needs 64-bit Windows." /SD IDOK
    Abort
  ${EndIf}
!endif
  SetRegView 64
FunctionEnd

; The tray app starts through Explorer so it runs as the signed-in user, not
; as the administrator who installed it: the server then sees that user's
; mapped drives and network share passwords.
Function StartMatinee
  Exec '"$WINDIR\explorer.exe" "$INSTDIR\matinee-tray.exe"'
FunctionEnd

; Quitting the tray app stops the server too (it closes the server's stdin).
!macro StopMatinee
  nsExec::Exec 'taskkill /IM matinee-tray.exe /F'
  Sleep 3000
  nsExec::Exec 'taskkill /IM matinee.exe /F'
  Sleep 500
!macroend

Section "Matinee"
  SetShellVarContext all
  !insertmacro StopMatinee

  SetOutPath "$INSTDIR"
  ; ffmpeg\ is replaced whole, so libraries from an older FFmpeg don't linger.
  RMDir /r "$INSTDIR\ffmpeg"
  File /r "${STAGE}\*.*"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  CreateShortcut "$SMPROGRAMS\Matinee.lnk" "$INSTDIR\matinee-tray.exe" "" "$INSTDIR\matinee-tray.exe" 0 SW_SHOWNORMAL "" "Start Matinee"

  ; Phones, TVs and other computers at home reach the server on private
  ; networks. Public networks (a café's Wi-Fi) stay closed.
  nsExec::Exec 'netsh advfirewall firewall delete rule name="${FIREWALL_RULE}"'
  nsExec::ExecToLog 'netsh advfirewall firewall add rule name="${FIREWALL_RULE}" description="Matinee media server" dir=in action=allow program="$INSTDIR\matinee.exe" enable=yes profile=private,domain'

  WriteRegStr HKLM "${UNINSTALL_KEY}" "DisplayName" "Matinee"
  WriteRegStr HKLM "${UNINSTALL_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNINSTALL_KEY}" "Publisher" "${PUBLISHER}"
  WriteRegStr HKLM "${UNINSTALL_KEY}" "DisplayIcon" "$INSTDIR\matinee-tray.exe"
  WriteRegStr HKLM "${UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "${UNINSTALL_KEY}" "URLInfoAbout" "https://matinee.yeix.io"
  WriteRegStr HKLM "${UNINSTALL_KEY}" "HelpLink" "https://matinee.yeix.io/docs/getting-started/windows/"
  WriteRegStr HKLM "${UNINSTALL_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKLM "${UNINSTALL_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegDWORD HKLM "${UNINSTALL_KEY}" "NoModify" 1
  WriteRegDWORD HKLM "${UNINSTALL_KEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  WriteRegDWORD HKLM "${UNINSTALL_KEY}" "EstimatedSize" $0
SectionEnd

Function .onInstSuccess
  ${GetParameters} $0
  ClearErrors
  ${GetOptions} $0 "/update" $1
  ${IfNot} ${Errors}
    Call StartMatinee
  ${EndIf}
FunctionEnd

Function un.onInit
  SetRegView 64
FunctionEnd

Section "Uninstall"
  SetShellVarContext all
  !insertmacro StopMatinee

  nsExec::Exec 'netsh advfirewall firewall delete rule name="${FIREWALL_RULE}"'
  Delete "$SMPROGRAMS\Matinee.lnk"
  DeleteRegKey HKLM "${UNINSTALL_KEY}"
  ; Starting at sign-in is set per user; this removes it for whoever uninstalls.
  DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "Matinee"

  RMDir /r "$INSTDIR\ffmpeg"
  Delete "$INSTDIR\matinee.exe"
  Delete "$INSTDIR\matinee-tray.exe"
  Delete "$INSTDIR\LICENSE.txt"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"

  ; The library (watch history, accounts, settings) stays unless asked, so
  ; reinstalling picks up where it left off.
  SetShellVarContext current
  ${If} ${FileExists} "$LOCALAPPDATA\Matinee\*.*"
    MessageBox MB_YESNO|MB_ICONQUESTION|MB_DEFBUTTON2 "Also delete your Matinee library, accounts and settings in $LOCALAPPDATA\Matinee?$\r$\n$\r$\nYour media files aren't touched either way." /SD IDNO IDNO keep
    RMDir /r "$LOCALAPPDATA\Matinee"
    keep:
  ${EndIf}
SectionEnd
