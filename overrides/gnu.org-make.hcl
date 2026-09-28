project = "gnu.org/make"
why     = "make links libintl on darwin and never said so. gettext is in bk's base toolchain, so it is always present while this is BUILT and the closure a consumer installs has a hole in it."

edits = [
  {
    path = "dependencies"
    set = {
      "darwin" = {
        "gnu.org/gettext" = "*"
      }
    }
  },
]
