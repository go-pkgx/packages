project = "pkgx.sh"
why     = "pkgx links liblzma on darwin and its recipe declares no runtime dependencies at all."

edits = [
  {
    path = "dependencies"
    set = {
      "darwin" = {
        "tukaani.org/xz" = "^5"
      }
    }
  },
]
