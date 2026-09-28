project = "rsync.samba.org"
why     = "rsync was silently borrowing a library it never declared, and the s390x build is where that stopped working."

edits = [
  {
    path = "dependencies[\"gnu.org/libidn2\"]"
    set  = "*"
  },
]
