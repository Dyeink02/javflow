!macro customHeader
  ; Override electron-builder's assisted installer Chinese copy with polished wording.
  !pragma warning disable 6030
  LangString chooseInstallationOptions ${LANG_SIMPCHINESE} "安装方式选择"
  LangString chooseUninstallationOptions ${LANG_SIMPCHINESE} "卸载方式选择"
  LangString whichInstallationShouldBeRemoved ${LANG_SIMPCHINESE} "请选择需要移除的已安装实例"
  LangString whoShouldThisApplicationBeInstalledFor ${LANG_SIMPCHINESE} "请选择本软件的安装范围"
  LangString selectUserMode ${LANG_SIMPCHINESE} "请选择仅为当前用户安装，还是为这台电脑上的所有用户安装。"
  LangString whichInstallationRemove ${LANG_SIMPCHINESE} "检测到本软件可能存在多个安装范围，请选择要移除的安装实例。"
  LangString freshInstallForAll ${LANG_SIMPCHINESE} "为这台电脑上的所有用户全新安装（需要管理员权限）"
  LangString freshInstallForCurrent ${LANG_SIMPCHINESE} "仅为当前用户全新安装"
  LangString onlyForMe ${LANG_SIMPCHINESE} "仅当前用户"
  LangString forAll ${LANG_SIMPCHINESE} "这台电脑上的所有用户"
  LangString loginWithAdminAccount ${LANG_SIMPCHINESE} "继续前，请使用具有管理员权限的账户登录。"
  LangString perUserInstallExists ${LANG_SIMPCHINESE} "已检测到当前用户安装。"
  LangString perUserInstall ${LANG_SIMPCHINESE} "当前用户安装已存在。"
  LangString perMachineInstallExists ${LANG_SIMPCHINESE} "已检测到所有用户安装。"
  LangString perMachineInstall ${LANG_SIMPCHINESE} "所有用户安装已存在。"
  LangString reinstallUpgrade ${LANG_SIMPCHINESE} "将执行重新安装或版本升级。"
  LangString uninstall ${LANG_SIMPCHINESE} "将执行卸载。"
  LangString installing ${LANG_SIMPCHINESE} "正在安装 JavFlow，请查看下方详细进度。"
  !pragma warning default 6030
!macroend

!macro customInstall
  CreateDirectory "$INSTDIR"
  ${IfNot} ${Silent}
    DetailPrint "正在完成安装收尾配置..."
    DetailPrint "安装即将完成，正在准备启动信息。"
  ${endif}
!macroend
