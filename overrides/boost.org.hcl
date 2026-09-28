project = "boost.org"
why     = "converted from boost.org-declares-bzip2.patch, boost.org-rpath.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.script"
    from = <<EOT
B
EOT
    to   = <<EOT
B
  install_name_tool -add_rpath @loader_path/../../.. $LIB
EOT
  },
  {
    path = "dependencies[\"sourceware.org/bzip2\"]"
    set = "^1"
  },
]
