; Optic by OB Industries - one-file Windows installer (per user, no admin needed)
; Built by scripts/build.sh:  makensis -DVERSION=x.y.z -DSRC=<folder with Optic.exe> -DOUT=<setup.exe> installer/optic.nsi

!ifndef VERSION
  !error "VERSION not defined"
!endif
!ifndef SRC
  !define SRC "..\dist\build"
!endif
!ifndef OUT
  !define OUT "..\dist\Optic-Setup-${VERSION}.exe"
!endif

!define APPNAME   "Optic"
!define COMPANY   "OB Industries"
!define EXE       "Optic.exe"
!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\Optic"

Unicode true
Target amd64-unicode   ; Optic is 64-bit only, so the installer is too
SetCompressor /SOLID lzma
RequestExecutionLevel user
ManifestDPIAware true

Name "${APPNAME}"
OutFile "${OUT}"
InstallDir "$LOCALAPPDATA\Programs\Optic"
InstallDirRegKey HKCU "Software\OB Industries\Optic" "InstallDir"
BrandingText "${APPNAME} ${VERSION} · created by ${COMPANY}"

VIProductVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "${APPNAME}"
VIAddVersionKey "CompanyName" "${COMPANY}"
VIAddVersionKey "FileDescription" "${APPNAME} Setup"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "LegalCopyright" "Created by ${COMPANY}"

!include "MUI2.nsh"
!include "FileFunc.nsh"

!define MUI_ICON   "..\assets\optic.ico"
!define MUI_UNICON "..\assets\optic.ico"
!define MUI_ABORTWARNING
!define MUI_WELCOMEFINISHPAGE_BITMAP "..\assets\installer-sidebar.bmp"
!define MUI_UNWELCOMEFINISHPAGE_BITMAP "..\assets\installer-sidebar.bmp"
!define MUI_WELCOMEPAGE_TITLE "Install ${APPNAME} ${VERSION}"
!define MUI_WELCOMEPAGE_TEXT "Optic makes the dark parts of your games easier to see by adjusting your screen's colours while you play. It never touches the game files, so it can't crash the game.$\r$\n$\r$\nOptic installs just for you - no administrator permission needed - and adds shortcuts to your desktop and Start menu.$\r$\n$\r$\nIf Optic is already running it will be closed and updated. Your looks and settings are kept."
!define MUI_FINISHPAGE_RUN "$INSTDIR\${EXE}"
!define MUI_FINISHPAGE_RUN_TEXT "Open Optic now"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Var IsUpdate

Function .onInit
  StrCpy $IsUpdate "0"
  ${GetParameters} $R0
  ClearErrors
  ${GetOptions} $R0 "/UPDATE" $R1
  IfErrors +2
    StrCpy $IsUpdate "1"
FunctionEnd

; Ask every running Optic to close properly (it puts the screen colours back
; first), then close anything left - including old portable copies (V1.0.x on the
; desktop or in Downloads), which don't know the quit signal. A force-closed copy
; leaves restore.json behind, and the new Optic puts the colours back on start.
!macro CloseOptic
  InitPluginsDir
  File "/oname=$PLUGINSDIR\${EXE}" "${SRC}\${EXE}"
  nsExec::Exec '"$PLUGINSDIR\${EXE}" --quit'
  Pop $0
  Sleep 1500
  nsExec::Exec 'taskkill /IM ${EXE} /F'
  Pop $0
  nsExec::Exec 'taskkill /IM ClearRide.exe /F'
  Pop $0
  ; wait until the installed exe is free to replace (up to ~8 s)
  StrCpy $1 0
  ${Do}
    IfFileExists "$INSTDIR\${EXE}" 0 closed
    ClearErrors
    FileOpen $2 "$INSTDIR\${EXE}" a
    IfErrors 0 +3
      Sleep 500
      Goto next
    FileClose $2
    Goto closed
    next:
    IntOp $1 $1 + 1
  ${LoopUntil} $1 >= 16
  closed:
!macroend

!include "LogicLib.nsh"

Section "Install"
  SetShellVarContext current
  !insertmacro CloseOptic

  SetOutPath "$INSTDIR"
  SetOverwrite on
  File "${SRC}\${EXE}"
  WriteUninstaller "$INSTDIR\Uninstall Optic.exe"

  CreateShortcut "$DESKTOP\Optic.lnk" "$INSTDIR\${EXE}" "" "$INSTDIR\${EXE}" 0
  CreateShortcut "$SMPROGRAMS\Optic.lnk" "$INSTDIR\${EXE}" "" "$INSTDIR\${EXE}" 0

  WriteRegStr HKCU "Software\OB Industries\Optic" "InstallDir" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayName" "${APPNAME}"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINSTKEY}" "Publisher" "${COMPANY}"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\${EXE},0"
  WriteRegStr HKCU "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTKEY}" "UninstallString" '"$INSTDIR\Uninstall Optic.exe"'
  WriteRegStr HKCU "${UNINSTKEY}" "QuietUninstallString" '"$INSTDIR\Uninstall Optic.exe" /S'
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKCU "${UNINSTKEY}" "EstimatedSize" "$0"

  ; silent installs (in-app updates) open Optic again straight away
  IfSilent 0 +2
    Exec '"$INSTDIR\${EXE}"'
SectionEnd

Section "Uninstall"
  SetShellVarContext current
  ; close Optic so it restores the screen colours
  nsExec::Exec '"$INSTDIR\${EXE}" --quit'
  Pop $0
  Sleep 1500
  nsExec::Exec 'taskkill /IM ${EXE} /F'
  Pop $0

  Delete "$INSTDIR\${EXE}"
  Delete "$INSTDIR\Uninstall Optic.exe"
  RMDir "$INSTDIR"
  Delete "$DESKTOP\Optic.lnk"
  Delete "$SMPROGRAMS\Optic.lnk"
  DeleteRegKey HKCU "${UNINSTKEY}"
  DeleteRegKey HKCU "Software\OB Industries\Optic"
  DeleteRegKey /ifempty HKCU "Software\OB Industries"
  DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "Optic"
  ; your looks and settings in %APPDATA%\Optic are kept, in case you reinstall
SectionEnd
