project = "gnupg.org/libassuan"
why     = "converted from gnupg.org-libassuan-declares-what-it-links.patch; the reason is in the git history of those files"

edits = [
  {
    path = "dependencies"
    set = {
      "gnupg.org/libgpg-error" = "^1"
    }
  },
]
