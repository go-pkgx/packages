project = "gnu.org/binutils"
why     = "`--with-zstd` is passed from binutils 2.39 onwards and makes the BUILT binaries link libzstd, but upstream declares zstd under build.dependencies alone. A consumer installing the bottle gets a binary that cannot start."

edits = [
  {
    path = "dependencies"
    set = {
      "facebook.com/zstd" = "^1"
    }
  },
]
