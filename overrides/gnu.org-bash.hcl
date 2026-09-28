project = "gnu.org/bash"
why     = "converted from gnu.org-bash-darwin-gettext.patch, gnu.org-bash-without-bash-malloc.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.script"
    set = [
      {
        "if" = "<5"
        "run" = "CFLAGS=\"$CFLAGS -Wno-incompatible-pointer-types -Wno-implicit-int\""
      },
      {
        "if" = "<5"
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
        "if" = "<5"
        "run" = "CFLAGS=\"$CFLAGS -Wno-incompatible-pointer-types -Wno-implicit-int\""
      },
      {
        "if" = "<5"
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
