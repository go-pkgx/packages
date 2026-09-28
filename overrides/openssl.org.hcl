project = "openssl.org"
why     = "converted from openssl.org-perl-line.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.dependencies[\"perl.org\"]"
    set = "~5.42"
  },
]
