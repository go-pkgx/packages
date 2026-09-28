project = "open-mpi.org/hwloc"
why     = "converted from open-mpi.org-hwloc-libxml2.patch; the reason is in the git history of those files"

edits = [
  {
    path = "dependencies"
    set = {
      "gnome.org/libxml2" = ">=2.14"
    }
  },
]
