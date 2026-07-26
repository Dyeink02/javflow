#!/usr/bin/env node

// packaging-owner: maintained NSIS template patch helper; marker=active-toolchain-prepare-nsis-templates
// NSIS template patcher for the maintained installer experience.
// This is packaging-only infrastructure: use it when installer UI copy or
// details output are wrong, not when the runtime application logic is wrong.
//
// Ownership summary:
// 1) patch third-party NSIS templates for the maintained installer experience
// 2) keep installer text/template surgery out of general packaging scripts
// 3) make installer-specific diffs easier to isolate and review
//
// Boundary rule:
// packaging-only helper; runtime code should not import this file.
//
// File map for maintainers:
// 1) template path discovery
// 2) replacement helpers for shared/common installer sections
// 3) patch orchestration and failure reporting

const fs = require('fs');
const path = require('path');

const repoRoot = path.join(__dirname, '..');
const nsisTemplateRoot = path.join(repoRoot, 'node_modules', 'app-builder-lib', 'templates', 'nsis');
const commonPath = path.join(nsisTemplateRoot, 'common.nsh');
const installSectionPath = path.join(nsisTemplateRoot, 'installSection.nsh');
const installerIncludePath = path.join(repoRoot, 'build', 'installer.nsh');

function replaceOnce(content, pattern, replacement, label) {
  if (!pattern.test(content)) {
    throw new Error(`\u65e0\u6cd5\u627e\u5230\u5f85\u66ff\u6362\u7247\u6bb5\uff1a${label}`);
  }

  return content.replace(pattern, replacement);
}

function patchCommonTemplate() {
  let content = fs.readFileSync(commonPath, 'utf8');
  content = content.replace('ShowInstDetails nevershow', 'ShowInstDetails show');
  content = content.replace('ShowUninstDetails nevershow', 'ShowUninstDetails show');
  fs.writeFileSync(commonPath, content, 'utf8');
}

function patchInstallSectionTemplate() {
  let content = fs.readFileSync(installSectionPath, 'utf8');

  content = replaceOnce(
    content,
    /\$\{IfNot\} \$\{Silent\}\s+SetDetailsPrint (?:none|both)\s+\$\{endif\}/m,
    '${IfNot} ${Silent}\n  SetDetailsPrint both\n${endif}',
    '\u5b89\u88c5\u8be6\u60c5\u663e\u793a\u8bbe\u7f6e'
  );

  content = replaceOnce(
    content,
    /SetOutPath \$INSTDIR[\s\S]*?!ifdef UNINSTALLER_ICON/m,
    [
      'SetOutPath $INSTDIR',
      '',
      '${IfNot} ${Silent}',
      '  DetailPrint "\u5b89\u88c5\u76ee\u5f55\uff1a$INSTDIR"',
      '  DetailPrint "\u6b63\u5728\u5b89\u88c5\u4e3b\u7a0b\u5e8f\uff1a${APP_EXECUTABLE_FILENAME}"',
      '${endif}',
      '',
      '!ifdef UNINSTALLER_ICON'
    ].join('\n'),
    '\u5b89\u88c5\u76ee\u5f55\u63d0\u793a'
  );

  content = replaceOnce(
    content,
    /!insertmacro installApplicationFiles[\s\S]*?!insertmacro registryAddInstallInfo/m,
    [
      '!insertmacro installApplicationFiles',
      '${IfNot} ${Silent}',
      '  DetailPrint "\u6838\u5fc3\u6587\u4ef6\u590d\u5236\u5b8c\u6210\uff0c\u6b63\u5728\u5199\u5165\u5378\u8f7d\u4e0e\u7248\u672c\u4fe1\u606f..."',
      '${endif}',
      '!insertmacro registryAddInstallInfo'
    ].join('\n'),
    '\u6ce8\u518c\u8868\u5199\u5165\u63d0\u793a'
  );

  content = replaceOnce(
    content,
    /!insertmacro registryAddInstallInfo[\s\S]*?!insertmacro addStartMenuLink \$keepShortcuts/m,
    [
      '!insertmacro registryAddInstallInfo',
      '${IfNot} ${Silent}',
      '  DetailPrint "\u6b63\u5728\u521b\u5efa\u5f00\u59cb\u83dc\u5355\u5feb\u6377\u65b9\u5f0f..."',
      '${endif}',
      '!insertmacro addStartMenuLink $keepShortcuts'
    ].join('\n'),
    '\u5f00\u59cb\u83dc\u5355\u63d0\u793a'
  );

  content = replaceOnce(
    content,
    /!insertmacro addStartMenuLink \$keepShortcuts[\s\S]*?!insertmacro addDesktopLink \$keepShortcuts/m,
    [
      '!insertmacro addStartMenuLink $keepShortcuts',
      '${IfNot} ${Silent}',
      '  DetailPrint "\u6b63\u5728\u521b\u5efa\u684c\u9762\u5feb\u6377\u65b9\u5f0f..."',
      '${endif}',
      '!insertmacro addDesktopLink $keepShortcuts'
    ].join('\n'),
    '\u684c\u9762\u5feb\u6377\u65b9\u5f0f\u63d0\u793a'
  );

  content = replaceOnce(
    content,
    /!insertmacro addDesktopLink \$keepShortcuts[\s\S]*?\$\{if\} \$\{FileExists\} "\$newStartMenuLink"/m,
    [
      '!insertmacro addDesktopLink $keepShortcuts',
      '${IfNot} ${Silent}',
      '  DetailPrint "\u6b63\u5728\u6574\u7406\u542f\u52a8\u5165\u53e3\u4e0e\u9644\u5e26\u6587\u4ef6..."',
      '${endif}',
      '',
      '${if} ${FileExists} "$newStartMenuLink"'
    ].join('\n'),
    '\u6536\u5c3e\u63d0\u793a'
  );

  fs.writeFileSync(installSectionPath, content, 'utf8');
}

function writeInstallerInclude() {
  const lines = [
    '!macro customHeader',
    "  ; Override electron-builder's assisted installer Chinese copy with polished wording.",
    '  !pragma warning disable 6030',
    '  LangString chooseInstallationOptions ${LANG_SIMPCHINESE} "\u5b89\u88c5\u65b9\u5f0f\u9009\u62e9"',
    '  LangString chooseUninstallationOptions ${LANG_SIMPCHINESE} "\u5378\u8f7d\u65b9\u5f0f\u9009\u62e9"',
    '  LangString whichInstallationShouldBeRemoved ${LANG_SIMPCHINESE} "\u8bf7\u9009\u62e9\u9700\u8981\u79fb\u9664\u7684\u5df2\u5b89\u88c5\u5b9e\u4f8b"',
    '  LangString whoShouldThisApplicationBeInstalledFor ${LANG_SIMPCHINESE} "\u8bf7\u9009\u62e9\u672c\u8f6f\u4ef6\u7684\u5b89\u88c5\u8303\u56f4"',
    '  LangString selectUserMode ${LANG_SIMPCHINESE} "\u8bf7\u9009\u62e9\u4ec5\u4e3a\u5f53\u524d\u7528\u6237\u5b89\u88c5\uff0c\u8fd8\u662f\u4e3a\u8fd9\u53f0\u7535\u8111\u4e0a\u7684\u6240\u6709\u7528\u6237\u5b89\u88c5\u3002"',
    '  LangString whichInstallationRemove ${LANG_SIMPCHINESE} "\u68c0\u6d4b\u5230\u672c\u8f6f\u4ef6\u53ef\u80fd\u5b58\u5728\u591a\u4e2a\u5b89\u88c5\u8303\u56f4\uff0c\u8bf7\u9009\u62e9\u8981\u79fb\u9664\u7684\u5b89\u88c5\u5b9e\u4f8b\u3002"',
    '  LangString freshInstallForAll ${LANG_SIMPCHINESE} "\u4e3a\u8fd9\u53f0\u7535\u8111\u4e0a\u7684\u6240\u6709\u7528\u6237\u5168\u65b0\u5b89\u88c5\uff08\u9700\u8981\u7ba1\u7406\u5458\u6743\u9650\uff09"',
    '  LangString freshInstallForCurrent ${LANG_SIMPCHINESE} "\u4ec5\u4e3a\u5f53\u524d\u7528\u6237\u5168\u65b0\u5b89\u88c5"',
    '  LangString onlyForMe ${LANG_SIMPCHINESE} "\u4ec5\u5f53\u524d\u7528\u6237"',
    '  LangString forAll ${LANG_SIMPCHINESE} "\u8fd9\u53f0\u7535\u8111\u4e0a\u7684\u6240\u6709\u7528\u6237"',
    '  LangString loginWithAdminAccount ${LANG_SIMPCHINESE} "\u7ee7\u7eed\u524d\uff0c\u8bf7\u4f7f\u7528\u5177\u6709\u7ba1\u7406\u5458\u6743\u9650\u7684\u8d26\u6237\u767b\u5f55\u3002"',
    '  LangString perUserInstallExists ${LANG_SIMPCHINESE} "\u5df2\u68c0\u6d4b\u5230\u5f53\u524d\u7528\u6237\u5b89\u88c5\u3002"',
    '  LangString perUserInstall ${LANG_SIMPCHINESE} "\u5f53\u524d\u7528\u6237\u5b89\u88c5\u5df2\u5b58\u5728\u3002"',
    '  LangString perMachineInstallExists ${LANG_SIMPCHINESE} "\u5df2\u68c0\u6d4b\u5230\u6240\u6709\u7528\u6237\u5b89\u88c5\u3002"',
    '  LangString perMachineInstall ${LANG_SIMPCHINESE} "\u6240\u6709\u7528\u6237\u5b89\u88c5\u5df2\u5b58\u5728\u3002"',
    '  LangString reinstallUpgrade ${LANG_SIMPCHINESE} "\u5c06\u6267\u884c\u91cd\u65b0\u5b89\u88c5\u6216\u7248\u672c\u5347\u7ea7\u3002"',
    '  LangString uninstall ${LANG_SIMPCHINESE} "\u5c06\u6267\u884c\u5378\u8f7d\u3002"',
    '  LangString installing ${LANG_SIMPCHINESE} "\u6b63\u5728\u5b89\u88c5 JAV\u81ea\u52a8\u5316\u722c\u866b\u5de5\u5177\uff0c\u8bf7\u67e5\u770b\u4e0b\u65b9\u8be6\u7ec6\u8fdb\u5ea6\u3002"',
    '  !pragma warning default 6030',
    '!macroend',
    '',
    '!macro customInstall',
    '  CreateDirectory "$INSTDIR"',
    '  ${IfNot} ${Silent}',
    '    DetailPrint "\u6b63\u5728\u5b8c\u6210\u5b89\u88c5\u6536\u5c3e\u914d\u7f6e..."',
    '    DetailPrint "\u5b89\u88c5\u5373\u5c06\u5b8c\u6210\uff0c\u6b63\u5728\u51c6\u5907\u542f\u52a8\u4fe1\u606f\u3002"',
    '  ${endif}',
    '!macroend',
    ''
  ];

  fs.writeFileSync(installerIncludePath, lines.join('\n'), 'utf8');
}

function main() {
  writeInstallerInclude();
  patchCommonTemplate();
  patchInstallSectionTemplate();
  console.log('NSIS \u5b89\u88c5\u6a21\u677f\u4e0e\u4e2d\u6587\u5b89\u88c5\u6587\u6848\u5df2\u66f4\u65b0\u3002');
}

main();
