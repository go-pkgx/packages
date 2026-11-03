distributable = [
  {
    strip-components = 1
    url = "https://sqlite.org/2025/sqlite-autoconf-{{version.major}}{{version.minor}}0{{version.patch}}00.tar.gz"
  },
  {
    strip-components = 1
    url = "https://sqlite.org/2026/sqlite-autoconf-{{version.major}}{{version.minor}}0{{version.patch}}00.tar.gz"
  },
]
provides = [
  "bin/sqlite3",
]

build {
  script = [
    {
      run = "$BK_CC -O2 -DSQLITE_THREADSAFE=0 -DSQLITE_OMIT_LOAD_EXTENSION=1 -DSQLITE_OMIT_WAL=1 -DSQLITE_OMIT_SHARED_CACHE=1 -DSQLITE_NO_SYNC=1 -DSQLITE_DEFAULT_MEMSTATUS=0 -DSQLITE_NOHAVE_SYSTEM=1 -D_WASI_EMULATED_GETPID -D_WASI_EMULATED_PROCESS_CLOCKS -D_WASI_EMULATED_SIGNAL -o sqlite3.wasm shell.c sqlite3.c -lwasi-emulated-getpid -lwasi-emulated-process-clocks -lwasi-emulated-signal"
    },
    "install -D sqlite3.wasm \"{{ prefix }}/bin/sqlite3.wasm\"",
    "install -D sqlite3.h \"{{ prefix }}/include/sqlite3.h\"",
  ]
}

versions {
  github = "sqlite/sqlite/tags"
  strip = "/^version-/"
}
