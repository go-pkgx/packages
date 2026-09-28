project = "gnu.org/bash"
why     = "bash links gettext's libintl on darwin and did not declare it, and `--without-bash-malloc` is needed because bash's own allocator does not survive this platform's page size."

edits = [
  {
    path = "build.script"
    set = [
      {
        "if"  = "<5"
        "run" = "CFLAGS=\"$CFLAGS -Wno-incompatible-pointer-types -Wno-implicit-int\""
      },
      {
        "if"  = "<5"
        "run" = "ARGS=\"$ARGS $LEGACY_ARGS\""
      },
      {
        "run" = "ARGS=\"$ARGS --without-bash-malloc\""
      },
      "./configure --prefix={{ prefix }} $ARGS",
      "make --jobs {{ hw.concurrency }} install",
    ]
    expect = [
      {
        "if"  = "<5"
        "run" = "CFLAGS=\"$CFLAGS -Wno-incompatible-pointer-types -Wno-implicit-int\""
      },
      {
        "if"  = "<5"
        "run" = "ARGS=\"$ARGS $LEGACY_ARGS\""
      },
      "./configure --prefix={{ prefix }} $ARGS",
      "make --jobs {{ hw.concurrency }} install",
    ]
  },
  {
    path = "dependencies"
    set = {
      "darwin" = {
        "gnu.org/gettext" = "^1"
      }
    }
  },
]
