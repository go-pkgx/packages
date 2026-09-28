project = "zlib.net"
why     = "converted from zlib.net-source-from-github.patch; the reason is in the git history of those files"

edits = [
  {
    path = "distributable.url"
    from = "zlib.net"
    to   = "github.com/madler/zlib/releases/download/v{{version.raw}}"
  },
]
