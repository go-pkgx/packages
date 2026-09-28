project = "lua.org"
why     = "converted from lua.org-sed-delimiter.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.script"
    set = [
      {
        "run" = "sed -i -e \"s|\\$(MYCFLAGS)|$${CFLAGS} -fPIC|\" -e \"s|\\$(MYLDFLAGS)|$${LDFLAGS}|\" Makefile"
        "working-directory" = "src"
      },
      {
        "if" = ">=5.5"
        "run" = "export OS=\"$${OS%-readline}\""
      },
      "make $OS INSTALL_TOP={{prefix}}",
      "make install INSTALL_TOP={{prefix}}",
      {
        "run" = [
          "OBJS=\"$(grep ^CORE_O= Makefile | sed -e 's/^CORE_O=//')\"",
          "OBJS=\"$OBJS $(grep ^LIB_O= Makefile | sed -e 's/^LIB_O=//')\"",
          "cc $CFLAGS $LDFLAGS -o {{prefix}}/lib/liblua.$EXT -shared $OBJS",
        ]
        "working-directory" = "src"
      },
      {
        "prop" = <<EOT
V= {{version.marketing}}
R= {{version}}
prefix={{prefix}}
INSTALL_BIN= $${prefix}/bin
INSTALL_INC= $${prefix}/include/lua
INSTALL_LIB= $${prefix}/lib
INSTALL_MAN= $${prefix}/share/man/man1
INSTALL_LMOD= $${prefix}/share/lua/$${V}
INSTALL_CMOD= $${prefix}/lib/lua/$${V}
exec_prefix=$${prefix}
libdir=$${exec_prefix}/lib
includedir=$${prefix}/include/lua

Name: Lua
Description: An Extensible Extension Language
Version: {{version}}
Requires:
Libs: -L$${libdir} -llua -lm
Cflags: -I$${includedir}
EOT
        "run" = "cp $PROP lua.pc"
        "working-directory" = "$${{prefix}}/lib/pkgconfig"
      },
      {
        "if" = "linux"
        "run" = "sed -i 's/-lm/-lm -ldl/' lua.pc"
        "working-directory" = "$${{prefix}}/lib/pkgconfig"
      },
      "make test",
    ]
    expect = [
      {
        "run" = "sed -i -e \"s_\\$(MYCFLAGS)_$${CFLAGS} -fPIC_\" -e \"s_\\$(MYLDFLAGS)_$${LDFLAGS}_\" Makefile"
        "working-directory" = "src"
      },
      {
        "if" = ">=5.5"
        "run" = "export OS=\"$${OS%-readline}\""
      },
      "make $OS INSTALL_TOP={{prefix}}",
      "make install INSTALL_TOP={{prefix}}",
      {
        "run" = [
          "OBJS=\"$(grep ^CORE_O= Makefile | sed -e 's/^CORE_O=//')\"",
          "OBJS=\"$OBJS $(grep ^LIB_O= Makefile | sed -e 's/^LIB_O=//')\"",
          "cc $CFLAGS $LDFLAGS -o {{prefix}}/lib/liblua.$EXT -shared $OBJS",
        ]
        "working-directory" = "src"
      },
      {
        "prop" = <<EOT
V= {{version.marketing}}
R= {{version}}
prefix={{prefix}}
INSTALL_BIN= $${prefix}/bin
INSTALL_INC= $${prefix}/include/lua
INSTALL_LIB= $${prefix}/lib
INSTALL_MAN= $${prefix}/share/man/man1
INSTALL_LMOD= $${prefix}/share/lua/$${V}
INSTALL_CMOD= $${prefix}/lib/lua/$${V}
exec_prefix=$${prefix}
libdir=$${exec_prefix}/lib
includedir=$${prefix}/include/lua

Name: Lua
Description: An Extensible Extension Language
Version: {{version}}
Requires:
Libs: -L$${libdir} -llua -lm
Cflags: -I$${includedir}
EOT
        "run" = "cp $PROP lua.pc"
        "working-directory" = "$${{prefix}}/lib/pkgconfig"
      },
      {
        "if" = "linux"
        "run" = "sed -i 's/-lm/-lm -ldl/' lua.pc"
        "working-directory" = "$${{prefix}}/lib/pkgconfig"
      },
      "make test",
    ]
  },
]
