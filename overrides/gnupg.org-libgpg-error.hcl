project = "gnupg.org/libgpg-error"
why     = "libgpg-error links gettext's libintl on darwin and did not declare it. Found by cross-checking every darwin Mach-O that references libintl against what its project declares."

edits = [
  {
    path = "dependencies"
    set = {
      "darwin" = {
        "gnu.org/gettext" = "^1"
      }
    }
  },
]
