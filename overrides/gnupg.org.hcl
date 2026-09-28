project = "gnupg.org"
why     = "converted from gnupg.org-gettext-v1.patch, gnupg.org-gpgme-declares-what-it-links.patch, gnupg.org-libassuan-declares-what-it-links.patch, gnupg.org-libgpg-error-darwin-gettext.patch, gnupg.org-v2.5-gettext-v1.patch; the reason is in the git history of those files"

edits = [
  {
    path = "dependencies.darwin[\"gnu.org/gettext\"]"
    set = "^1"
  },
]
