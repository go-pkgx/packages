project = "lua.org"
why     = "lua 5.5.1 fails to build on darwin, both arches: the recipe's `sed` uses a delimiter that $LDFLAGS itself contains, so the substitution runs off the end of the expression."

edits = [
  {
    path = "build.script"
    from = "\"s_\\$(MYCFLAGS)_$${CFLAGS} -fPIC_\" -e \"s_\\$(MYLDFLAGS)_$${LDFLAGS}_\""
    to   = "\"s|\\$(MYCFLAGS)|$${CFLAGS} -fPIC|\" -e \"s|\\$(MYLDFLAGS)|$${LDFLAGS}|\""
  },
]
