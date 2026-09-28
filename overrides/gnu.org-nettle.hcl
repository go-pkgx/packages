project = "gnu.org/nettle"
why     = "converted from gnu.org-nettle-declares-gmp.patch; the reason is in the git history of those files"

edits = [
  {
    path = "dependencies"
    set = {
      "gnu.org/gmp" = "^6"
    }
  },
]
