project = "lua.org"
why     = "converted from lua.org-sed-delimiter.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.script"
    from = "\"s_\\$(MYCFLAGS)_$${CFLAGS} -fPIC_\" -e \"s_\\$(MYLDFLAGS)_$${LDFLAGS}_\""
    to   = "\"s|\\$(MYCFLAGS)|$${CFLAGS} -fPIC|\" -e \"s|\\$(MYLDFLAGS)|$${LDFLAGS}|\""
  },
]
