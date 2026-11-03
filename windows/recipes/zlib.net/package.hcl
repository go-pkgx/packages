# zlib for windows/x86-64, cross-built from linux with llvm-mingw.
#
# The pilot for the Windows C lane, chosen because bk's own build package
# names it as a tested path — `build/build.go`'s header lists "zlib native,
# zlib→windows cross, wget with openssl" among the real packages its tests
# cover. A first lane should fail for reasons about the LANE, not about the
# package.
#
# ⛔ THE COMPILER COMES FROM THIS RECIPE, NOT FROM bk. For every other target
# bk puts `llvm.org` in the environment itself; for a windows cross build it
# deliberately does not, because "its compiler comes from the recipe's
# llvm-mingw dep" (buildscript/wrapper.go). So `llvm.org/mingw-w64` is a hard
# dependency here and the tool names are spelled in the script: nothing else
# will supply them.
#
# ⛔ win32/Makefile.gcc, NOT ./configure. zlib's configure probes by compiling
# and running, which a cross build cannot do; upstream ships this makefile for
# exactly this case. `PREFIX=x86_64-w64-mingw32-` is how it is told which
# toolchain to call — it prepends that to `gcc`, `ar` and `strip`.

provides = [
  "bin/minigzip.exe",
]

build {
  dependencies = {
    "llvm.org/mingw-w64" = "*"
    "gnu.org/make"       = "*"
  }
  script = [
    "make -f win32/Makefile.gcc libz.a minigzip.exe PREFIX=x86_64-w64-mingw32-",
    "install -D minigzip.exe \"{{ prefix }}/bin/minigzip.exe\"",
    "install -D libz.a \"{{ prefix }}/lib/libz.a\"",
    "install -D zlib.h \"{{ prefix }}/include/zlib.h\"",
    "install -D zconf.h \"{{ prefix }}/include/zconf.h\"",
  ]
}

distributable {
  strip-components = 1
  url              = "https://zlib.net/zlib-{{version.raw}}.tar.gz"
}

versions {
  github = "madler/zlib"
}
