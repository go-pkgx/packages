project = "gnu.org/binutils"
why     = "converted from gnu.org-binutils-zstd-runtime.patch; the reason is in the git history of those files"

edits = [
  {
    path = "dependencies"
    set = {
      "facebook.com/zstd" = "^1"
    }
  },
]
