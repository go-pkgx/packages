project = "pkgx.sh"
why     = "converted from pkgx.sh-darwin-xz.patch; the reason is in the git history of those files"

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
