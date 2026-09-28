project = "boost.org"
why     = "libboost_iostreams links bzip2 and nothing declared it, so a bottle installed into an empty store cannot start. The rpath edit is the same fault on the other side: the library recorded a path that exists on the runner that built it and on no user's machine."

edits = [
  {
    path = "build.script"
    from = <<EOT
@loader_path $LIB
done
EOT
    to   = <<EOT
@loader_path $LIB
  install_name_tool -add_rpath @loader_path/../../.. $LIB
done
EOT
  },
  {
    path = "dependencies[\"sourceware.org/bzip2\"]"
    set  = "^1"
  },
]
