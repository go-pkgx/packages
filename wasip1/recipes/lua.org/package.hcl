provides = [
  "bin/lua",
  "bin/luac",
]

build {
  dependencies = {
    "gnu.org/make" = "*"
  }
  script = [
    {
      prop = <<EOT
void *tmpfile(void) { return 0; }
char *tmpnam(char *s) { (void)s; return 0; }
int system(const char *c) { return c ? -1 : 0; }
EOT
      run = "cp $PROP wasi_stubs.c"
    },
    {
      run = "$BK_CC -O2 -c -o wasi_stubs.o wasi_stubs.c"
    },
    "make -C src -j {{ hw.concurrency }} lua luac CC=\"$BK_CC\" AR=\"llvm-ar rcu\" RANLIB=llvm-ranlib MYCFLAGS=\"-DL_tmpnam=32 -D_WASI_EMULATED_PROCESS_CLOCKS -D_WASI_EMULATED_SIGNAL -mllvm -wasm-enable-sjlj -mllvm -wasm-use-legacy-eh=false\" MYLIBS=\"$SRCROOT/wasi_stubs.o -lwasi-emulated-process-clocks -lwasi-emulated-signal -lsetjmp\" MYLDFLAGS=\"-mllvm -wasm-enable-sjlj -mllvm -wasm-use-legacy-eh=false\"",
    "install -D src/lua \"{{ prefix }}/bin/lua.wasm\"",
    "install -D src/luac \"{{ prefix }}/bin/luac.wasm\"",
  ]
}

distributable {
  strip-components = 1
  url = "http://www.lua.org/ftp/lua-{{version}}.tar.gz"
}

versions {
  match = "/lua-(\\d+\\.\\d+\\.\\d+)\\.tar\\.gz/"
  strip = [
    "/lua-/",
    "/.tar.gz/",
  ]
  url = "http://www.lua.org/ftp/"
}
