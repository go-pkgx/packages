project = "openssl.org"
why     = "openssl's build declares `perl.org: 5`, while texinfo and gettext pin ~5.42 — their XS modules are compiled against that minor and bk's base toolchain pins it for the same reason. One closure cannot satisfy both."

edits = [
  {
    path = "build.dependencies[\"perl.org\"]"
    set  = "~5.42"
  },
]
