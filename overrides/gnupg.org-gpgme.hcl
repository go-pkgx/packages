project = "gnupg.org/gpgme"
why     = "converted from gnupg.org-gpgme-declares-what-it-links.patch; the reason is in the git history of those files"

edits = [
  {
    path = "dependencies"
    set = {
      "gnupg.org/libassuan" = "^2"
      "gnupg.org/libgpg-error" = "^1"
    }
  },
]
