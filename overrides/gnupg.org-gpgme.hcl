project = "gnupg.org/gpgme"
why     = "gpgme declares what its BUILD runs and not what the result LINKS, so a consumer installing the bottle into an empty store is missing libraries nothing named."

edits = [
  {
    path = "dependencies"
    set = {
      "gnupg.org/libassuan"    = "^2"
      "gnupg.org/libgpg-error" = "^1"
    }
  },
]
